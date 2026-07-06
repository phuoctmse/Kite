# Kite Development Roadmap

## Project Status

Kite is a well-architected Kubernetes operator with solid foundations but critical components requiring implementation. The controller framework, agent architecture, and interfaces are complete. Core business logic (LLM integration, K8s observation, cluster snapshot) needs implementation.

---

## Phase 1: Observability (Read-Only Mode) ✅ **COMPLETE**

**Completed:** 2026-07-06 — `go build ./...` and `go vet ./...` pass with zero errors.

**Goal:** Agent that observes cluster state, receives events, reasons about problems, and logs diagnostic rationale — without modifying anything.

### Tasks

- [x] **1.1 Snapshot Builder** (`internal/snapshot/snapshot.go`)
  - ✅ Implement `Build()` to query cluster state
  - ✅ List Pods across namespaces, count by phase
  - ✅ List Nodes, count Ready vs NotReady
  - ✅ Query recent Warning events

- [x] **1.2 Anthropic Provider** (`internal/brain/anthropic.go`)
  - ✅ Implement `Decide()` method
  - ✅ Build Anthropic Messages API request
  - ✅ Map tools to Anthropic tool definitions
  - ✅ Parse `tool_use` blocks → `Plan.Actions`
  - ✅ Extract rationale from text blocks

- [x] **1.3 Observer Watcher** (`internal/observer/watcher.go`)
  - ✅ Set up controller-runtime informers (fixed from invalid Watch() API)
  - ✅ Watch Pod events (OOMKill, crash loops)
  - ✅ Watch Node condition changes
  - ✅ Watch Warning events
  - ✅ Send events to agent mailbox (non-blocking)

- [x] **1.4 Built-in Actions** (`internal/executor/builtins.go`)
  - ✅ Implement `get_pods` (list pods with status)
  - ✅ Implement `get_logs` (tail 200 lines via typed clientset)
  - ✅ Implement `describe_node` (conditions + capacity)

- [x] **1.5 Controller Integration** (`internal/controller/reconciler.go`)
  - ✅ Resolve LLM provider from Secret
  - ✅ Extract API key based on provider type
  - ✅ Instantiate correct brain.Provider
  - ✅ Open SQLite store at deterministic path
  - ✅ Wire brain + store + typed clientset into agent

### Deliverable

Agent running in **Passive Mode**:
- ✅ Observes cluster events (pod crashes, OOM kills, node issues)
- ✅ Snapshots cluster health every poll interval
- ✅ Calls LLM to reason about problems
- ✅ Can query pods, logs, and nodes
- ✅ Logs diagnostic rationale to SQLite
- ✅ **Never mutates cluster state**

### Testing Checklist

- [ ] Deploy to local K3s/kind cluster
- [ ] Create test pod that crashes repeatedly
- [ ] Verify agent detects crash and queries LLM
- [ ] Check SQLite store for event history
- [ ] Verify debounce window coalesces burst events
- [ ] Test with `allowedActions` containing only read-only actions

---

## Phase 2: Self-Healing (Write Actions) 🎯 **CURRENT PHASE**

**Goal:** Transition from observation to autonomous remediation with safe, bounded mutation actions.

### Tasks

- [ ] **2.1 Mutation Action Plugins**
  - Implement `scale_deployment` action
    - Adjust replica count up/down
    - Add min/max bounds checking
  - Implement `restart_pod` action
    - Delete pod to trigger recreation
    - Add cooldown to prevent restart loops
  - Implement `cordon_node` action
    - Mark node unschedulable
    - Require confirmation flag for production

- [ ] **2.2 Safety Rails**
  - Add per-action rate limiting
  - Implement cooldown periods between mutations
  - Add dry-run mode for testing
  - Log all mutations with timestamp + rationale

- [ ] **2.3 Rollback Mechanism**
  - Store pre-mutation state snapshots
  - Implement undo/rollback for scaling operations
  - Add manual rollback CLI tool

### Deliverable

Agent running in **Auto Mode**:
- ✅ Everything from Phase 1
- ✅ Can scale deployments in response to load
- ✅ Can restart failing pods
- ✅ Can cordon unhealthy nodes
- ✅ Respects safety limits and cooldowns
- ✅ Logs all mutations for audit

### Operating Modes

| Mode | allowedActions | Behavior |
|------|----------------|----------|
| **Passive** | `get_pods`, `get_logs`, `describe_node` | Observe only, log rationale |
| **Suggest** | Same as Passive | Observe + suggest actions in logs (human reviews) |
| **Auto** | Passive + `scale_deployment`, `restart_pod`, etc. | Full autonomous remediation |

---

## Phase 3: Production Hardening 🚀 **FUTURE**

**Goal:** Enterprise-ready operator with multi-provider support, observability, and operational tooling.

### Tasks

