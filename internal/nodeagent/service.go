package nodeagent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jourloy/0trace0-node/internal/controlapi"
	"github.com/jourloy/0trace0-node/internal/publicaddr"
	"github.com/jourloy/0trace0-node/internal/runtime"
	"github.com/jourloy/0trace0-node/internal/runtimeapply"
)

const (
	serviceVersion = "external-node-v1"
	defaultTimeout = 15 * time.Second
)

var serviceCapabilities = []string{
	"desired-state-apply",
	"runtime-health",
	"event-journal",
	"mtproxy-stats",
}

type stateFile struct {
	InstanceID       string         `json:"instanceId"`
	NodeID           string         `json:"nodeId"`
	ObservedRevision string         `json:"observedRevision"`
	AssignedPorts    map[string]int `json:"assignedPorts"`
	LastAppliedAt    *time.Time     `json:"lastAppliedAt,omitempty"`
	LastError        string         `json:"lastError,omitempty"`
	LastEventCursor  int64          `json:"lastEventCursor"`
	Status           string         `json:"status"`
	Health           map[string]any `json:"health"`
}

type Service struct {
	cfg        Config
	logger     *slog.Logger
	supervisor *runtimeapply.Supervisor

	mu    sync.Mutex
	state stateFile
}

func New(cfg Config, logger *slog.Logger) (*Service, error) {
	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, errors.New("NODE_API_TOKEN is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, err
	}

	service := &Service{
		cfg:        cfg,
		logger:     logger,
		supervisor: runtimeapply.NewSupervisor(runtimeConfig(cfg), logger),
		state: stateFile{
			AssignedPorts: map[string]int{},
			Status:        "offline",
			Health:        map[string]any{},
		},
	}
	if err := service.loadState(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if strings.TrimSpace(service.state.InstanceID) == "" {
		service.state.InstanceID = uuid.NewString()
	}
	if service.state.AssignedPorts == nil {
		service.state.AssignedPorts = map[string]int{}
	}
	if service.state.Health == nil {
		service.state.Health = map[string]any{}
	}
	if err := service.saveState(); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultTimeout,
	}
	listener, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		return err
	}
	s.logger.Info("0trace0-node control API listening", "addr", s.cfg.HTTPAddr)

	errCh := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/api/v1/node/info", s.withAuth(http.HandlerFunc(s.handleInfo)))
	mux.Handle("/api/v1/node/runtime", s.withAuth(http.HandlerFunc(s.handleRuntime)))
	mux.Handle("/api/v1/node/desired-state", s.withAuth(http.HandlerFunc(s.handleDesiredState)))
	mux.Handle("/api/v1/node/events", s.withAuth(http.HandlerFunc(s.handleEvents)))
	return mux
}

func (s *Service) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Service) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.Lock()
	instanceID := s.state.InstanceID
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, controlapi.NodeInfoResponse{
		InstanceID:    instanceID,
		Name:          s.cfg.NodeName,
		Version:       serviceVersion,
		PublicAddress: publicaddr.EffectiveAddress(r.Context(), s.cfg.PublicAddress),
		Capabilities:  append([]string{}, serviceCapabilities...),
	})
}

