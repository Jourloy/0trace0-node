package nodeagent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jourloy/0trace0-node/internal/controlapi"
	"github.com/jourloy/0trace0-node/internal/runtime"
)

func TestNodeAPIRequiresAuthExceptHealth(t *testing.T) {
	service := newTestService(t, Config{
		HTTPAddr: ":0",
		APIToken: "secret-token",
		NodeName: "node-test",
		StateDir: t.TempDir(),
	})
	handler := service.Handler()

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", healthRec.Code, http.StatusOK)
	}

	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/node/runtime", nil)
	runtimeRec := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRec, runtimeReq)
	if runtimeRec.Code != http.StatusUnauthorized {
		t.Fatalf("runtime status = %d, want %d", runtimeRec.Code, http.StatusUnauthorized)
	}
}

func TestDesiredStateIsIdempotentForSameRevision(t *testing.T) {
	service := newTestService(t, Config{
		HTTPAddr: ":0",
		APIToken: "secret-token",
		NodeName: "node-test",
		StateDir: t.TempDir(),
	})
	handler := service.Handler()

	request := controlapi.NodeDesiredStateRequest{
		NodeID:          "node-1",
		DesiredRevision: "rev-1",
		Resources:       map[string][]controlapi.ManagedResource{},
	}

	first := putDesiredState(t, handler, service.cfg.APIToken, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first desired-state status = %d, want %d, body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	second := putDesiredState(t, handler, service.cfg.APIToken, request)
	if second.Code != http.StatusOK {
		t.Fatalf("second desired-state status = %d, want %d, body=%s", second.Code, http.StatusOK, second.Body.String())
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/v1/node/events", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+service.cfg.APIToken)
	eventsRec := httptest.NewRecorder()
	handler.ServeHTTP(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d", eventsRec.Code, http.StatusOK)
	}

	var response controlapi.NodeEventsResponse
	if err := json.NewDecoder(eventsRec.Body).Decode(&response); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("events count = %d, want 1", len(response.Items))
	}
	if response.Items[0].Event.EventType != "config_applied" {
		t.Fatalf("event type = %q, want config_applied", response.Items[0].Event.EventType)
	}

	cursorReq := httptest.NewRequest(http.MethodGet, "/api/v1/node/events?cursor="+response.NextCursor, nil)
	cursorReq.Header.Set("Authorization", "Bearer "+service.cfg.APIToken)
	cursorRec := httptest.NewRecorder()
	handler.ServeHTTP(cursorRec, cursorReq)
	if cursorRec.Code != http.StatusOK {
		t.Fatalf("events with cursor status = %d, want %d", cursorRec.Code, http.StatusOK)
	}

	var empty controlapi.NodeEventsResponse
	if err := json.NewDecoder(cursorRec.Body).Decode(&empty); err != nil {
		t.Fatalf("decode cursor events response: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Fatalf("events after cursor = %d, want 0", len(empty.Items))
	}
}

func TestFailedApplyUpdatesRuntimeAndWritesEvent(t *testing.T) {
	service := newTestService(t, Config{
		HTTPAddr: ":0",
		APIToken: "secret-token",
		NodeName: "node-test",
		StateDir: t.TempDir(),
	})
	handler := service.Handler()

	protocol := "trojan"
	request := controlapi.NodeDesiredStateRequest{
		NodeID:          "node-1",
		DesiredRevision: "broken-rev",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{
					ID:        "inbound-1",
					Kind:      controlapi.KindInbound,
					Name:      "Inbound",
					Slug:      "inbound",
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
					ID:        "inbound-2",
					Kind:      controlapi.KindInbound,
					Name:      "Inbound 2",
					Slug:      "inbound-2",
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

	recorder := putDesiredState(t, handler, service.cfg.APIToken, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("desired-state status = %d, want %d, body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}

	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/node/runtime", nil)
	runtimeReq.Header.Set("Authorization", "Bearer "+service.cfg.APIToken)
	runtimeRec := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRec, runtimeReq)
	if runtimeRec.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, want %d", runtimeRec.Code, http.StatusOK)
	}

	var runtimeResponse controlapi.NodeRuntimeResponse
	if err := json.NewDecoder(runtimeRec.Body).Decode(&runtimeResponse); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if runtimeResponse.Status != "degraded" {
		t.Fatalf("runtime status = %q, want degraded", runtimeResponse.Status)
	}
	if runtimeResponse.LastError == "" {
		t.Fatalf("lastError is empty, want populated")
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/v1/node/events", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+service.cfg.APIToken)
	eventsRec := httptest.NewRecorder()
	handler.ServeHTTP(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d", eventsRec.Code, http.StatusOK)
	}

	var eventsResponse controlapi.NodeEventsResponse
	if err := json.NewDecoder(eventsRec.Body).Decode(&eventsResponse); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(eventsResponse.Items) != 1 {
		t.Fatalf("events count = %d, want 1", len(eventsResponse.Items))
	}
	if eventsResponse.Items[0].Event.EventType != "config_apply_failed" {
		t.Fatalf("event type = %q, want config_apply_failed", eventsResponse.Items[0].Event.EventType)
	}
}

func TestApplyUsesFixedPublicProtocolPorts(t *testing.T) {
	service := newTestService(t, Config{
		HTTPAddr: ":0",
		APIToken: "secret-token",
		NodeName: "node-test",
		StateDir: t.TempDir(),
	})
	handler := service.Handler()

	trojan := "trojan"
	mtproxy := "mtproxy"
	request := controlapi.NodeDesiredStateRequest{
		NodeID:          "node-1",
		DesiredRevision: "rev-fixed-ports",
		Resources: map[string][]controlapi.ManagedResource{
			string(controlapi.KindInbound): {
				{
					ID:        "trojan-1",
					Kind:      controlapi.KindInbound,
					Name:      "Trojan",
					Slug:      "trojan",
					Protocol:  &trojan,
					IsEnabled: true,
					Spec: map[string]any{
						"serverName": "edge.example.com",
						"sni":        "edge.example.com",
						"streamSettings": map[string]any{
							"security": "reality",
						},
						"realitySettings": map[string]any{
							"privateKey": "XBM0eloc-kUEHh8YTKzlIJAdc-9iB0lKx0xG5lweJFg",
							"shortIds":   []any{"01234567"},
							"serverNames": []any{
								"edge.example.com",
							},
							"target": "edge.example.com:443",
						},
					},
				},
				{
					ID:        "mtproxy-1",
					Kind:      controlapi.KindInbound,
					Name:      "MTProxy",
					Slug:      "mtproxy",
					Protocol:  &mtproxy,
					IsEnabled: true,
					Spec: map[string]any{
						"mtproxySettings": map[string]any{
							"transportMode": "secure",
							"workers":       2,
						},
					},
				},
			},
			string(controlapi.KindClient): {
				{
					ID:        "trojan-client",
					Kind:      controlapi.KindClient,
					Name:      "Trojan Client",
					IsEnabled: true,
					Spec: map[string]any{
						"inboundId": "trojan-1",
						"password":  "secret",
					},
				},
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

	recorder := putDesiredState(t, handler, service.cfg.APIToken, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("desired-state status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/node/runtime", nil)
	runtimeReq.Header.Set("Authorization", "Bearer "+service.cfg.APIToken)
	runtimeRec := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRec, runtimeReq)
	if runtimeRec.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, want %d", runtimeRec.Code, http.StatusOK)
	}

	var runtimeResponse controlapi.NodeRuntimeResponse
	if err := json.NewDecoder(runtimeRec.Body).Decode(&runtimeResponse); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if runtimeResponse.AssignedPorts["trojan-1"] != runtime.TrojanPublicPort {
		t.Fatalf("trojan port = %d, want %d", runtimeResponse.AssignedPorts["trojan-1"], runtime.TrojanPublicPort)
	}
	if runtimeResponse.AssignedPorts["mtproxy-1"] != runtime.MTProxyPublicPort {
		t.Fatalf("mtproxy port = %d, want %d", runtimeResponse.AssignedPorts["mtproxy-1"], runtime.MTProxyPublicPort)
	}
}

func newTestService(t *testing.T, cfg Config) *Service {
	t.Helper()
	service, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func putDesiredState(t *testing.T, handler http.Handler, token string, request controlapi.NodeDesiredStateRequest) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal desired-state request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/node/desired-state", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
