package executor

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func registerBuiltins(r *Registry, c client.Client) {
	r.Register(&getPods{client: c})
	r.Register(&getLogs{client: c})
	r.Register(&describeNode{client: c})
}

type getPods struct{ client client.Client }

func (g *getPods) Name() string { return "get_pods" }
func (g *getPods) Run(ctx context.Context, params map[string]any) (string, error) {
	// TODO: list pods in params["namespace"] via g.client; format as text table
	ns, _ := params["namespace"].(string)
	_ = ns
	return "", fmt.Errorf("get_pods: not implemented")
}

type getLogs struct{ client client.Client }

func (g *getLogs) Name() string { return "get_logs" }
func (g *getLogs) Run(ctx context.Context, params map[string]any) (string, error) {
	// TODO: stream pod logs for params["pod"] in params["namespace"]; cap at 200 lines
	return "", fmt.Errorf("get_logs: not implemented")
}

type describeNode struct{ client client.Client }

func (g *describeNode) Name() string { return "describe_node" }
func (g *describeNode) Run(ctx context.Context, params map[string]any) (string, error) {
	// TODO: get node params["node"]; render conditions, capacity, allocatable
	return "", fmt.Errorf("describe_node: not implemented")
}
