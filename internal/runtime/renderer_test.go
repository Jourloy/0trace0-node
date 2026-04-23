package runtime

import (
	"encoding/json"
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

func TestRenderXrayCopiesTopLevelRealitySettingsIntoStreamSettings(t *testing.T) {
	trojan := "trojan"
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			"inbound": {
				{
					ID:       "in-1",
					Name:     "Trojan reality ingress",
					Slug:     "trojan-reality",
					Protocol: &trojan,
					Spec: map[string]any{
						"streamSettings": map[string]any{
							"security": "reality",
							"network":  "tcp",
							"realitySettings": map[string]any{
								"show": false,
							},
						},
						"realitySettings": map[string]any{
							"target":      "www.apple.com:443",
							"serverNames": []any{"www.apple.com"},
							"privateKey":  "private-key",
							"shortIds":    []any{"bdab20cd"},
							"pqv":         "post-quantum-value",
							"show":        true,
						},
					},
				},
			},
			"client": {
				{
					ID:   "client-1",
					Name: "Client A",
					Spec: map[string]any{
						"inboundId": "in-1",
						"password":  "trojan-password",
					},
				},
			},
		},
	}

	raw, _, err := RenderXray(bundle, map[string]int{"in-1": 18080})
	if err != nil {
		t.Fatalf("RenderXray returned error: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal xray config: %v", err)
	}

	inbounds, ok := config["inbounds"].([]any)
	if !ok || len(inbounds) != 1 {
		t.Fatalf("expected one inbound, got %#v", config["inbounds"])
	}
	inbound, ok := inbounds[0].(map[string]any)
	if !ok {
		t.Fatalf("expected inbound object, got %#v", inbounds[0])
	}
	if got := inbound["port"]; got != float64(18080) {
		t.Fatalf("inbound port = %#v, want 18080", got)
	}

	settings, ok := inbound["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected inbound settings object, got %#v", inbound["settings"])
	}
	clients, ok := settings["clients"].([]any)
	if !ok || len(clients) != 1 {
		t.Fatalf("expected one trojan client, got %#v", settings["clients"])
	}
	client, ok := clients[0].(map[string]any)
	if !ok {
		t.Fatalf("expected trojan client object, got %#v", clients[0])
	}
	if got := client["password"]; got != "trojan-password" {
		t.Fatalf("client password = %#v, want trojan-password", got)
	}

	streamSettings, ok := inbound["streamSettings"].(map[string]any)
	if !ok {
		t.Fatalf("expected streamSettings object, got %#v", inbound["streamSettings"])
	}
	if got := streamSettings["security"]; got != "reality" {
		t.Fatalf("streamSettings.security = %#v, want reality", got)
	}

	realitySettings, ok := streamSettings["realitySettings"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested realitySettings object, got %#v", streamSettings["realitySettings"])
	}
	if got := realitySettings["target"]; got != "www.apple.com:443" {
		t.Fatalf("reality target = %#v, want www.apple.com:443", got)
	}
	serverNames, ok := realitySettings["serverNames"].([]any)
	if !ok || len(serverNames) != 1 || serverNames[0] != "www.apple.com" {
		t.Fatalf("reality serverNames = %#v, want [www.apple.com]", realitySettings["serverNames"])
	}
	if got := realitySettings["privateKey"]; got != "private-key" {
		t.Fatalf("reality privateKey = %#v, want private-key", got)
	}
	shortIDs, ok := realitySettings["shortIds"].([]any)
	if !ok || len(shortIDs) != 1 || shortIDs[0] != "bdab20cd" {
		t.Fatalf("reality shortIds = %#v, want [bdab20cd]", realitySettings["shortIds"])
	}
	if got := realitySettings["pqv"]; got != "post-quantum-value" {
		t.Fatalf("reality pqv = %#v, want post-quantum-value", got)
	}
	if got := realitySettings["show"]; got != false {
		t.Fatalf("existing nested realitySettings.show = %#v, want false", got)
	}
}
