package executor

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(objs ...client.Object) client.Client {
	return fakeclient.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(objs...).Build()
}

func int32Ptr(v int32) *int32 { return &v }

// ─── scale_deployment ────────────────────────────────────────────────────────

func TestScaleDeployment_RejectsReplicasAboveMax(t *testing.T) {
	c := newFakeClient(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	})
	action := &scaleDeployment{client: c}

	_, err := action.Run(context.Background(), map[string]any{
		"name": "web", "namespace": "default", "replicas": float64(51),
	})
	if err == nil {
		t.Fatal("expected error for replicas above safe max (50), got nil")
	}
}

func TestScaleDeployment_RejectsNegativeReplicas(t *testing.T) {
	c := newFakeClient(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	})
	action := &scaleDeployment{client: c}

	_, err := action.Run(context.Background(), map[string]any{
		"name": "web", "namespace": "default", "replicas": float64(-1),
	})
	if err == nil {
		t.Fatal("expected error for negative replicas, got nil")
	}
}

func TestScaleDeployment_PatchesReplicasWithinRange(t *testing.T) {
	c := newFakeClient(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	})
	action := &scaleDeployment{client: c}

	if _, err := action.Run(context.Background(), map[string]any{
		"name": "web", "namespace": "default", "replicas": float64(7),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var deploy appsv1.Deployment
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "web"}, &deploy); err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas != 7 {
		t.Fatalf("expected replicas=7, got %v", deploy.Spec.Replicas)
	}
}

func TestScaleDeployment_DryRunDoesNotPatch(t *testing.T) {
	c := newFakeClient(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	})
	action := &scaleDeployment{client: c}

	if _, err := action.Run(context.Background(), map[string]any{
		"name": "web", "namespace": "default", "replicas": float64(7), "dry_run": true,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var deploy appsv1.Deployment
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "web"}, &deploy); err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas != 3 {
		t.Fatalf("dry_run must not patch cluster state, got replicas=%v", deploy.Spec.Replicas)
	}
}

// ─── restart_pod ─────────────────────────────────────────────────────────────

func TestRestartPod_RefusesStandalonePod(t *testing.T) {
	c := newFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "default"},
	})
	action := &restartPod{client: c}

	if _, err := action.Run(context.Background(), map[string]any{"name": "standalone", "namespace": "default"}); err == nil {
		t.Fatal("expected error refusing to delete standalone pod, got nil")
	}

	var pod corev1.Pod
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "standalone"}, &pod); err != nil {
		t.Fatalf("pod should still exist after refused restart: %v", err)
	}
}

func TestRestartPod_DeletesOwnedPod(t *testing.T) {
	c := newFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "web-abc123", APIVersion: "apps/v1", UID: "test-uid"},
			},
		},
	})
	action := &restartPod{client: c}

	if _, err := action.Run(context.Background(), map[string]any{"name": "owned", "namespace": "default"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var pod corev1.Pod
	err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "owned"}, &pod)
	if err == nil {
		t.Fatal("expected owned pod to be deleted, but it still exists")
	}
}

func TestRestartPod_DryRunDoesNotDelete(t *testing.T) {
	c := newFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "web-abc123", APIVersion: "apps/v1", UID: "test-uid"},
			},
		},
	})
	action := &restartPod{client: c}

	if _, err := action.Run(context.Background(), map[string]any{
		"name": "owned", "namespace": "default", "dry_run": true,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var pod corev1.Pod
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "owned"}, &pod); err != nil {
		t.Fatalf("dry_run must not delete pod: %v", err)
	}
}

// ─── cordon_node / uncordon_node ──────────────────────────────────────────────

func TestCordonNode_PatchesUnschedulable(t *testing.T) {
	c := newFakeClient(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: false},
	})
	action := &cordonNode{client: c}

	if _, err := action.Run(context.Background(), map[string]any{"name": "node-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var node corev1.Node
	if err := c.Get(context.Background(), client.ObjectKey{Name: "node-1"}, &node); err != nil {
		t.Fatalf("get node: %v", err)
	}
	if !node.Spec.Unschedulable {
		t.Fatal("expected node to be cordoned (unschedulable=true)")
	}
}

func TestCordonNode_IdempotentWhenAlreadyCordoned(t *testing.T) {
	c := newFakeClient(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	})
	action := &cordonNode{client: c}

	if _, err := action.Run(context.Background(), map[string]any{"name": "node-1"}); err != nil {
		t.Fatalf("expected no error re-cordoning an already-cordoned node, got: %v", err)
	}
}

func TestUncordonNode_PatchesSchedulable(t *testing.T) {
	c := newFakeClient(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	})
	action := &uncordonNode{client: c}

	if _, err := action.Run(context.Background(), map[string]any{"name": "node-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var node corev1.Node
	if err := c.Get(context.Background(), client.ObjectKey{Name: "node-1"}, &node); err != nil {
		t.Fatalf("get node: %v", err)
	}
	if node.Spec.Unschedulable {
		t.Fatal("expected node to be uncordoned (unschedulable=false)")
	}
}

func TestUncordonNode_IdempotentWhenAlreadyUncordoned(t *testing.T) {
	c := newFakeClient(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: false},
	})
	action := &uncordonNode{client: c}

	if _, err := action.Run(context.Background(), map[string]any{"name": "node-1"}); err != nil {
		t.Fatalf("expected no error re-uncordoning an already-schedulable node, got: %v", err)
	}
}