func (s *Service) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.Lock()
	response, err := s.refreshRuntimeLocked()
	if err == nil {
		err = s.saveState()
	}
	s.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build runtime snapshot")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDesiredState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req controlapi.NodeDesiredStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "nodeId is required")
		return
	}
	if strings.TrimSpace(req.DesiredRevision) == "" {
		writeError(w, http.StatusBadRequest, "desiredRevision is required")
		return
	}
	if req.GeneratedAt.IsZero() {
		req.GeneratedAt = time.Now().UTC()
	}

	s.mu.Lock()
	response, err := s.applyDesiredStateLocked(req)
	saveErr := s.saveState()
	s.mu.Unlock()
	if saveErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist node state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cursor, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cursor must be a positive integer")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))

	s.mu.Lock()
	response, err := s.eventsAfterLocked(cursor, limit)
	s.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load node events")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) applyDesiredStateLocked(req controlapi.NodeDesiredStateRequest) (controlapi.NodeRuntimeResponse, error) {
	if strings.TrimSpace(req.NodeID) == "" {
		return controlapi.NodeRuntimeResponse{}, errors.New("nodeId is required")
	}
	if req.DesiredRevision == s.state.ObservedRevision && s.state.ObservedRevision != "" {
		return s.refreshRuntimeLocked()
	}

	s.state.NodeID = strings.TrimSpace(req.NodeID)
	bundle := &controlapi.ConfigBundle{
		NodeID:      s.state.NodeID,
		GeneratedAt: req.GeneratedAt.UTC(),
		Resources:   req.Resources,
	}
	if _, err := runtime.AssignPorts(*bundle); err != nil {
		return s.recordApplyFailureLocked(req.DesiredRevision, err)
	}

	result, err := s.supervisor.ApplyBundle(bundle, s.state.AssignedPorts)
	if err != nil {
		now := time.Now().UTC()
		s.state.Status = "degraded"
		s.state.LastError = err.Error()
		s.state.Health = mergeHealth(s.state.Health, result.Health)
		s.state.Health["last_apply_error"] = err.Error()
		if appendErr := s.appendEventsLocked([]controlapi.SessionEvent{{
			NodeID:    s.state.NodeID,
			EventType: "config_apply_failed",
			Status:    "error",
			Payload: map[string]any{
				"desiredRevision": req.DesiredRevision,
				"error":           err.Error(),
			},
			CreatedAt: now,
		}}); appendErr != nil {
			s.logger.Warn("failed to record apply failure event", "error", appendErr)
		}
		return s.runtimeResponseLocked(s.copyMTProxyHealth()), fmt.Errorf("failed to apply desired state: %w", err)
	}

	now := time.Now().UTC()
	mtproxyEvents, mtproxyHealth := s.supervisor.CollectMTProxyStats(s.state.NodeID)
	result.Health["mtproxy_inbounds"] = mtproxyHealth
	result.Health["mtproxy_running"] = len(mtproxyHealth)
	result.Health["mtproxy_degraded"] = mtproxyHealthDegraded(mtproxyHealth)
	result.Health["last_apply_error"] = ""

	s.state.ObservedRevision = req.DesiredRevision
	s.state.AssignedPorts = copyAssignedPorts(result.AssignedPorts)
	s.state.LastAppliedAt = &now
	s.state.LastError = ""
	s.state.Health = copyMap(result.Health)
	s.state.Status = runtimeStatus(s.state.Health)

	events := []controlapi.SessionEvent{{
		NodeID:    s.state.NodeID,
		EventType: "config_applied",
		Status:    "ok",
		Payload: map[string]any{
			"desiredRevision": req.DesiredRevision,
			"assignedPorts":   result.AssignedPorts,
			"warnings":        append([]string{}, result.Warnings...),
		},
		CreatedAt: now,
	}}
	events = append(events, mtproxyEvents...)
	if err := s.appendEventsLocked(events); err != nil {
		s.logger.Warn("failed to append runtime events", "error", err)
	}

	return s.runtimeResponseLocked(mtproxyHealth), nil
}

func (s *Service) recordApplyFailureLocked(desiredRevision string, applyErr error) (controlapi.NodeRuntimeResponse, error) {
	now := time.Now().UTC()
	s.state.Status = "degraded"
	s.state.LastError = applyErr.Error()
	s.state.Health = mergeHealth(s.state.Health, map[string]any{
		"last_apply_error": applyErr.Error(),
	})
	if appendErr := s.appendEventsLocked([]controlapi.SessionEvent{{
		NodeID:    s.state.NodeID,
		EventType: "config_apply_failed",
		Status:    "error",
		Payload: map[string]any{
			"desiredRevision": desiredRevision,
			"error":           applyErr.Error(),
		},
		CreatedAt: now,
	}}); appendErr != nil {
		s.logger.Warn("failed to record apply failure event", "error", appendErr)
	}
	return s.runtimeResponseLocked(s.copyMTProxyHealth()), fmt.Errorf("failed to apply desired state: %w", applyErr)
}

