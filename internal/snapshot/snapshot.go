package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
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
	summary := Summary{}

	// List all pods across specified namespaces (or cluster-wide if empty)
	var podList corev1.PodList
	if len(namespaces) > 0 {
		// For multiple namespaces, we'll list all and filter
		if err := s.client.List(ctx, &podList); err != nil {
			return nil, fmt.Errorf("list pods: %w", err)
		}
		// Filter to allowed namespaces
		nsMap := make(map[string]bool)
		for _, ns := range namespaces {
			nsMap[ns] = true
		}
		filtered := make([]corev1.Pod, 0)
		for _, pod := range podList.Items {
			if nsMap[pod.Namespace] {
				filtered = append(filtered, pod)
			}
		}
		podList.Items = filtered
	} else {
		// Empty namespaces = watch all
		if err := s.client.List(ctx, &podList); err != nil {
			return nil, fmt.Errorf("list all pods: %w", err)
		}
	}

	summary.TotalPods = len(podList.Items)
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			summary.RunningPods++
		} else if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodUnknown {
			summary.FailingPods++
		} else if pod.Status.Phase == corev1.PodPending {
			// Check if it's stuck pending (container waiting with error)
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && (cs.State.Waiting.Reason == "CrashLoopBackOff" ||
					cs.State.Waiting.Reason == "ImagePullBackOff" ||
					cs.State.Waiting.Reason == "ErrImagePull") {
					summary.FailingPods++
					break
				}
			}
		}
	}

	// List nodes and count ready vs not ready
	var nodeList corev1.NodeList
	if err := s.client.List(ctx, &nodeList); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	summary.TotalNodes = len(nodeList.Items)
	for _, node := range nodeList.Items {
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				summary.ReadyNodes++
				break
			}
		}
	}

	// Query recent Warning events (last 50, deduplicate)
	var eventList corev1.EventList
	if err := s.client.List(ctx, &eventList); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	// Filter to Warning type, sort by timestamp, take recent 50
	warnings := make([]corev1.Event, 0)
	for _, ev := range eventList.Items {
		if ev.Type == corev1.EventTypeWarning {
			warnings = append(warnings, ev)
		}
	}

	// Sort by last timestamp (most recent first)
	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].LastTimestamp.After(warnings[j].LastTimestamp.Time)
	})

	// Take up to 50 most recent, deduplicate by resource
	seen := make(map[string]bool)
	for _, ev := range warnings {
		if len(summary.RecentEvents) >= 50 {
			break
		}
		key := ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Namespace + "/" + ev.InvolvedObject.Name
		if !seen[key] {
			seen[key] = true
			summary.RecentEvents = append(summary.RecentEvents, EventSummary{
				Kind:      ev.InvolvedObject.Kind,
				Namespace: ev.InvolvedObject.Namespace,
				Resource:  ev.InvolvedObject.Name,
				Message:   ev.Message,
			})
		}
	}

	return &Snapshot{summary: summary}, nil
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