- [ ] **3.1 Additional LLM Providers**
  - Implement `OpenAIProvider.Decide()` (`internal/brain/openai.go`)
  - Implement `OllamaProvider.Decide()` (`internal/brain/ollama.go`)
  - Add provider auto-detection from endpoint
  - Support Azure OpenAI endpoints

- [ ] **3.2 Metrics & Monitoring**
  - Export Prometheus metrics
    - `kite_events_total{kind, namespace}`
    - `kite_actions_total{action, status}`
    - `kite_llm_calls_total{provider, status}`
    - `kite_llm_latency_seconds{provider}`
  - Add health check endpoints
  - Integrate with Grafana dashboards

- [ ] **3.3 Web Dashboard** (v0.4 Roadmap)
  - React/Vue frontend
  - Real-time event stream
  - Action history viewer
  - Manual trigger interface
  - Configuration editor

- [ ] **3.4 Advanced Features**
  - **v0.2:** AI auto-scaling driven by Prometheus metrics
    - Query Prometheus for CPU/memory/custom metrics
    - Feed metrics into LLM decision context
    - Dynamic HPA-style scaling
  - **v0.3:** Log anomaly detection
    - Stream pod logs through anomaly detector
    - Flag unusual patterns (error spikes, latency)
    - Trigger investigation workflow
  - **v1.0:** Multi-cluster support
    - Manage multiple clusters from single control plane
    - Cross-cluster event correlation
    - Federated store for global view

---

## Phase 4: Ecosystem & Extensions 🌐 **FUTURE**

**Goal:** Plugin ecosystem and integrations for common ops workflows.

### Tasks

- [ ] **4.1 Plugin SDK**
  - Documented `Action` interface
  - Example plugins (Slack notifications, PagerDuty)
  - Plugin registry and versioning

- [ ] **4.2 Integrations**
  - Slack/Discord ChatOps commands
  - PagerDuty incident creation
  - Datadog/New Relic metric injection
  - ArgoCD deployment hooks
  - Cert-manager certificate checks

- [ ] **4.3 Advanced Reasoning**
  - Multi-turn conversation support
  - Ask clarifying questions before acting
  - Chain-of-thought prompting
  - Retrieval-augmented generation (RAG) over runbooks

---

## Implementation Notes

### Token Efficiency Strategy

Kite is designed to minimize LLM costs:

1. **Structured Snapshots:** Cluster state compressed to ~300 tokens (not raw kubectl output)
2. **Prompt Caching:** Static tool definitions cached provider-side
3. **Debouncing:** 500ms window prevents LLM calls during event cascades
4. **On-Demand Fetching:** LLM fetches only needed data via tools (no upfront dump)

### Safety Philosophy

- **Whitelist-Only Execution:** Executor blocks any action not in `allowedActions`
- **Progressive Trust:** Start Passive → Suggest → Auto as confidence builds
- **Audit Trail:** SQLite store persists every event + action + rationale
- **Rate Limiting:** Per-action cooldowns prevent runaway automation
- **Dry-Run Mode:** Test action logic without cluster mutations

### Development Environment

**Prerequisites:**
- Go 1.26.4+
- K3s/kind cluster for testing
- Anthropic/OpenAI API key (or local Ollama)

**Local Development Loop:**
```bash
# Terminal 1: Run operator locally
go run cmd/agent/main.go

# Terminal 2: Apply test CRs
kubectl apply -f deploy/manifests/sample-kiteagent.yaml

# Terminal 3: Create chaos
kubectl run crasher --image=busybox --restart=Always -- sh -c "exit 1"

# Monitor agent logs for LLM reasoning
```

---

## Success Metrics

### Phase 1 (Observability)
- [ ] Agent successfully detects 3 types of cluster events
- [ ] LLM returns valid action plans with rationale
- [ ] SQLite store accumulates history over 24h run
- [ ] Zero false positives from debounce logic

### Phase 2 (Self-Healing)
- [ ] Agent autonomously recovers from induced failures
- [ ] No action exceeds configured safety limits
- [ ] Mean time to recovery (MTTR) < 5 minutes
- [ ] Zero unintended side effects over 1 week test

### Phase 3 (Production)
- [ ] Support 100+ node clusters
- [ ] Multi-provider LLM redundancy
- [ ] <1% token cost vs managed observability SaaS
- [ ] Web dashboard used by 5+ beta testers

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for:
- Code style guide
- Testing requirements
- PR process
- Local environment setup

---

## Version History

| Version | Release Date | Focus |
|---------|-------------|-------|
| **v0.1** | TBD | Phase 1 complete (Observability) |
| **v0.2** | TBD | AI auto-scaling + Prometheus |
| **v0.3** | TBD | Log anomaly detection |
| **v0.4** | TBD | Web dashboard |
| **v1.0** | TBD | Multi-cluster support |

---

**Last Updated:** 2026-07-06
**Current Phase:** Phase 2 (Planned)
**Last Completed:** Phase 1 ✅ (2026-07-06)
**Next Milestone:** v0.1 release with read-only agent → then v0.2 with self-healing
