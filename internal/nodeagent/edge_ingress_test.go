package nodeagent

import (
	"strings"
	"testing"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func TestBuildEdgeRoutePlanRejectsDuplicateTLSMarkers(t *testing.T) {
	protocol := "trojan"
	bundle := &controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{
					ID:        "in-1",
					Name:      "Ingress A",
					Protocol:  &protocol,
					IsEnabled: true,
					Spec: map[string]any{
						"serverName": "edge.example.com",
						"sni":        "edge.example.com",
						"streamSettings": map[string]any{
							"security": "tls",
						},
					},
				},
				{
					ID:        "in-2",
					Name:      "Ingress B",
					Protocol:  &protocol,
					IsEnabled: true,
					Spec: map[string]any{
						"serverName": "edge.example.com",
						"sni":        "edge.example.com",
						"streamSettings": map[string]any{
							"security": "tls",
						},
					},
				},
			},
		},
	}

	_, err := buildEdgeRoutePlan(bundle, map[string]int{"in-1": 21000, "in-2": 21001})
	if err == nil {
		t.Fatalf("buildEdgeRoutePlan returned nil, want duplicate marker error")
	}
	if !strings.Contains(err.Error(), "duplicate TLS ingress marker") {
		t.Fatalf("error = %v, want duplicate TLS marker", err)
	}
}

func TestRouteTCPIngressUsesControlAndSOCKS5Routes(t *testing.T) {
	service := &Service{
		edgePlan: edgeRoutePlan{
			HTTPPort:   21000,
			SOCKS5Port: 21001,
		},
	}

	control := service.routeTCPIngress([]byte("GET /api/v1/node/runtime HTTP/1.1\r\nHost: node\r\n\r\n"))
	if control != internalControlAPIAddr {
		t.Fatalf("control route = %q, want %q", control, internalControlAPIAddr)
	}

	socks := service.routeTCPIngress([]byte{0x05, 0x01, 0x00})
	if socks != tcpBackendAddr(21001) {
		t.Fatalf("socks route = %q, want %q", socks, tcpBackendAddr(21001))
	}
}
