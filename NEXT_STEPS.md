# Next Steps to Complete Phase 1

## Quick Status
- ✅ **5/5 components implemented**
- ⚠️ **2 API usage fixes needed**
- ⏱️ **Estimated time: 45 minutes**

---

## Fix #1: Observer Watch Implementation (30 min)

### Current Issue
```
internal\observer\watcher.go:48: w.client.Watch undefined
```

### Root Cause
controller-runtime's `client.Client` doesn't expose `Watch()` directly.

### Solution: Use Manager's Cache

#### Step 1: Pass Cache to Observer
**File:** `internal/controller/reconciler.go`

Change:
```go
obs := observer.New(a.Mailbox(), r.Client, pollInterval)
```

To:
```go
// Note: Need to pass manager reference or create cache
// Option A: Pass manager to buildAgent
obs := observer.New(a.Mailbox(), mgr.GetCache(), pollInterval)
```

#### Step 2: Update Observer Constructor
**File:** `internal/observer/observer.go`

Change signature:
```go
func New(mailbox chan<- event.Event, c client.Client, pollInterval time.Duration) *Observer
```

To:
```go
func New(mailbox chan<- event.Event, cache cache.Cache, pollInterval time.Duration) *Observer
```

#### Step 3: Update Watcher to Use Informers
**File:** `internal/observer/watcher.go`

Replace the watch loops with informer setup:

```go
import (
    "sigs.k8s.io/controller-runtime/pkg/cache"
)

type Watcher struct {
    mailbox chan<- event.Event
    cache   cache.Cache
}

func (w *Watcher) start(ctx context.Context) {
    logger := log.FromContext(ctx)
    logger.Info("watcher started")

    // Get informers from cache
    podInformer, err := w.cache.GetInformer(ctx, &corev1.Pod{})
    if err != nil {
        logger.Error(err, "failed to get pod informer")
        return
    }

    eventInformer, err := w.cache.GetInformer(ctx, &corev1.Event{})
    if err != nil {
        logger.Error(err, "failed to get event informer")
        return
    }

    nodeInformer, err := w.cache.GetInformer(ctx, &corev1.Node{})
    if err != nil {
        logger.Error(err, "failed to get node informer")
        return
    }

    // Track state for detecting changes
    podStates := &sync.Map{} // key: namespace/name → *corev1.Pod

    // Add event handlers
    podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        UpdateFunc: func(oldObj, newObj interface{}) {
            oldPod := oldObj.(*corev1.Pod)
            newPod := newObj.(*corev1.Pod)
            w.handlePodUpdate(oldPod, newPod)
        },
    })

    eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: func(obj interface{}) {
            ev := obj.(*corev1.Event)
            if ev.Type == corev1.EventTypeWarning {
                w.send(event.Event{
                    Kind:      event.DriftDetect,
                    Namespace: ev.InvolvedObject.Namespace,
                    Resource:  ev.InvolvedObject.Name,
                    RawObj:    ev,
                    Timestamp: time.Now(),
                })
            }
        },
    })

    nodeStates := &sync.Map{}
    nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        UpdateFunc: func(oldObj, newObj interface{}) {
            oldNode := oldObj.(*corev1.Node)
            newNode := newObj.(*corev1.Node)
            w.handleNodeUpdate(oldNode, newNode)
        },
    })

    logger.Info("watcher event handlers registered")
    <-ctx.Done()
    logger.Info("watcher stopped")
}
```

**Note:** The cache is already started by the manager, so we don't need to start it again.

---

## Fix #2: getLogs RESTClient (15 min)

### Current Issue
```
internal\executor\builtins.go:99: g.client.RESTClient undefined
```

### Root Cause
controller-runtime client doesn't expose RESTClient() for raw K8s API access.

### Solution: Add Typed Clientset

#### Step 1: Update Executor Constructor
**File:** `internal/executor/executor.go`

Add typed clientset parameter:

```go
import (
    "k8s.io/client-go/kubernetes"
)

type Runner struct {
    registry      *Registry
    typedClientset kubernetes.Interface
}

func NewRunner(c client.Client, typedClientset kubernetes.Interface) *Runner {
    r := &Runner{
        registry:       newRegistry(),
        typedClientset: typedClientset,
    }
    registerBuiltins(r.registry, c, typedClientset)
    return r
}
```

#### Step 2: Update registerBuiltins
**File:** `internal/executor/builtins.go`

```go
func registerBuiltins(r *Registry, c client.Client, typedClientset kubernetes.Interface) {
    r.Register(&getPods{client: c})
    r.Register(&getLogs{client: c, typedClientset: typedClientset})
    r.Register(&describeNode{client: c})
}
```

#### Step 3: Update getLogs Implementation
**File:** `internal/executor/builtins.go`

```go
import (
    corev1 "k8s.io/api/core/v1"
    "k8s.io/client-go/kubernetes"
)

type getLogs struct {
    client         client.Client
    typedClientset kubernetes.Interface
}

func (g *getLogs) Run(ctx context.Context, params map[string]any) (string, error) {
    podName, ok := params["name"].(string)
    if !ok || podName == "" {
        return "", fmt.Errorf("missing required param: name")
    }

    ns, ok := params["namespace"].(string)
    if !ok || ns == "" {
        ns = "default"
    }

    // Get pod to find first container if not specified
    var pod corev1.Pod
    if err := g.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: podName}, &pod); err != nil {
        return "", fmt.Errorf("get pod: %w", err)
    }

    if len(pod.Spec.Containers) == 0 {
        return "", fmt.Errorf("pod has no containers")
    }

    container := pod.Spec.Containers[0].Name
    if c, ok := params["container"].(string); ok && c != "" {
        container = c
    }

    tailLines := int64(200)
    if t, ok := params["tail"].(float64); ok && t > 0 {
        tailLines = int64(t)
    }

    // Use typed clientset for log streaming
    req := g.typedClientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
        Container: container,
        TailLines: &tailLines,
    })

    stream, err := req.Stream(ctx)
    if err != nil {
        return "", fmt.Errorf("stream logs: %w", err)
    }
    defer stream.Close()

    var buf bytes.Buffer
    if _, err := io.Copy(&buf, stream); err != nil {
        return "", fmt.Errorf("read logs: %w", err)
    }

    logs := buf.String()
    if logs == "" {
        return fmt.Sprintf("No logs found for pod %s/%s container %s", ns, podName, container), nil
    }

    return fmt.Sprintf("Logs for pod %s/%s container %s (last %d lines):\n\n%s",
        ns, podName, container, tailLines, logs), nil
}
```

