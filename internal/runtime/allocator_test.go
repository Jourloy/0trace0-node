package runtime

import (
	"testing"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func TestAssignPortsKeepsExistingAndAllocatesMissing(t *testing.T) {
	protocol := "trojan"
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			"inbound": {
				{ID: "a", Name: "Trojan", Protocol: &protocol},
				{ID: "b", Name: "VLESS", Protocol: &protocol},
			},
		},
	}

	assigned, err := AssignPorts(bundle, map[string]int{"a": 22222}, 22000, 22999)
	if err != nil {
		t.Fatalf("AssignPorts returned error: %v", err)
	}
	if assigned["a"] != 22222 {
		t.Fatalf("expected existing port to be preserved, got %d", assigned["a"])
	}
	if assigned["b"] == 0 {
		t.Fatalf("expected allocated port for second inbound")
	}
	if assigned["b"] == assigned["a"] {
		t.Fatalf("expected unique ports")
	}
}
