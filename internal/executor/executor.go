package executor

import (
	"context"
	"fmt"

	"github.com/kite-io/kite/api/v1alpha1"
	"github.com/kite-io/kite/internal/brain"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Result struct {
	ActionName string
	Output     string
	Err        error
}

type Runner struct {
	registry *Registry
}

func NewRunner(c client.Client) *Runner {
	r := &Runner{registry: newRegistry()}
	registerBuiltins(r.registry, c)
	return r
}

// Register adds a user-defined action plugin to the runner at runtime.
func (r *Runner) Register(a Action) {
	r.registry.Register(a)
}

// Execute runs all actions in plan, enforcing the allowedActions whitelist before each call.
func (r *Runner) Execute(ctx context.Context, plan brain.Plan, allowed []v1alpha1.ActionSpec) ([]Result, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allowedSet[a.Name] = struct{}{}
	}

	results := make([]Result, 0, len(plan.Actions))
	for _, req := range plan.Actions {
		if _, ok := allowedSet[req.Name]; !ok {
			return nil, fmt.Errorf("action %q blocked by whitelist", req.Name)
		}
		action, err := r.registry.Get(req.Name)
		if err != nil {
			return nil, err
		}
		out, runErr := action.Run(ctx, req.Params)
		results = append(results, Result{ActionName: req.Name, Output: out, Err: runErr})
	}
	return results, nil
}
