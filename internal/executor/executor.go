package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/kite-io/kite/api/v1alpha1"
	"github.com/kite-io/kite/internal/brain"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultMutationCooldown is the minimum interval between two mutating
// actions on the same (action, namespace, resource) tuple.
const defaultMutationCooldown = 5 * time.Minute

type Result struct {
	ActionName string
	Output     string
	Err        error
}

type Runner struct {
	registry       *Registry
	typedClientset kubernetes.Interface
	cooldown       *MutationCooldown
}

func NewRunner(c client.Client, typedClientset kubernetes.Interface) *Runner {
	r := &Runner{
		registry:       newRegistry(),
		typedClientset: typedClientset,
		cooldown:       NewMutationCooldown(),
	}
	registerBuiltins(r.registry, c, typedClientset)
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

		if _, mutating := mutationActionNames[req.Name]; mutating && !boolParam(req.Params, "dry_run") {
			name, _ := req.Params["name"].(string)
			namespace, _ := req.Params["namespace"].(string)
			if !r.cooldown.Allow(req.Name, namespace, name, defaultMutationCooldown) {
				results = append(results, Result{
					ActionName: req.Name,
					Err:        fmt.Errorf("action %q on %s/%s skipped: within cooldown window", req.Name, namespace, name),
				})
				continue
			}
		}

		out, runErr := action.Run(ctx, req.Params)
		results = append(results, Result{ActionName: req.Name, Output: out, Err: runErr})
	}
	return results, nil
}
