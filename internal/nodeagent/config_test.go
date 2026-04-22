package nodeagent

import (
	"testing"
)

func TestLoadConfigUsesNodeServiceEnvNames(t *testing.T) {
	t.Setenv("NODE_HTTP_ADDR", ":9090")
	t.Setenv("NODE_API_TOKEN", "token-123")
	t.Setenv("NODE_NAME", "node-1")
	t.Setenv("PUBLIC_ADDRESS", "203.0.113.10")
	t.Setenv("STATE_DIR", "/var/lib/node-agent")

	cfg := LoadConfig()

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("expected NODE_HTTP_ADDR to populate HTTPAddr, got %q", cfg.HTTPAddr)
	}
	if cfg.APIToken != "token-123" {
		t.Fatalf("expected NODE_API_TOKEN to populate APIToken, got %q", cfg.APIToken)
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
}

func TestLoadConfigIgnoresLegacyAgentEnvNames(t *testing.T) {
	t.Setenv("API_URL", "https://legacy-0trace0-panel.example.com")
	t.Setenv("NODE_TOKEN", "legacy-token")
	t.Setenv("NODE_ID", "legacy-node-id")
	t.Setenv("ZEROTRACEZERO_NODE_NAME", "legacy-node-name")
	t.Setenv("SYNC_INTERVAL", "1m")
	t.Setenv("HTTP_TIMEOUT", "1m")

	cfg := LoadConfig()

	if cfg.HTTPAddr != ":8090" {
		t.Fatalf("expected legacy agent env vars to be ignored, got %q", cfg.HTTPAddr)
	}
	if cfg.APIToken != "" {
		t.Fatalf("expected legacy agent env vars to be ignored, got %q", cfg.APIToken)
	}
	if cfg.NodeName != "node-service" {
		t.Fatalf("expected legacy node name env vars to be ignored, got %q", cfg.NodeName)
	}
	if cfg.PublicAddress != "" {
		t.Fatalf("expected legacy public address env vars to be ignored, got %q", cfg.PublicAddress)
	}
	if cfg.StateDir != "./data/node-agent" {
		t.Fatalf("expected legacy state dir env var to be ignored, got %q", cfg.StateDir)
	}
}
