# kite

AI-powered ops agent for K3s and Kubernetes clusters.

## What is kite?

Small teams running Kubernetes often lack a dedicated ops engineer watching for pod crashes, OOM kills, and configuration drift. Kite fills that gap by running inside your cluster as a native operator, continuously observing the cluster state and calling an LLM to reason about what it sees. When something is wrong, kite can diagnose it, retrieve logs, and take corrective action — without you having to wake up at 3 am. Unlike bolt-on AI tools that sit outside your infrastructure, kite is self-hosted, runs in-cluster, and uses the LLM purely as a decision engine over data it already owns.

## How it works

When kite starts, the observer layer launches two concurrent processes: a Kubernetes informer that watches Pod, Event, and Node objects for signals like OOM kills, crash loops, and node condition changes, and a periodic poller that ticks every `pollInterval` seconds to catch slower drift. Both feed events into a buffered mailbox channel (capacity 64). The agent drains that mailbox one event at a time; a 500 ms debounce window coalesces burst events of the same kind and namespace so a cascade of pod restarts produces one LLM call, not fifty.

On each event the agent calls the snapshot layer, which queries the cluster and produces two representations of its state: a compact JSON object (pod counts, node health, recent warnings) for structured tool calls, and a Markdown narrative for the LLM system prompt. Both representations are passed to the brain layer, which calls the configured LLM provider via its `Decide` method and returns a `Plan` — an ordered list of `ActionRequest` values plus a `Rationale` string. The executor then runs each action in the plan, but only after verifying that every action name appears in the `allowedActions` whitelist from the `KiteAgent` spec. Results and turn history are persisted to SQLite for context injection in future calls.

The effective operating mode is determined by what you put in `allowedActions`. With only the built-in read-only actions permitted the agent observes and reasons but never mutates anything — useful for auditing and gaining trust before enabling write actions. Adding mutation actions (scaling, patching) transitions the agent toward autonomous remediation.

## Architecture

```
cmd/agent/main.go
  └─ controller-runtime Manager
       └─ internal/controller.Reconciler        (one goroutine per KiteAgent CR)
            └─ internal/agent.Agent             (event loop, debounce, orchestration)
                 ├─ internal/observer.Observer
                 │    ├─ Watcher                (K8s informers → mailbox)
                 │    └─ Poller                 (time.Ticker → mailbox)
                 ├─ internal/snapshot.Snapshotter
                 │    ├─ JSON()                 (compact struct for tool calls)
                 │    └─ Markdown()             (narrative for system prompt)
                 ├─ internal/brain.Provider     (interface)
                 │    ├─ AnthropicProvider
                 │    ├─ OpenAIProvider
                 │    └─ OllamaProvider
                 ├─ internal/executor.Runner    (whitelist check + action dispatch)
                 │    └─ Registry              (get_pods, get_logs, describe_node + plugins)
                 └─ internal/store.Store        (SQLite via modernc.org/sqlite)

api/v1alpha1   KiteAgent CRD (group: kite.io)
```

## Quick start

### Prerequisites

- Go 1.26.4 or later
- A running K3s or Kubernetes cluster
- `kubectl` configured against that cluster
- An API key for Anthropic, an OpenAI-compatible endpoint, or a local Ollama instance

### Install

Run the install script to create the `kite-system` namespace, apply the CRD, and apply RBAC:

```bash
curl -fsSL https://raw.githubusercontent.com/kite-io/kite/main/deploy/install.sh | bash
```

To install into a different namespace:

```bash
KITE_NAMESPACE=my-namespace bash <(curl -fsSL https://raw.githubusercontent.com/kite-io/kite/main/deploy/install.sh)
```

### Deploy

Create a Secret containing your LLM API key:

```bash
kubectl create secret generic kite-llm-secret \
  --from-literal=ANTHROPIC_API_KEY=<your-key> \
  -n kite-system
```

Apply the sample `KiteAgent` CR:

```bash
kubectl apply -f deploy/manifests/sample-kiteagent.yaml
```

Verify the agent is running:

```bash
kubectl get kiteagents -n kite-system
# NAME     PROVIDER    PHASE   AGE
# sample   anthropic           30s
```

### Configure

A fully annotated `KiteAgent` resource:

