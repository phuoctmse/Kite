package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func registerBuiltins(r *Registry, c client.Client, typedClientset kubernetes.Interface) {
	r.Register(&getPods{client: c})
	r.Register(&getLogs{client: c, typedClientset: typedClientset})
	r.Register(&describeNode{client: c})
}

type getPods struct{ client client.Client }

func (g *getPods) Name() string { return "get_pods" }

func (g *getPods) Run(ctx context.Context, params map[string]any) (string, error) {
	ns, ok := params["namespace"].(string)
	if !ok || ns == "" {
		ns = "default"
	}

	var podList corev1.PodList
	if err := g.client.List(ctx, &podList, client.InNamespace(ns)); err != nil {
		return "", fmt.Errorf("list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Sprintf("No pods found in namespace %s", ns), nil
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("Pods in namespace %s:\n\n", ns))
	buf.WriteString("NAME                                 STATUS      RESTARTS   AGE\n")
	buf.WriteString("-------------------------------------------------------------------\n")

	for _, pod := range podList.Items {
		status := string(pod.Status.Phase)
		restarts := int32(0)
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}

		age := "unknown"
		if !pod.CreationTimestamp.IsZero() {
			age = pod.CreationTimestamp.Time.String()
		}

		buf.WriteString(fmt.Sprintf("%-36s %-10s %-10d %s\n",
			pod.Name, status, restarts, age))
	}

	return buf.String(), nil
}

type getLogs struct {
	client         client.Client
	typedClientset kubernetes.Interface
}

func (g *getLogs) Name() string { return "get_logs" }

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

type describeNode struct{ client client.Client }

func (g *describeNode) Name() string { return "describe_node" }

func (g *describeNode) Run(ctx context.Context, params map[string]any) (string, error) {
	nodeName, ok := params["name"].(string)
	if !ok || nodeName == "" {
		return "", fmt.Errorf("missing required param: name")
	}

	var node corev1.Node
	if err := g.client.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
		return "", fmt.Errorf("get node: %w", err)
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("Node: %s\n\n", node.Name))

	// Conditions
	buf.WriteString("Conditions:\n")
	for _, cond := range node.Status.Conditions {
		buf.WriteString(fmt.Sprintf("  %s: %s", cond.Type, cond.Status))
		if cond.Message != "" {
			buf.WriteString(fmt.Sprintf(" (%s)", cond.Message))
		}
		buf.WriteString("\n")
	}

	// Capacity
	buf.WriteString("\nCapacity:\n")
	for k, v := range node.Status.Capacity {
		buf.WriteString(fmt.Sprintf("  %s: %s\n", k, v.String()))
	}

	// Allocatable
	buf.WriteString("\nAllocatable:\n")
	for k, v := range node.Status.Allocatable {
		buf.WriteString(fmt.Sprintf("  %s: %s\n", k, v.String()))
	}

	// Node Info
	buf.WriteString("\nNode Info:\n")
	buf.WriteString(fmt.Sprintf("  OS: %s\n", node.Status.NodeInfo.OperatingSystem))
	buf.WriteString(fmt.Sprintf("  Architecture: %s\n", node.Status.NodeInfo.Architecture))
	buf.WriteString(fmt.Sprintf("  Container Runtime: %s\n", node.Status.NodeInfo.ContainerRuntimeVersion))
	buf.WriteString(fmt.Sprintf("  Kubelet Version: %s\n", node.Status.NodeInfo.KubeletVersion))

	return buf.String(), nil
}
