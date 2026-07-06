package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kite-io/kite/api/v1alpha1"
	"github.com/kite-io/kite/internal/agent"
	"github.com/kite-io/kite/internal/brain"
	"github.com/kite-io/kite/internal/executor"
	"github.com/kite-io/kite/internal/observer"
	"github.com/kite-io/kite/internal/snapshot"
	"github.com/kite-io/kite/internal/store"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Reconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Cache          cache.Cache
	TypedClientset kubernetes.Interface
	cancels        sync.Map // key: types.NamespacedName → context.CancelFunc
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
	logger := log.FromContext(ctx)

	// Resolve LLM provider and API key from secret
	var brainProvider brain.Provider
	if ka.Spec.LLMSecret != "" {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Namespace: ka.Namespace,
			Name:      ka.Spec.LLMSecret,
		}
		if err := r.Get(ctx, secretKey, secret); err != nil {
			return nil, fmt.Errorf("get llm secret: %w", err)
		}

		var apiKey string
		var model string
		switch ka.Spec.LLMProvider {
		case "anthropic":
			apiKey = string(secret.Data["ANTHROPIC_API_KEY"])
			if apiKey == "" {
				return nil, fmt.Errorf("ANTHROPIC_API_KEY not found in secret %s", ka.Spec.LLMSecret)
			}
			model = string(secret.Data["ANTHROPIC_MODEL"])
			if model == "" {
				model = "claude-3-5-sonnet-20241022"
			}
			provider := brain.NewAnthropic(apiKey, model)
			if ka.Spec.LLMEndpoint != "" {
				provider.Endpoint = ka.Spec.LLMEndpoint
			}
			brainProvider = provider

		case "openai":
			apiKey = string(secret.Data["OPENAI_API_KEY"])
			if apiKey == "" {
				return nil, fmt.Errorf("OPENAI_API_KEY not found in secret %s", ka.Spec.LLMSecret)
			}
			model = string(secret.Data["OPENAI_MODEL"])
			if model == "" {
				model = "gpt-4"
			}
			endpoint := ka.Spec.LLMEndpoint
			if endpoint == "" {
				endpoint = "https://api.openai.com"
			}
			brainProvider = brain.NewOpenAI(apiKey, model, endpoint)

		case "ollama":
			// Ollama doesn't require API key
			model = string(secret.Data["OLLAMA_MODEL"])
			if model == "" {
				model = "llama2"
			}
			endpoint := ka.Spec.LLMEndpoint
			if endpoint == "" {
				endpoint = "http://localhost:11434"
			}
			brainProvider = brain.NewOllama(model, endpoint)

		default:
			return nil, fmt.Errorf("unsupported llm provider: %s", ka.Spec.LLMProvider)
		}
	} else {
		logger.Info("no LLM secret specified, agent will run without brain")
	}

	pollInterval := 60 * time.Second
	if ka.Spec.PollInterval.Duration > 0 {
		pollInterval = ka.Spec.PollInterval.Duration
	}

	// Open SQLite store at deterministic path
	storePath := fmt.Sprintf("/tmp/kite-%s-%s.db", ka.Namespace, ka.Name)
	st, err := store.Open(storePath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	logger.Info("opened store", "path", storePath)

	snap := snapshot.New(r.Client)
	exec := executor.NewRunner(r.Client, r.TypedClientset)

	a := agent.New(&ka.Spec, brainProvider, exec, nil, st, snap)

	// Wire observer to this agent's mailbox after construction.
	obs := observer.New(a.Mailbox(), r.Cache, pollInterval)
	a.SetObserver(obs)

	return a, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KiteAgent{}).
		Complete(r)
}
