package snapshot

import (
	"context"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EventSummary struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Resource  string `json:"resource"`
	Message   string `json:"message"`
}

type Summary struct {
	TotalPods    int            `json:"totalPods"`
	RunningPods  int            `json:"runningPods"`
	FailingPods  int            `json:"failingPods"`
	TotalNodes   int            `json:"totalNodes"`
	ReadyNodes   int            `json:"readyNodes"`
	RecentEvents []EventSummary `json:"recentEvents"`
}

type Snapshot struct {
	summary Summary
}

type Snapshotter struct {
	client client.Client
}

func New(c client.Client) *Snapshotter {
	return &Snapshotter{client: c}
}

// Build queries the cluster and returns a Snapshot ready for rendering.
func (s *Snapshotter) Build(ctx context.Context, namespaces []string) (*Snapshot, error) {
	// TODO: list Pods across namespaces; count Running vs non-Running
	// TODO: list Nodes; count Ready vs not-Ready
	// TODO: list recent Warning Events
	return &Snapshot{summary: Summary{}}, nil
}

// JSON returns a compact structured representation for LLM tool calls.
func (s *Snapshot) JSON() ([]byte, error) {
	return json.Marshal(s.summary)
}

// Markdown returns a narrative description for the LLM system prompt context block.
func (s *Snapshot) Markdown() string {
	m := s.summary
	return fmt.Sprintf(
		"# Cluster Snapshot\n\n"+
			"**Pods:** %d total, %d running, %d failing\n"+
			"**Nodes:** %d total, %d ready\n"+
			"**Recent events:** %d\n",
		m.TotalPods, m.RunningPods, m.FailingPods,
		m.TotalNodes, m.ReadyNodes,
		len(m.RecentEvents),
	)
}
