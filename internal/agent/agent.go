package agent

import (
	"context"
	"sync"
	"time"

	"github.com/kite-io/kite/api/v1alpha1"
	"github.com/kite-io/kite/internal/brain"
	"github.com/kite-io/kite/internal/event"
	"github.com/kite-io/kite/internal/executor"
	"github.com/kite-io/kite/internal/observer"
	"github.com/kite-io/kite/internal/snapshot"
	"github.com/kite-io/kite/internal/store"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	mailboxSize    = 64
	debounceWindow = 500 * time.Millisecond
)

type Agent struct {
	mailbox     chan event.Event
	config      *v1alpha1.KiteAgentSpec
	brain       brain.Provider
	executor    *executor.Runner
	observer    *observer.Observer
	store       store.Store
	snapshotter *snapshot.Snapshotter
	debounce    map[string]time.Time
	mu          sync.Mutex
}

func New(
	cfg *v1alpha1.KiteAgentSpec,
	b brain.Provider,
	exec *executor.Runner,
	obs *observer.Observer,
	st store.Store,
	snap *snapshot.Snapshotter,
) *Agent {
	return &Agent{
		mailbox:     make(chan event.Event, mailboxSize),
		config:      cfg,
		brain:       b,
		executor:    exec,
		observer:    obs,
		store:       st,
		snapshotter: snap,
		debounce:    make(map[string]time.Time),
	}
}

// SetObserver wires the observer after construction, allowing the caller to
// create the observer with this agent's mailbox channel.
func (a *Agent) SetObserver(obs *observer.Observer) {
	a.observer = obs
}

// Mailbox returns the send-only channel the observer uses to deliver events.
func (a *Agent) Mailbox() chan<- event.Event {
	return a.mailbox
}

// Run is the agent's main event loop. Call it in a goroutine; cancel ctx to stop.
func (a *Agent) Run(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("agent started")

	a.observer.Start(ctx)

	for {
		select {
		case e := <-a.mailbox:
			a.handleEvent(ctx, e)
		case <-ctx.Done():
			logger.Info("agent stopping")
			return
		}
	}
}

// shouldProcess returns false when an event of the same Kind+Namespace arrived
// within debounceWindow, coalescing burst events before they reach the LLM.
func (a *Agent) shouldProcess(e event.Event) bool {
	key := string(e.Kind) + "/" + e.Namespace
	a.mu.Lock()
	defer a.mu.Unlock()
	if last, ok := a.debounce[key]; ok && time.Since(last) < debounceWindow {
		return false
	}
	a.debounce[key] = time.Now()
	return true
}

func (a *Agent) handleEvent(ctx context.Context, e event.Event) {
	logger := log.FromContext(ctx).WithValues("kind", e.Kind, "namespace", e.Namespace)

	if !a.shouldProcess(e) {
		logger.V(1).Info("event dropped", "reason", "debounced")
		return
	}

	snap, err := a.snapshotter.Build(ctx, a.config.Namespaces)
	if err != nil {
		logger.Error(err, "snapshot failed")
		return
	}

	snapJSON, err := snap.JSON()
	if err != nil {
		logger.Error(err, "snapshot marshal failed")
		return
	}

	var history []brain.Turn
	if a.store != nil {
		history, _ = a.store.RecentHistory(20)
	}

	prompt := brain.Prompt{
		SystemContext:  snap.Markdown(),
		SnapshotJSON:   snapJSON,
		History:        history,
		AvailableTools: a.buildToolDefs(),
	}

	plan, err := a.brain.Decide(ctx, prompt)
	if err != nil {
		logger.Error(err, "brain decision failed")
		return
	}

	results, err := a.executor.Execute(ctx, plan, a.config.AllowedActions)
	if err != nil {
		logger.Error(err, "executor failed")
		return
	}

	// Persist after action; non-fatal if write fails
	if a.store != nil {
		_ = a.store.WriteEvent(string(e.Kind), e.Namespace, e.Resource, e.Timestamp)
		for _, r := range results {
			errStr := ""
			if r.Err != nil {
				errStr = r.Err.Error()
			}
			_ = a.store.WriteAction(store.ActionResult{
				ActionName: r.ActionName,
				Output:     r.Output,
				Err:        errStr,
				Timestamp:  e.Timestamp,
			})
		}
	}
}

// builtinToolDefs are always exposed to the brain regardless of AllowedActions.
var builtinToolDefs = []brain.ToolDef{
	{Name: "get_cluster_snapshot", Description: "Get compressed cluster health snapshot — call this first"},
	{Name: "get_pod_logs", Description: "Get last N log lines from a pod. Params: name, namespace, tail int"},
	{Name: "get_pod_events", Description: "Get recent K8s events for a specific pod. Params: name, namespace"},
	{Name: "propose_action", Description: "Propose an action for the executor. Call this last after reasoning"},
}

func (a *Agent) buildToolDefs() []brain.ToolDef {
	defs := make([]brain.ToolDef, 0, len(builtinToolDefs)+len(a.config.AllowedActions))
	defs = append(defs, builtinToolDefs...)
	for _, ac := range a.config.AllowedActions {
		defs = append(defs, brain.ToolDef{
			Name:        ac.Name,
			Description: ac.Description,
		})
	}
	return defs
}