func (s *Service) refreshRuntimeLocked() (controlapi.NodeRuntimeResponse, error) {
	health := copyMap(s.state.Health)
	mtproxyHealth := s.copyMTProxyHealth()
	processSnapshot := s.supervisor.Snapshot()

	xrayState := processSnapshot.Processes["xray"]
	singboxState := processSnapshot.Processes["singbox"]
	health["xray_running"] = xrayState.Running
	health["singbox_running"] = singboxState.Running

	if strings.TrimSpace(s.state.NodeID) != "" {
		events, updatedMTProxy := s.supervisor.CollectMTProxyStats(s.state.NodeID)
		mtproxyHealth = updatedMTProxy
		if err := s.appendEventsLocked(events); err != nil {
			s.logger.Warn("failed to append mtproxy stats", "error", err)
		}
	}

	health["mtproxy_inbounds"] = mtproxyHealth
	health["mtproxy_running"] = len(mtproxyHealth)
	health["mtproxy_degraded"] = mtproxyHealthDegraded(mtproxyHealth)

	lastError := strings.TrimSpace(s.state.LastError)
	if strings.TrimSpace(xrayState.LastError) != "" {
		lastError = xrayState.LastError
	}
	if strings.TrimSpace(singboxState.LastError) != "" {
		lastError = singboxState.LastError
	}
	health["last_apply_error"] = lastError

	s.state.LastError = lastError
	s.state.Health = health
	if s.state.ObservedRevision == "" && lastError == "" {
		s.state.Status = "offline"
	} else {
		s.state.Status = runtimeStatus(health)
	}

	return s.runtimeResponseLocked(mtproxyHealth), nil
}

func (s *Service) runtimeResponseLocked(mtproxyHealth map[string]controlapi.MTProxyInboundHealth) controlapi.NodeRuntimeResponse {
	var lastAppliedAt *time.Time
	if s.state.LastAppliedAt != nil {
		value := *s.state.LastAppliedAt
		lastAppliedAt = &value
	}

	return controlapi.NodeRuntimeResponse{
		Status:           s.state.Status,
		ObservedRevision: s.state.ObservedRevision,
		AssignedPorts:    copyAssignedPorts(s.state.AssignedPorts),
		Health:           copyMap(s.state.Health),
		MTProxyInbounds:  copyMTProxyHealth(mtproxyHealth),
		LastAppliedAt:    lastAppliedAt,
		LastError:        s.state.LastError,
	}
}

func (s *Service) copyMTProxyHealth() map[string]controlapi.MTProxyInboundHealth {
	return s.supervisor.Snapshot().MTProxyInbounds
}

func runtimeConfig(cfg Config) runtimeapply.Config {
	return runtimeapply.Config{
		StateDir: cfg.StateDir,
	}
}

func (s *Service) statePath() string {
	return filepath.Join(s.cfg.StateDir, "service-state.json")
}

func (s *Service) loadState() error {
	raw, err := os.ReadFile(s.statePath())
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &s.state)
}

func (s *Service) saveState() error {
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.statePath(), raw, 0o600)
}

func mergeHealth(base map[string]any, extra map[string]any) map[string]any {
	out := copyMap(base)
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
	mtproxyDegraded, _ := health["mtproxy_degraded"].(bool)
	if (xrayRequired && !xrayRunning) || (singboxRequired && !singboxRunning) || certPending || hostUnresolved || mtproxyDegraded {
		return "degraded"
	}
	return "online"
}

func mtproxyHealthDegraded(values map[string]controlapi.MTProxyInboundHealth) bool {
	for _, value := range values {
		if strings.TrimSpace(value.Status) != "running" {
			return true
		}
	}
	return false
}

func copyAssignedPorts(values map[string]int) map[string]int {
	out := make(map[string]int, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyMTProxyHealth(values map[string]controlapi.MTProxyInboundHealth) map[string]controlapi.MTProxyInboundHealth {
	out := make(map[string]controlapi.MTProxyInboundHealth, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
