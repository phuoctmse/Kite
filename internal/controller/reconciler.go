package controller

import (
	"context"
	"sync"
	"time"

	"github.com/kite-io/kite/api/v1alpha1"
	"github.com/kite-io/kite/internal/agent"
	"github.com/kite-io/kite/internal/executor"
	"github.com/kite-io/kite/internal/observer"
	"github.com/kite-io/kite/internal/snapshot"
	"github.com/kite-io/kite/internal/store"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Reconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	cancels sync.Map // key: types.NamespacedName → context.CancelFunc
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ka v1alpha1.KiteAgent
	if err := r.Get(ctx, req.NamespacedName, &ka); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Stop any existing agent goroutine for this CR before (re)starting.
	r.stopAgent(req.NamespacedName)

	if ka.DeletionTimestamp != nil {
		logger.Info("KiteAgent deleted, agent stopped", "name", req.Name)
		return ctrl.Result{}, nil
	}

	agentCtx, cancel := context.WithCancel(context.Background())
	r.cancels.Store(req.NamespacedName, cancel)

	a, err := r.buildAgent(agentCtx, &ka)
	if err != nil {
		cancel()
		logger.Error(err, "failed to build agent")
		return ctrl.Result{}, err
	}

	go a.Run(agentCtx)
	logger.Info("KiteAgent started", "name", req.Name)
	return ctrl.Result{}, nil
}

func (r *Reconciler) stopAgent(key types.NamespacedName) {
	if v, ok := r.cancels.LoadAndDelete(key); ok {
		v.(context.CancelFunc)()
	}
}

func (r *Reconciler) buildAgent(ctx context.Context, ka *v1alpha1.KiteAgent) (*agent.Agent, error) {
	// TODO: resolve LLM provider from ka.Spec.LLMProvider + ka.Spec.LLMSecret

	pollInterval := 60 * time.Second
	if ka.Spec.PollInterval.Duration > 0 {
		pollInterval = ka.Spec.PollInterval.Duration
	}

	// TODO: open store at a deterministic per-CR path (e.g. /data/<namespace>-<name>.db)
	var st store.Store

	snap := snapshot.New(r.Client)
	exec := executor.NewRunner(r.Client)

	// TODO: pass real brain.Provider once LLM adapter is resolved
	a := agent.New(&ka.Spec, nil, exec, nil, st, snap)

	// Wire observer to this agent's mailbox after construction.
	obs := observer.New(a.Mailbox(), r.Client, pollInterval)
	a.SetObserver(obs)

	return a, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KiteAgent{}).
		Complete(r)
}
