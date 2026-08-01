package executor

import (
	"context"
	"testing"

	"github.com/kite-io/kite/api/v1alpha1"
	"github.com/kite-io/kite/internal/brain"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// countingAction is a test double registered under the real "scale_deployment"
// name so Execute's cooldown gating can be observed without a live cluster.
type countingAction struct{ calls int }

func (c *countingAction) Name() string { return "scale_deployment" }

func (c *countingAction) Run(ctx context.Context, params map[string]any) (string, error) {
	c.calls++
	return "ok", nil
}

func newTestRunner() *Runner {
	return NewRunner(fakeclient.NewClientBuilder().Build(), k8sfake.NewSimpleClientset())
}

func scalePlan(name, namespace string, dryRun bool) brain.Plan {
	return brain.Plan{Actions: []brain.ActionRequest{
		{Name: "scale_deployment", Params: map[string]any{
			"name": name, "namespace": namespace, "replicas": float64(3), "dry_run": dryRun,
		}},
	}}
}

func TestExecute_BlocksRepeatedMutationWithinCooldown(t *testing.T) {
	r := newTestRunner()
	fake := &countingAction{}
	r.Register(fake)

	allowed := []v1alpha1.ActionSpec{{Name: "scale_deployment"}}
	plan := scalePlan("web", "default", false)

	if _, err := r.Execute(context.Background(), plan, allowed); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected 1 call after first execute, got %d", fake.calls)
	}

	results, err := r.Execute(context.Background(), plan, allowed)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected action.Run NOT invoked on second call within cooldown, got %d total calls", fake.calls)
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("expected a cooldown error result, got %+v", results)
	}
}

func TestExecute_DryRunBypassesCooldown(t *testing.T) {
	r := newTestRunner()
	fake := &countingAction{}
	r.Register(fake)

	allowed := []v1alpha1.ActionSpec{{Name: "scale_deployment"}}
	plan := scalePlan("web", "default", true)

	for i := 0; i < 2; i++ {
		if _, err := r.Execute(context.Background(), plan, allowed); err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}
	if fake.calls != 2 {
		t.Fatalf("expected dry_run calls to bypass cooldown (2 calls), got %d", fake.calls)
	}
}

func TestExecute_DifferentResourceNotBlockedByCooldown(t *testing.T) {
	r := newTestRunner()
	fake := &countingAction{}
	r.Register(fake)

	allowed := []v1alpha1.ActionSpec{{Name: "scale_deployment"}}

	if _, err := r.Execute(context.Background(), scalePlan("web", "default", false), allowed); err != nil {
		t.Fatalf("execute web: %v", err)
	}
	if _, err := r.Execute(context.Background(), scalePlan("api", "default", false), allowed); err != nil {
		t.Fatalf("execute api: %v", err)
	}
	if fake.calls != 2 {
		t.Fatalf("expected cooldown to be scoped per-resource (2 calls), got %d", fake.calls)
	}
}
