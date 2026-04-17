package nodeagent

import (
	"testing"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func TestBundleChecksumIsDeterministic(t *testing.T) {
	trojan := "trojan"
	bundle := controlapi.ConfigBundle{
		NodeID:      "node-1",
		GeneratedAt: time.Unix(1700000000, 0).UTC(),
		Resources: map[string][]controlapi.ManagedResource{
			"inbound": {
				{
					ID:       "inbound-1",
					Name:     "Inbound",
					Slug:     "inbound",
					Protocol: &trojan,
					Spec:     map[string]any{"listen": "0.0.0.0"},
				},
			},
		},
	}
	ports := map[string]int{"inbound-1": 24443}

	first := bundleChecksum(bundle, ports)
	second := bundleChecksum(bundle, ports)
	if first != second {
		t.Fatalf("expected deterministic checksum, got %s and %s", first, second)
	}

	ports["inbound-1"] = 24444
	if bundleChecksum(bundle, ports) == first {
		t.Fatalf("expected checksum to change after payload mutation")
	}
}