```yaml
apiVersion: kite.io/v1alpha1
kind: KiteAgent
metadata:
  name: sample
  namespace: kite-system
spec:
  # Required. LLM backend to use. One of: anthropic, openai, ollama.
  llmProvider: anthropic

  # Optional. Override the provider's default API endpoint URL.
  llmEndpoint: ""

  # Optional. Name of a Kubernetes Secret in the same namespace that
  # holds the API key. For Anthropic the key must be ANTHROPIC_API_KEY.
  llmSecret: kite-llm-secret

  # Optional. How often the poller wakes the agent. Defaults to 60s.
  pollInterval: 60s

  # Optional. Restrict the agent to these namespaces.
  # Leave empty to watch all namespaces.
  namespaces:
    - default
    - kite-system

  # Required to allow any action beyond observation.
  # Each entry must match a registered action name.
  # The executor blocks any action not listed here.
  allowedActions:
    - name: get_pods
      description: List pods and their status in a namespace
    - name: get_logs
      description: Retrieve recent pod logs (capped at 200 lines)
    - name: describe_node
      description: Show node conditions, capacity, and allocatable resources
```

## LLM providers

| Provider | `llmProvider` value | Notes |
|---|---|---|
| Anthropic | `anthropic` | Default. Set `ANTHROPIC_API_KEY` in the referenced Secret. |
| OpenAI-compatible | `openai` | Works with OpenAI, Azure OpenAI, or any `/v1/chat/completions` endpoint. Set `llmEndpoint` for non-OpenAI hosts. |
| Ollama | `ollama` | Local inference. Set `llmEndpoint` to your Ollama host, e.g. `http://localhost:11434`. No API key required. |

## Operating modes

The agent's behaviour is controlled entirely by what you put in `allowedActions`. There is no separate mode field.

| Mode | Behaviour | When to use |
|---|---|---|
| Passive | Only read-only actions (`get_pods`, `get_logs`, `describe_node`) are listed in `allowedActions`. The agent observes, reasons, and logs its rationale but never mutates cluster state. | Getting started; auditing what the agent would do before trusting it. |
| Suggest | Same read-only `allowedActions`, but you monitor the agent's turn history in SQLite or logs to review its proposed `plan.Rationale` before acting manually. | Teams that want AI diagnosis but human approval for changes. |
| Auto | `allowedActions` includes mutation actions registered as plugins. The executor runs them immediately after the LLM returns a plan. | Production remediation when the action set is well-understood and bounded. |

## Actions

Kite separates the tools the LLM can reason with from the actions the executor can run. Four tools are always available to the brain regardless of `allowedActions`:

| Tool | Purpose |
|---|---|
| `get_cluster_snapshot` | Returns the compressed cluster health snapshot. The LLM should call this first. |
| `get_pod_logs` | Fetches the last N log lines from a pod. Params: `name`, `namespace`, `tail` (int). |
| `get_pod_events` | Returns recent Kubernetes events for a specific pod. Params: `name`, `namespace`. |
| `propose_action` | Signals the executor to run an action. The LLM calls this last, after reasoning. |

Three executor actions are registered by default and can be enabled by adding them to `allowedActions`:

| Action name | What it does |
|---|---|
| `get_pods` | Lists pods and their status in a namespace. |
| `get_logs` | Retrieves pod logs, capped at 200 lines. |
| `describe_node` | Returns node conditions, capacity, and allocatable resources. |

To add your own action, implement the `executor.Action` interface and call `runner.Register()`:

```go
type Action interface {
    Name() string
    Run(ctx context.Context, params map[string]any) (string, error)
}
```

Then add the action's name and description to the `allowedActions` list in your `KiteAgent` CR. The executor will block any action not present in that list, regardless of whether it is registered.

## Token efficiency

Kite is designed to stay cheap to run. The snapshot layer reduces the entire cluster state to a structured `Summary` object — total pods, running pods, failing pods, total nodes, ready nodes, and recent warning events — which serialises to a few hundred tokens rather than the tens of thousands you would get from raw `kubectl get all -A` output. The static sections of the system prompt (tool definitions, operator instructions) are good candidates for provider-side prompt caching, which cuts repeated costs significantly. The LLM fetches only the data it needs through explicit tool calls rather than receiving a dump of everything upfront. Finally, the 500 ms debounce window ensures that a cascade of related events — such as a deployment rolling out ten pods — produces a single LLM invocation rather than ten.

## Roadmap

| Version | Focus |
|---|---|
| v0.1 | Install, ChatOps, basic self-heal (current) |
| v0.2 | AI auto-scaling driven by Prometheus metrics |
| v0.3 | Log anomaly detection |
| v0.4 | Web dashboard |
| v1.0 | Multi-cluster support |

## Contributing

Contributions are welcome. The project uses Go 1.26.4, `modernc.org/sqlite` for embedded storage (no CGO or C toolchain required), and `sigs.k8s.io/controller-runtime` v0.20.4 for the operator framework. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on setting up a local environment, running tests, and submitting pull requests.

## License

Apache 2.0
