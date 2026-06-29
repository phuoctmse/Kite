package executor

import (
	"context"
	"fmt"
)

// Action is an executable capability the agent may invoke.
type Action interface {
	Name() string
	Run(ctx context.Context, params map[string]any) (string, error)
}

type Registry struct {
	actions map[string]Action
}

func newRegistry() *Registry {
	return &Registry{actions: make(map[string]Action)}
}

// Register adds an action to the registry. Overwrites if name already exists.
func (r *Registry) Register(a Action) {
	r.actions[a.Name()] = a
}

func (r *Registry) Get(name string) (Action, error) {
	a, ok := r.actions[name]
	if !ok {
		return nil, fmt.Errorf("action %q not registered", name)
	}
	return a, nil
}
