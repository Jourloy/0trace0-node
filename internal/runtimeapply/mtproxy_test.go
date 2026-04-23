package runtimeapply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
	"github.com/jourloy/0trace0-node/internal/runtime"
)

func TestBuildMTProxyPlansOmitsProxyTagWhenEmpty(t *testing.T) {
	supervisor := newTestMTProxySupervisor(t)
	plans, warnings, err := supervisor.buildMTProxyPlansLocked(
		testMTProxyBundle(t, ""),
		map[string]int{"mtproxy-1": runtime.MTProxyPublicPort},
	)
	if err != nil {
		t.Fatalf("buildMTProxyPlansLocked returned error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(plans) != 1 {
		t.Fatalf("plans count = %d, want 1", len(plans))
	}
	if plans[0].ProxyTag != "" {
		t.Fatalf("proxyTag = %q, want empty", plans[0].ProxyTag)
	}
	if containsArg(plans[0].Args, "-P") {
		t.Fatalf("args = %v, want no -P flag", plans[0].Args)
	}
}

func TestBuildMTProxyPlansIncludesNormalizedProxyTag(t *testing.T) {
	supervisor := newTestMTProxySupervisor(t)
	plans, warnings, err := supervisor.buildMTProxyPlansLocked(
		testMTProxyBundle(t, "ABCDEF0123456789ABCDEF0123456789"),
		map[string]int{"mtproxy-1": runtime.MTProxyPublicPort},
	)
	if err != nil {
		t.Fatalf("buildMTProxyPlansLocked returned error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(plans) != 1 {
		t.Fatalf("plans count = %d, want 1", len(plans))
	}
	if plans[0].ProxyTag != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("proxyTag = %q, want lowercase hex", plans[0].ProxyTag)
	}
	if !hasArgPair(plans[0].Args, "-P", "abcdef0123456789abcdef0123456789") {
		t.Fatalf("args = %v, want normalized -P value", plans[0].Args)
	}
}

func TestBuildMTProxyPlansRejectsInvalidProxyTagBeforeStart(t *testing.T) {
	supervisor := NewSupervisor(Config{StateDir: t.TempDir()}, nil)
	plans, warnings, err := supervisor.buildMTProxyPlansLocked(
		testMTProxyBundle(t, "@hq_0trace0"),
		map[string]int{"mtproxy-1": runtime.MTProxyPublicPort},
	)
	if err == nil {
		t.Fatal("buildMTProxyPlansLocked returned nil error, want invalid proxy tag")
	}
	if plans != nil {
		t.Fatalf("plans = %v, want nil", plans)
	}
	if warnings != nil {
		t.Fatalf("warnings = %v, want nil", warnings)
	}
	if !strings.Contains(err.Error(), "proxy tag must be 32 hex chars") {
		t.Fatalf("error = %q, want proxy tag guidance", err.Error())
	}
}

func newTestMTProxySupervisor(t *testing.T) *Supervisor {
	t.Helper()
	stateDir := t.TempDir()
	assetsDir := filepath.Join(stateDir, "mtproxy-assets")
	if err := writeTestMTProxyAsset(filepath.Join(assetsDir, "proxy-secret"), "test-secret\n"); err != nil {
		t.Fatalf("write proxy-secret: %v", err)
	}
	if err := writeTestMTProxyAsset(filepath.Join(assetsDir, "proxy-multi.conf"), "proxy_for 1.1.1.1:443;\n"); err != nil {
		t.Fatalf("write proxy-multi.conf: %v", err)
	}
	return NewSupervisor(Config{StateDir: stateDir}, nil)
}

func writeTestMTProxyAsset(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func testMTProxyBundle(t *testing.T, proxyTag string) controlapi.ConfigBundle {
	t.Helper()
	mtproxy := "mtproxy"
	return controlapi.ConfigBundle{
		NodeID:      "node-1",
		GeneratedAt: time.Unix(1700000000, 0).UTC(),
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{
					ID:        "mtproxy-1",
					Kind:      controlapi.KindInbound,
					Name:      "MTProxy",
					Protocol:  &mtproxy,
					IsEnabled: true,
					Spec: map[string]any{
						"mtproxySettings": map[string]any{
							"transportMode": "secure",
							"proxyTag":      proxyTag,
							"workers":       2,
						},
					},
				},
			},
			string(controlapi.KindClient): {
				{
					ID:        "mtproxy-client",
					Kind:      controlapi.KindClient,
					Name:      "MTProxy Client",
					IsEnabled: true,
					Spec: map[string]any{
						"inboundId": "mtproxy-1",
						"secret":    "0123456789abcdef0123456789abcdef",
					},
				},
			},
		},
	}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func hasArgPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}
