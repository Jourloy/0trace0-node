package runtime

import (
	"testing"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

func TestAssignPortsIsDeterministic(t *testing.T) {
	protocol := "socks5"
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{ID: "a", Name: "Inbound A", Protocol: &protocol, IsEnabled: true},
				{ID: "bridge", Name: "Bridge", Protocol: &protocol, IsEnabled: true, Spec: map[string]any{"bridge": true}},
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

	if first["a"] != second["a"] || first["bridge"] != second["bridge"] {
		t.Fatalf("AssignPorts returned unstable ports: first=%v second=%v", first, second)
	}
	if first["a"] != SOCKS5PublicPort {
		t.Fatalf("AssignPorts returned public port %d, want %d", first["a"], SOCKS5PublicPort)
	}
	if first["bridge"] < internalInboundPortMin || first["bridge"] > internalInboundPortMax {
		t.Fatalf("AssignPorts returned bridge port %d, want %d-%d", first["bridge"], internalInboundPortMin, internalInboundPortMax)
	}
}

func TestAssignPortsUsesFixedPublicProtocolPoolAndInternalBridgePorts(t *testing.T) {
	protocols := map[string]string{
		"trojan":    "trojan",
		"vless":     "vless",
		"mtproxy":   "mtproxy",
		"http":      "http",
		"socks5":    "socks5",
		"hysteria2": "hysteria2",
		"wireguard": "wireguard",
	}
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{ID: "trojan", Name: "Trojan", Protocol: stringPtr(protocols["trojan"]), IsEnabled: true},
				{ID: "vless", Name: "VLESS", Protocol: stringPtr(protocols["vless"]), IsEnabled: true},
				{ID: "mtproxy", Name: "MTProxy", Protocol: stringPtr(protocols["mtproxy"]), IsEnabled: true},
				{ID: "http", Name: "HTTP", Protocol: stringPtr(protocols["http"]), IsEnabled: true},
				{ID: "socks5", Name: "SOCKS5", Protocol: stringPtr(protocols["socks5"]), IsEnabled: true},
				{ID: "hysteria2", Name: "Hysteria2", Protocol: stringPtr(protocols["hysteria2"]), IsEnabled: true},
				{ID: "wireguard", Name: "WireGuard", Protocol: stringPtr(protocols["wireguard"]), IsEnabled: true},
				{ID: "socks5-bridge", Name: "Bridge", Protocol: stringPtr(protocols["socks5"]), IsEnabled: true, Spec: map[string]any{"bridge": true}},
			},
		},
	}

	assigned, err := AssignPorts(bundle)
	if err != nil {
		t.Fatalf("AssignPorts returned error: %v", err)
	}

	want := map[string]int{
		"trojan":    TrojanPublicPort,
		"vless":     VLESSPublicPort,
		"mtproxy":   MTProxyPublicPort,
		"http":      HTTPPublicPort,
		"socks5":    SOCKS5PublicPort,
		"hysteria2": Hysteria2PublicPort,
		"wireguard": WireGuardPublicPort,
	}
	for inboundID, port := range want {
		if assigned[inboundID] != port {
			t.Fatalf("%s port = %d, want %d", inboundID, assigned[inboundID], port)
		}
	}
	if bridgePort := assigned["socks5-bridge"]; bridgePort < internalInboundPortMin || bridgePort > internalInboundPortMax {
		t.Fatalf("bridge port = %d, want %d-%d", bridgePort, internalInboundPortMin, internalInboundPortMax)
	}
}

func TestAssignPortsRejectsDuplicateEnabledPublicProtocols(t *testing.T) {
	protocol := "trojan"
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{ID: "a", Name: "A", Protocol: &protocol, IsEnabled: true},
				{ID: "b", Name: "B", Protocol: &protocol, IsEnabled: true},
			},
		},
	}

	if _, err := AssignPorts(bundle); err == nil {
		t.Fatal("AssignPorts returned nil, want duplicate public protocol error")
	}
}

func TestAssignPortsAllowsDisabledPublicDuplicate(t *testing.T) {
	protocol := "trojan"
	bundle := controlapi.ConfigBundle{
		NodeID: "node-1",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{ID: "enabled", Name: "Enabled", Protocol: &protocol, IsEnabled: true},
				{ID: "disabled", Name: "Disabled", Protocol: &protocol, IsEnabled: false},
			},
		},
	}

	assigned, err := AssignPorts(bundle)
	if err != nil {
		t.Fatalf("AssignPorts returned error: %v", err)
	}
	if assigned["enabled"] != TrojanPublicPort {
		t.Fatalf("enabled port = %d, want %d", assigned["enabled"], TrojanPublicPort)
	}
	if assigned["disabled"] < internalInboundPortMin || assigned["disabled"] > internalInboundPortMax {
		t.Fatalf("disabled port = %d, want %d-%d", assigned["disabled"], internalInboundPortMin, internalInboundPortMax)
	}
}

func stringPtr(value string) *string { return &value }

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
