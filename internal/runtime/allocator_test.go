package runtime

import (
	"testing"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func TestAssignPortsIsDeterministic(t *testing.T) {
	protocol := "trojan"
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{ID: "b", Name: "Inbound B", Protocol: &protocol},
				{ID: "a", Name: "Inbound A", Protocol: &protocol},
			},
		},
	}

	first, err := AssignPorts(bundle)
	if err != nil {
		t.Fatalf("AssignPorts returned error: %v", err)
	}
	second, err := AssignPorts(bundle)
	if err != nil {
		t.Fatalf("AssignPorts returned error on repeat: %v", err)
	}

	if first["a"] != second["a"] || first["b"] != second["b"] {
		t.Fatalf("AssignPorts returned unstable ports: first=%v second=%v", first, second)
	}
	if first["a"] == first["b"] {
		t.Fatalf("AssignPorts returned duplicate internal ports: %v", first)
	}
	if first["a"] < internalInboundPortMin || first["a"] > internalInboundPortMax {
		t.Fatalf("port for a = %d, want %d-%d", first["a"], internalInboundPortMin, internalInboundPortMax)
	}
}

func TestAssignStatsPortIsDeterministic(t *testing.T) {
	firstUsed := map[int]struct{}{}
	first, err := AssignStatsPort("node-1:inbound-a", firstUsed)
	if err != nil {
		t.Fatalf("AssignStatsPort returned error: %v", err)
	}

	secondUsed := map[int]struct{}{}
	second, err := AssignStatsPort("node-1:inbound-a", secondUsed)
	if err != nil {
		t.Fatalf("AssignStatsPort returned error on repeat: %v", err)
	}

	if first != second {
		t.Fatalf("AssignStatsPort = %d, want deterministic %d", first, second)
	}
	if first < internalStatsPortMin || first > internalStatsPortMax {
		t.Fatalf("stats port = %d, want %d-%d", first, internalStatsPortMin, internalStatsPortMax)
	}
}
