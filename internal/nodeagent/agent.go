package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
	"github.com/jourloy/0trace0-node/internal/publicaddr"
	"github.com/jourloy/0trace0-node/internal/runtime"
	"github.com/jourloy/0trace0-node/internal/runtimeapply"
)

type stateFile struct {
	NodeID          string         `json:"nodeId"`
	AssignedPorts   map[string]int `json:"assignedPorts"`
	CurrentRevision string         `json:"currentRevision"`
	LastSyncAt      time.Time      `json:"lastSyncAt"`
}

type Agent struct {
	cfg        Config
	client     *http.Client
	logger     *slog.Logger
	state      stateFile
	supervisor *runtimeapply.Supervisor
}

func New(cfg Config, logger *slog.Logger) (*Agent, error) {
	client, err := cfg.HTTPClient()
	if err != nil {
		return nil, err
	}
	agent := &Agent{
		cfg:        cfg,
		client:     client,
		logger:     logger,
		supervisor: runtimeapply.NewSupervisor(agentRuntimeConfig(cfg), logger),
		state: stateFile{
			NodeID:          strings.TrimSpace(cfg.NodeID),
			AssignedPorts:   map[string]int{},
			CurrentRevision: "",
		},
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, err
	}
	_ = agent.loadState()
	return agent, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.registerIfNeeded(ctx); err != nil {
		return err
	}
	if err := a.syncOnce(ctx); err != nil {
		a.logger.Warn("initial sync failed", "error", err)
	}

	ticker := time.NewTicker(a.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.syncOnce(ctx); err != nil {
				a.logger.Warn("sync failed", "error", err)
			}
		}
	}
}

func (a *Agent) registerIfNeeded(ctx context.Context) error {
	if strings.TrimSpace(a.state.NodeID) != "" {
		return nil
	}
	payload := controlapi.AgentRegisterRequest{
		NodeID:        a.cfg.NodeID,
		Name:          a.cfg.NodeName,
		PublicAddress: publicaddr.EffectiveAddress(ctx, a.cfg.PublicAddress),
		Labels:        map[string]string{"runtime": "xray+extras"},
		Version:       "v1",
	}
	var resp struct {
		Data controlapi.AgentRegisterResponse `json:"data"`
	}
	if err := a.doJSON(ctx, http.MethodPost, "/api/v1/agent/register", payload, &resp); err != nil {
		return err
	}
	a.state.NodeID = resp.Data.NodeID
	return a.saveState()
}

func (a *Agent) syncOnce(ctx context.Context) error {
	bundle, err := a.fetchBundle(ctx)
	if err != nil {
		return err
	}

	result, err := a.applyBundle(bundle)
	if err != nil {
		_ = a.sendHeartbeat(ctx, applyResult{
			Revision:      a.state.CurrentRevision,
			AssignedPorts: a.state.AssignedPorts,
			Warnings:      []string{},
			Health: map[string]any{
				"heartbeat_ok":     true,
				"xray_running":     false,
				"singbox_running":  false,
				"last_apply_error": err.Error(),
			},
			Status: "degraded",
		})
		_ = a.sendTelemetry(ctx, []controlapi.SessionEvent{{
			EventType: "config_apply_failed",
			NodeID:    a.state.NodeID,
			Status:    "error",
			Payload: map[string]any{
				"error": err.Error(),
			},
			CreatedAt: time.Now().UTC(),
		}})
		return err
	}

	a.state.AssignedPorts = result.AssignedPorts
	a.state.CurrentRevision = result.Revision
	a.state.LastSyncAt = time.Now().UTC()
	if err := a.saveState(); err != nil {
		a.logger.Warn("failed to persist state", "error", err)
	}

	events := []controlapi.SessionEvent{{
		EventType: "config_applied",
		NodeID:    a.state.NodeID,
		Status:    "ok",
		Payload: map[string]any{
			"revision":      result.Revision,
			"assignedPorts": result.AssignedPorts,
			"warnings":      result.Warnings,
		},
		CreatedAt: time.Now().UTC(),
	}}
	if err := a.sendTelemetry(ctx, events); err != nil {
		a.logger.Warn("failed to send telemetry", "error", err)
	}
	return a.sendHeartbeat(ctx, result)
}

