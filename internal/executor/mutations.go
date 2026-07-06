package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// registerMutations adds Phase 2 write actions to the registry.
// These are only effective when listed in the KiteAgent's allowedActions.
func registerMutations(r *Registry, c client.Client) {
	r.Register(&scaleDeployment{client: c})
	r.Register(&restartPod{client: c})
	r.Register(&cordonNode{client: c})
	r.Register(&uncordonNode{client: c})
	r.Register(&deletePod{client: c})
}

// ─── scale_deployment ────────────────────────────────────────────────────────

// scaleDeployment adjusts the replica count of a Deployment.
// Params: name (string), namespace (string), replicas (float64), dry_run (bool)
type scaleDeployment struct{ client client.Client }

func (s *scaleDeployment) Name() string { return "scale_deployment" }

func (s *scaleDeployment) Run(ctx context.Context, params map[string]any) (string, error) {
	name, ns, err := requireNameNS(params)
	if err != nil {
		return "", err
	}

	replicasF, ok := params["replicas"].(float64)
	if !ok {
		return "", fmt.Errorf("missing required param: replicas (number)")
	}
	replicas := int32(replicasF)

	if replicas < 0 || replicas > 50 {
		return "", fmt.Errorf("replicas %d out of safe range [0, 50]", replicas)
	}

	dryRun := boolParam(params, "dry_run")

	var deploy appsv1.Deployment
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &deploy); err != nil {
		return "", fmt.Errorf("get deployment %s/%s: %w", ns, name, err)
	}

	previous := int32(0)
	if deploy.Spec.Replicas != nil {
		previous = *deploy.Spec.Replicas
	}

	if dryRun {
		return fmt.Sprintf("[DRY RUN] Would scale deployment %s/%s from %d → %d replicas",
			ns, name, previous, replicas), nil
	}

	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	if err := s.client.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return "", fmt.Errorf("patch deployment %s/%s: %w", ns, name, err)
	}

	return fmt.Sprintf("Scaled deployment %s/%s from %d → %d replicas", ns, name, previous, replicas), nil
}

// ─── restart_pod ─────────────────────────────────────────────────────────────

// restartPod deletes a pod so its controller recreates it.
// Params: name (string), namespace (string), dry_run (bool)
type restartPod struct{ client client.Client }

func (r *restartPod) Name() string { return "restart_pod" }

func (r *restartPod) Run(ctx context.Context, params map[string]any) (string, error) {
	name, ns, err := requireNameNS(params)
	if err != nil {
		return "", err
	}

	dryRun := boolParam(params, "dry_run")

	var pod corev1.Pod
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &pod); err != nil {
		return "", fmt.Errorf("get pod %s/%s: %w", ns, name, err)
	}

	// Safety: only restart pods owned by a controller (Deployment, ReplicaSet, etc.)
	// to avoid deleting standalone pods that won't be recreated.
	if len(pod.OwnerReferences) == 0 {
		return "", fmt.Errorf(
			"pod %s/%s has no owner — refusing to delete standalone pod (would not be recreated)",
			ns, name,
		)
	}

	if dryRun {
		return fmt.Sprintf("[DRY RUN] Would delete (restart) pod %s/%s (owner: %s %s)",
			ns, name, pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name), nil
	}

	if err := r.client.Delete(ctx, &pod); err != nil {
		return "", fmt.Errorf("delete pod %s/%s: %w", ns, name, err)
	}

	return fmt.Sprintf("Deleted pod %s/%s — controller will recreate it (owner: %s %s)",
		ns, name, pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name), nil
}

// ─── cordon_node ──────────────────────────────────────────────────────────────

// cordonNode marks a node as unschedulable so no new pods land on it.
// Params: name (string), dry_run (bool)
type cordonNode struct{ client client.Client }

func (c *cordonNode) Name() string { return "cordon_node" }

func (c *cordonNode) Run(ctx context.Context, params map[string]any) (string, error) {
	name, err := requireName(params)
	if err != nil {
		return "", err
	}

	dryRun := boolParam(params, "dry_run")

	var node corev1.Node
	if err := c.client.Get(ctx, client.ObjectKey{Name: name}, &node); err != nil {
		return "", fmt.Errorf("get node %s: %w", name, err)
	}

	if node.Spec.Unschedulable {
		return fmt.Sprintf("Node %s is already cordoned (unschedulable)", name), nil
	}

	if dryRun {
		return fmt.Sprintf("[DRY RUN] Would cordon node %s", name), nil
	}

	patch := []byte(`{"spec":{"unschedulable":true}}`)
	if err := c.client.Patch(ctx, &node, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return "", fmt.Errorf("patch node %s: %w", name, err)
	}

	return fmt.Sprintf("Cordoned node %s — no new pods will be scheduled on it", name), nil
}

