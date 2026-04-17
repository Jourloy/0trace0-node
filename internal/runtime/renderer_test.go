package runtime

import (
	"strings"
	"testing"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func TestRenderersIncludeExpectedProtocols(t *testing.T) {
	hysteria := "hysteria2"
	trojan := "trojan"
	wireguard := "wireguard_tunnel"
	selector := "selector"

	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			"inbound": {
				{
					ID:       "in-1",
					Name:     "Trojan ingress",
					Slug:     "trojan-ingress",
					Protocol: &trojan,
					Spec:     map[string]any{},
				},
				{
					ID:       "in-2",
					Name:     "Hysteria ingress",
					Slug:     "hy2-ingress",
					Protocol: &hysteria,
					Spec: map[string]any{
						"publicAddress": "edge.example.com",
						"serverName":    "edge.example.com",
						"sni":           "edge.example.com",
					},
				},
			},
			"outbound": {
				{
					ID:       "out-1",
					Name:     "WG tunnel",
					Slug:     "wg-tunnel",
					Protocol: &wireguard,
					Spec:     map[string]any{},
				},
				{
					ID:       "out-2",
					Name:     "Selector",
					Slug:     "selector-a",
					Protocol: &selector,
					Spec: map[string]any{
						"outbounds": []any{"direct", "wg-tunnel"},
					},
				},
			},
			"client": {
				{
					ID:   "client-1",
					Name: "Client A",
					Spec: map[string]any{"inboundId": "in-1", "password": "secret"},
				},
			},
			"routing_policy": {},
			"certificate": {
				{
					ID:        "cert-1",
					Name:      "Edge cert",
					Slug:      "edge-cert",
					IsEnabled: true,
					Status:    "ready",
					Spec: map[string]any{
						"subject":  "edge.example.com",
						"domains":  []any{"edge.example.com"},
						"certFile": "/tmp/edge.crt",
						"keyFile":  "/tmp/edge.key",
					},
				},
			},
		},
	}

	ports := map[string]int{"in-1": 24443, "in-2": 24444}

	xray, _, err := RenderXray(bundle, ports)
	if err != nil {
		t.Fatalf("RenderXray returned error: %v", err)
	}
	if !strings.Contains(string(xray), "trojan-ingress") {
		t.Fatalf("expected xray config to include trojan inbound tag")
	}

	singbox, _, err := RenderSingbox(bundle, ports)
	if err != nil {
		t.Fatalf("RenderSingbox returned error: %v", err)
	}
	if !strings.Contains(string(singbox), "hy2-ingress") {
		t.Fatalf("expected sing-box config to include hysteria2 inbound")
	}
	if !strings.Contains(string(singbox), `"type": "hysteria2"`) {
		t.Fatalf("expected sing-box config to render hysteria2 inbound type")
	}
	if !strings.Contains(string(singbox), `"certificate_path": "/tmp/edge.crt"`) {
		t.Fatalf("expected sing-box hysteria2 inbound to include certificate path")
	}
	if !strings.Contains(string(singbox), `"server_name": "edge.example.com"`) {
		t.Fatalf("expected sing-box hysteria2 inbound to include server_name")
	}
	if !strings.Contains(string(singbox), "selector-a") {
		t.Fatalf("expected sing-box config to include selector outbound")
	}
}