func (a *Agent) fetchBundle(ctx context.Context) (*controlapi.ConfigBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint("/api/v1/agent/config?nodeId="+a.state.NodeID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)

	res, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("config request failed with status %d", res.StatusCode)
	}
	var envelope struct {
		Data controlapi.ConfigBundle `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (a *Agent) sendHeartbeat(ctx context.Context, result applyResult) error {
	payload := controlapi.AgentHeartbeatRequest{
		NodeID:          a.state.NodeID,
		NodeName:        a.cfg.NodeName,
		PublicAddress:   publicaddr.EffectiveAddress(ctx, a.cfg.PublicAddress),
		Version:         "v1",
		Status:          result.Status,
		CurrentRevision: result.Revision,
		AssignedPorts:   result.AssignedPorts,
		Health: mergeHealth(result.Health, map[string]any{
			"warnings":          result.Warnings,
			"heartbeat_ok":      true,
			"xrayConfigPath":    filepath.Join(a.cfg.StateDir, "rendered", "xray.json"),
			"singboxConfigPath": filepath.Join(a.cfg.StateDir, "rendered", "singbox.json"),
		}),
	}
	var resp map[string]any
	return a.doJSON(ctx, http.MethodPost, "/api/v1/agent/heartbeat", payload, &resp)
}

func (a *Agent) sendTelemetry(ctx context.Context, events []controlapi.SessionEvent) error {
	if len(events) == 0 {
		return nil
	}
	payload := controlapi.AgentTelemetryRequest{
		NodeID: a.state.NodeID,
		Events: events,
	}
	var resp map[string]any
	return a.doJSON(ctx, http.MethodPost, "/api/v1/agent/telemetry", payload, &resp)
}

func (a *Agent) doJSON(ctx context.Context, method, path string, payload any, dst any) error {
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.endpoint(path), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("request failed: status %d", res.StatusCode)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(dst)
}

func (a *Agent) endpoint(path string) string {
	return strings.TrimRight(a.cfg.ControlPlaneURL, "/") + path
}

func (a *Agent) statePath() string {
	return filepath.Join(a.cfg.StateDir, "agent-state.json")
}

func (a *Agent) loadState() error {
	raw, err := os.ReadFile(a.statePath())
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &a.state)
}

func (a *Agent) saveState() error {
	raw, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.statePath(), raw, 0o600)
}

func (a *Agent) assignPorts(bundle *controlapi.ConfigBundle) (map[string]int, error) {
	return runtime.AssignPorts(*bundle, a.state.AssignedPorts, a.cfg.PortMin, a.cfg.PortMax)
}

func agentRuntimeConfig(cfg Config) runtimeapply.Config {
	return runtimeapply.Config{
		StateDir: cfg.StateDir,
		PortMin:  cfg.PortMin,
		PortMax:  cfg.PortMax,
	}
}

func mergeHealth(base map[string]any, extra map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func runtimeStatus(health map[string]any) string {
	if value, ok := health["last_apply_error"].(string); ok && strings.TrimSpace(value) != "" {
		return "degraded"
	}
	xrayRequired, _ := health["xray_required"].(bool)
	singboxRequired, _ := health["singbox_required"].(bool)
	xrayRunning, _ := health["xray_running"].(bool)
	singboxRunning, _ := health["singbox_running"].(bool)
	certPending, _ := health["cert_pending"].(bool)
	hostUnresolved, _ := health["host_unresolved"].(bool)
	if (xrayRequired && !xrayRunning) || (singboxRequired && !singboxRunning) || certPending || hostUnresolved {
		return "degraded"
	}
	return "online"
}