// ─── uncordon_node ────────────────────────────────────────────────────────────

// uncordonNode re-enables scheduling on a previously cordoned node.
// Params: name (string), dry_run (bool)
type uncordonNode struct{ client client.Client }

func (u *uncordonNode) Name() string { return "uncordon_node" }

func (u *uncordonNode) Run(ctx context.Context, params map[string]any) (string, error) {
	name, err := requireName(params)
	if err != nil {
		return "", err
	}

	dryRun := boolParam(params, "dry_run")

	var node corev1.Node
	if err := u.client.Get(ctx, client.ObjectKey{Name: name}, &node); err != nil {
		return "", fmt.Errorf("get node %s: %w", name, err)
	}

	if !node.Spec.Unschedulable {
		return fmt.Sprintf("Node %s is already schedulable (not cordoned)", name), nil
	}

	if dryRun {
		return fmt.Sprintf("[DRY RUN] Would uncordon node %s", name), nil
	}

	patch := []byte(`{"spec":{"unschedulable":false}}`)
	if err := u.client.Patch(ctx, &node, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return "", fmt.Errorf("patch node %s: %w", name, err)
	}

	return fmt.Sprintf("Uncordoned node %s — scheduling re-enabled", name), nil
}

// ─── delete_pod ───────────────────────────────────────────────────────────────

// deletePod force-deletes a pod regardless of owner. Use when restart_pod is
// too conservative (e.g. stuck Terminating pods).
// Params: name (string), namespace (string), grace_period_seconds (float64), dry_run (bool)
type deletePod struct{ client client.Client }

func (d *deletePod) Name() string { return "delete_pod" }

func (d *deletePod) Run(ctx context.Context, params map[string]any) (string, error) {
	name, ns, err := requireNameNS(params)
	if err != nil {
		return "", err
	}

	dryRun := boolParam(params, "dry_run")

	gracePeriod := int64(30) // default
	if gp, ok := params["grace_period_seconds"].(float64); ok && gp >= 0 {
		gracePeriod = int64(gp)
	}

	var pod corev1.Pod
	if err := d.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &pod); err != nil {
		return "", fmt.Errorf("get pod %s/%s: %w", ns, name, err)
	}

	ownerInfo := "no owner (standalone)"
	if len(pod.OwnerReferences) > 0 {
		ownerInfo = fmt.Sprintf("owner: %s %s", pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name)
	}

	if dryRun {
		return fmt.Sprintf("[DRY RUN] Would delete pod %s/%s with grace period %ds (%s)",
			ns, name, gracePeriod, ownerInfo), nil
	}

	deleteOpts := &client.DeleteOptions{}
	deleteOpts.GracePeriodSeconds = &gracePeriod

	if err := d.client.Delete(ctx, &pod, deleteOpts); err != nil {
		return "", fmt.Errorf("delete pod %s/%s: %w", ns, name, err)
	}

	return fmt.Sprintf("Deleted pod %s/%s with grace period %ds (%s)",
		ns, name, gracePeriod, ownerInfo), nil
}

// ─── Cooldown tracker ────────────────────────────────────────────────────────

// MutationCooldown tracks the last time each mutation was applied per resource,
// preventing the agent from hammering the same resource repeatedly.
type MutationCooldown struct {
	entries map[string]time.Time
}

func NewMutationCooldown() *MutationCooldown {
	return &MutationCooldown{entries: make(map[string]time.Time)}
}

// Allow returns true if no mutation with this key has been applied within
// the given cooldown duration. Updates the timestamp when it returns true.
func (mc *MutationCooldown) Allow(action, namespace, resource string, cooldown time.Duration) bool {
	key := strings.Join([]string{action, namespace, resource}, "/")
	if last, ok := mc.entries[key]; ok && time.Since(last) < cooldown {
		return false
	}
	mc.entries[key] = time.Now()
	return true
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func requireName(params map[string]any) (string, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return "", fmt.Errorf("missing required param: name")
	}
	return name, nil
}

func requireNameNS(params map[string]any) (string, string, error) {
	name, err := requireName(params)
	if err != nil {
		return "", "", err
	}
	ns, _ := params["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	return name, ns, nil
}

func boolParam(params map[string]any, key string) bool {
	v, _ := params[key].(bool)
	return v
}
