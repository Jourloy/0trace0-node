package nodeagent

import (
	"testing"
	"time"
)

func TestLoadConfigUsesShortEnvNames(t *testing.T) {
	t.Setenv("API_URL", "https://0trace0-panel.example.com")
	t.Setenv("NODE_TOKEN", "token-123")
	t.Setenv("NODE_ID", "node-id-1")
	t.Setenv("NODE_NAME", "node-1")
	t.Setenv("PUBLIC_ADDRESS", "203.0.113.10")
	t.Setenv("STATE_DIR", "/var/lib/node-agent")
	t.Setenv("SYNC_INTERVAL", "45s")
	t.Setenv("HTTP_TIMEOUT", "22s")
	t.Setenv("PORT_MIN", "21000")
	t.Setenv("PORT_MAX", "22000")
	t.Setenv("MTLS_CERT_FILE", "/etc/node-agent/tls.crt")
	t.Setenv("MTLS_KEY_FILE", "/etc/node-agent/tls.key")
	t.Setenv("MTLS_CA_FILE", "/etc/node-agent/ca.crt")

	cfg := LoadConfig()

	if cfg.PanelURL != "https://0trace0-panel.example.com" {
		t.Fatalf("expected API_URL to populate PanelURL, got %q", cfg.PanelURL)
	}
	if cfg.Token != "token-123" {
		t.Fatalf("expected NODE_TOKEN to populate Token, got %q", cfg.Token)
	}
	if cfg.NodeID != "node-id-1" {
		t.Fatalf("expected NODE_ID to populate NodeID, got %q", cfg.NodeID)
	}
	if cfg.NodeName != "node-1" {
		t.Fatalf("expected NODE_NAME to populate NodeName, got %q", cfg.NodeName)
	}
	if cfg.PublicAddress != "203.0.113.10" {
		t.Fatalf("expected PUBLIC_ADDRESS to populate PublicAddress, got %q", cfg.PublicAddress)
	}
	if cfg.StateDir != "/var/lib/node-agent" {
		t.Fatalf("expected STATE_DIR to populate StateDir, got %q", cfg.StateDir)
	}
	if cfg.SyncInterval != 45*time.Second {
		t.Fatalf("expected SYNC_INTERVAL to populate SyncInterval, got %s", cfg.SyncInterval)
	}
	if cfg.HTTPTimeout != 22*time.Second {
		t.Fatalf("expected HTTP_TIMEOUT to populate HTTPTimeout, got %s", cfg.HTTPTimeout)
	}
	if cfg.PortMin != 21000 {
		t.Fatalf("expected PORT_MIN to populate PortMin, got %d", cfg.PortMin)
	}
	if cfg.PortMax != 22000 {
		t.Fatalf("expected PORT_MAX to populate PortMax, got %d", cfg.PortMax)
	}
	if cfg.MTLSCertFile != "/etc/node-agent/tls.crt" {
		t.Fatalf("expected MTLS_CERT_FILE to populate MTLSCertFile, got %q", cfg.MTLSCertFile)
	}
	if cfg.MTLSKeyFile != "/etc/node-agent/tls.key" {
		t.Fatalf("expected MTLS_KEY_FILE to populate MTLSKeyFile, got %q", cfg.MTLSKeyFile)
	}
	if cfg.MTLSCAFile != "/etc/node-agent/ca.crt" {
		t.Fatalf("expected MTLS_CA_FILE to populate MTLSCAFile, got %q", cfg.MTLSCAFile)
	}
}

func TestLoadConfigIgnoresLegacyPrefixedEnvNames(t *testing.T) {
	t.Setenv("ZEROTRACEZERO_CONTROL_PLANE_URL", "https://legacy-0trace0-panel.example.com")
	t.Setenv("ZEROTRACEZERO_AGENT_CONTROL_PLANE_URL", "https://legacy-agent-0trace0-panel.example.com")
	t.Setenv("ZEROTRACEZERO_NODE_TOKEN", "legacy-token")
	t.Setenv("ZEROTRACEZERO_AGENT_TOKEN", "legacy-agent-token")
	t.Setenv("ZEROTRACEZERO_AGENT_NODE_ID", "legacy-node-id")
	t.Setenv("ZEROTRACEZERO_NODE_NAME", "legacy-node-name")
	t.Setenv("ZEROTRACEZERO_AGENT_NODE_NAME", "legacy-agent-node-name")
	t.Setenv("ZEROTRACEZERO_NODE_PUBLIC_ADDRESS", "198.51.100.20")
	t.Setenv("ZEROTRACEZERO_AGENT_PUBLIC_ADDRESS", "198.51.100.21")
	t.Setenv("ZEROTRACEZERO_AGENT_STATE_DIR", "/legacy/state")
	t.Setenv("ZEROTRACEZERO_AGENT_SYNC_INTERVAL", "1m")
	t.Setenv("ZEROTRACEZERO_AGENT_HTTP_TIMEOUT", "1m")
	t.Setenv("ZEROTRACEZERO_AGENT_PORT_MIN", "23000")
	t.Setenv("ZEROTRACEZERO_AGENT_PORT_MAX", "24000")
	t.Setenv("ZEROTRACEZERO_AGENT_MTLS_CERT_FILE", "/legacy/tls.crt")
	t.Setenv("ZEROTRACEZERO_AGENT_MTLS_KEY_FILE", "/legacy/tls.key")
	t.Setenv("ZEROTRACEZERO_AGENT_MTLS_CA_FILE", "/legacy/ca.crt")

	cfg := LoadConfig()

	if cfg.PanelURL != "http://localhost:8080" {
		t.Fatalf("expected legacy panel env vars to be ignored, got %q", cfg.PanelURL)
	}
	if cfg.Token != "" {
		t.Fatalf("expected legacy token env vars to be ignored, got %q", cfg.Token)
	}
	if cfg.NodeID != "" {
		t.Fatalf("expected legacy node id env var to be ignored, got %q", cfg.NodeID)
	}
	if cfg.NodeName != "node-agent" {
		t.Fatalf("expected legacy node name env vars to be ignored, got %q", cfg.NodeName)
	}
	if cfg.PublicAddress != "" {
		t.Fatalf("expected legacy public address env vars to be ignored, got %q", cfg.PublicAddress)
	}
	if cfg.StateDir != "./data/node-agent" {
		t.Fatalf("expected legacy state dir env var to be ignored, got %q", cfg.StateDir)
	}
	if cfg.SyncInterval != 15*time.Second {
		t.Fatalf("expected legacy sync interval env var to be ignored, got %s", cfg.SyncInterval)
	}
	if cfg.HTTPTimeout != 15*time.Second {
		t.Fatalf("expected legacy http timeout env var to be ignored, got %s", cfg.HTTPTimeout)
	}
	if cfg.PortMin != 20000 {
		t.Fatalf("expected legacy port min env var to be ignored, got %d", cfg.PortMin)
	}
	if cfg.PortMax != 45000 {
		t.Fatalf("expected legacy port max env var to be ignored, got %d", cfg.PortMax)
	}
	if cfg.MTLSCertFile != "" {
		t.Fatalf("expected legacy mTLS cert env var to be ignored, got %q", cfg.MTLSCertFile)
	}
	if cfg.MTLSKeyFile != "" {
		t.Fatalf("expected legacy mTLS key env var to be ignored, got %q", cfg.MTLSKeyFile)
	}
	if cfg.MTLSCAFile != "" {
		t.Fatalf("expected legacy mTLS CA env var to be ignored, got %q", cfg.MTLSCAFile)
	}
}
