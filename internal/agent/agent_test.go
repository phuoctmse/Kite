package agent

import (
	"testing"

	"github.com/kite-io/kite/api/v1alpha1"
)

// registeredActionNames mirrors what internal/executor actually registers
// (see internal/executor/builtins.go and mutations.go). Kept independent of
// the executor package so this test doesn't silently pass just because it
// imports the same (possibly also-wrong) constant.
var registeredActionNames = map[string]bool{
	"get_pods":         true,
	"get_logs":         true,
	"describe_node":    true,
	"scale_deployment": true,
	"restart_pod":      true,
	"cordon_node":      true,
	"uncordon_node":    true,
	"delete_pod":       true,
}

func TestBuildToolDefs_OnlyAdvertisesRegisteredActions(t *testing.T) {
	a := &Agent{config: &v1alpha1.KiteAgentSpec{
		AllowedActions: []v1alpha1.ActionSpec{
			{Name: "scale_deployment", Description: "scale it"},
		},
	}}

	for _, def := range a.buildToolDefs() {
		if !registeredActionNames[def.Name] {
			t.Errorf("buildToolDefs advertised tool %q, which the executor never registers", def.Name)
		}
	}
}