#### Step 4: Update Controller to Pass Typed Clientset
**File:** `internal/controller/reconciler.go`

Add to imports:
```go
import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)
```

Add field to Reconciler:
```go
type Reconciler struct {
    client.Client
    Scheme         *runtime.Scheme
    TypedClientset kubernetes.Interface
    cancels        sync.Map
}
```

Update buildAgent:
```go
exec := executor.NewRunner(r.Client, r.TypedClientset)
```

Update SetupWithManager in cmd/agent/main.go:
```go
// Create typed clientset
config := ctrl.GetConfigOrDie()
typedClientset, err := kubernetes.NewForConfig(config)
if err != nil {
    setupLog.Error(err, "unable to create typed clientset")
    os.Exit(1)
}

if err := (&controller.Reconciler{
    Client:         mgr.GetClient(),
    Scheme:         mgr.GetScheme(),
    TypedClientset: typedClientset,
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "KiteAgent")
    os.Exit(1)
}
```

---

## Testing Checklist

### Build Test
```bash
cd d:\Kite
go build ./...
```

Expected: No errors

### Unit Test (Optional)
```bash
go test ./internal/...
```

### Integration Test (Requires Cluster)
```bash
# 1. Start agent locally
go run cmd/agent/main.go

# 2. Create test namespace + secret
kubectl create namespace kite-system
kubectl create secret generic kite-llm-secret \
  --from-literal=ANTHROPIC_API_KEY=your-key-here \
  -n kite-system

# 3. Apply CRD and RBAC
kubectl apply -f deploy/manifests/kite-agent-crd.yaml
kubectl apply -f deploy/manifests/rbac.yaml

# 4. Apply sample KiteAgent
kubectl apply -f deploy/manifests/sample-kiteagent.yaml

# 5. Create crasher pod to trigger events
kubectl run crasher --image=busybox --restart=Always -- sh -c "exit 1"

# 6. Monitor agent logs
# Should see:
# - "watcher started"
# - "agent started"
# - Event detection for crasher pod
# - LLM call with cluster snapshot
# - Action execution (get_pods, get_logs)
# - SQLite persistence
```

---

## Validation Criteria

### ✅ Phase 1 Complete When:
1. [ ] `go build ./...` succeeds
2. [ ] Agent starts without errors
3. [ ] Watcher detects pod crash events
4. [ ] Snapshot captures cluster state correctly
5. [ ] LLM returns action plan with rationale
6. [ ] Executor runs get_pods successfully
7. [ ] Executor streams logs via get_logs
8. [ ] Executor describes nodes via describe_node
9. [ ] SQLite store persists events + actions
10. [ ] Debounce window coalesces burst events

---

## Estimated Timeline

| Task | Time | Cumulative |
|------|------|------------|
| Fix #1: Observer watch → informers | 30 min | 30 min |
| Fix #2: getLogs → typed clientset | 15 min | 45 min |
| Build + fix compilation errors | 10 min | 55 min |
| Deploy + test crasher detection | 15 min | 70 min |
| Verify full e2e flow | 10 min | 80 min |
| **Total** | **80 min** | **1h 20min** |

---

## Success Metrics

### Before Fixes
- ❌ Code doesn't compile
- ❌ Can't run agent
- ❌ No cluster observation

### After Fixes
- ✅ Clean build
- ✅ Agent runs in-cluster
- ✅ Detects pod crashes, OOM kills, node issues
- ✅ LLM reasons about problems
- ✅ Can fetch logs and describe resources
- ✅ Full audit trail in SQLite

---

## Resources

**Files to modify:**
1. `cmd/agent/main.go` - Add typed clientset creation
2. `internal/controller/reconciler.go` - Pass typed clientset + cache
3. `internal/observer/observer.go` - Accept cache instead of client
4. `internal/observer/watcher.go` - Use informers instead of Watch()
5. `internal/executor/executor.go` - Accept typed clientset
6. `internal/executor/builtins.go` - Use typed clientset for logs

**Total files:** 6  
**Total changes:** ~100 lines modified/added

---

## Alternative: Quick Workaround

If you want to test business logic without fixing APIs:

### Mock the Observer
Comment out watcher in `internal/observer/observer.go`:
```go
func (o *Observer) Start(ctx context.Context) {
    // go o.watcher.start(ctx)  // Disabled temporarily
    go o.poller.start(ctx)
}
```

### Mock getLogs
Return fake logs in `internal/executor/builtins.go`:
```go
func (g *getLogs) Run(ctx context.Context, params map[string]any) (string, error) {
    return "Mock logs: Container is crash-looping due to OOMKill", nil
}
```

This lets you test:
- ✅ Poller-driven events
- ✅ Snapshot generation
- ✅ LLM reasoning
- ✅ Action execution (except get_logs)
- ✅ SQLite persistence

---

**Created:** 2026-01-06  
**Phase:** Phase 1 Completion  
**Priority:** High  
**Complexity:** Low (API changes only, no logic changes)
