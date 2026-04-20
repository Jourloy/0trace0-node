package runtimeapply

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
)

const (
	mtproxyAssetSecretURL = "https://core.telegram.org/getProxySecret"
	mtproxyAssetConfigURL = "https://core.telegram.org/getProxyConfig"
	mtproxyStatsFreshness = 24 * time.Hour
)

type mtproxyCounters struct {
	ActiveRPCs            int64
	TotForwardedQueries   int64
	TotForwardedResponses int64
	MTProtoProxyErrors    int64
	HTTPConnections       int64
	ExtConnections        int64
}

type mtproxyProcessPlan struct {
	InboundID     string
	ProcessName   string
	PublicPort    int
	StatsPort     int
	TransportMode string
	ProxyTag      string
	TLSDomains    []string
	Workers       int
	PublicAddress string
	Secrets       []string
	Args          []string
	LogPath       string
	ManifestPath  string
}

type mtproxyAssetFiles struct {
	ProxySecretPath string
	ProxyConfigPath string
}

type mtproxyPlanState struct {
	Plan      mtproxyProcessPlan
	Running   bool
	LastError string
}

func (s *Supervisor) reconcileMTProxyLocked(bundle controlapi.ConfigBundle, assignedPorts map[string]int) ([]string, error) {
	warnings := []string{}
	plans, planWarnings, err := s.buildMTProxyPlansLocked(bundle, assignedPorts)
	warnings = append(warnings, planWarnings...)
	if err != nil {
		return warnings, err
	}

	desiredNames := make(map[string]struct{}, len(plans))
	nextPlans := make(map[string]mtproxyProcessPlan, len(plans))
	nextHealth := make(map[string]controlapi.MTProxyInboundHealth, len(plans))

	for _, plan := range plans {
		desiredNames[plan.ProcessName] = struct{}{}
		nextPlans[plan.InboundID] = plan
		proc := s.process[plan.ProcessName]
		if proc == nil {
			proc = &managedProcess{
				name:       plan.ProcessName,
				logPath:    plan.LogPath,
				configPath: plan.ManifestPath,
			}
			s.process[plan.ProcessName] = proc
		}
		running, procWarnings, procErr := s.reconcileProcessLocked(proc, mtproxyBinaryPath, plan.Args, true)
		warnings = append(warnings, procWarnings...)
		entry := s.mtproxyHealth[plan.InboundID]
		entry.TransportMode = plan.TransportMode
		entry.ProxyTag = plan.ProxyTag
		entry.TLSDomains = append([]string{}, plan.TLSDomains...)
		entry.Workers = plan.Workers
		entry.UpdatedAt = time.Now().UTC()
		if running {
			entry.Status = "running"
			entry.LastError = ""
		} else {
			entry.Status = "degraded"
			entry.LastError = firstNonEmpty(proc.lastError, "mtproxy process not running")
		}
		nextHealth[plan.InboundID] = entry
		if procErr != nil {
			entry.Status = "degraded"
			entry.LastError = procErr.Error()
			nextHealth[plan.InboundID] = entry
			s.mtproxyHealth = nextHealth
			s.mtproxyPlans = nextPlans
			return warnings, procErr
		}
	}

	for name, proc := range s.process {
		if !strings.HasPrefix(name, "mtproxy:") {
			continue
		}
		if _, ok := desiredNames[name]; ok {
			continue
		}
		_ = s.stopProcessLocked(proc)
		delete(s.process, name)
		inboundID := strings.TrimPrefix(name, "mtproxy:")
		delete(s.mtproxyStatsPorts, inboundID)
		delete(s.mtproxyPrevStats, inboundID)
		delete(s.mtproxyHealth, inboundID)
		delete(s.mtproxyPlans, inboundID)
	}

	s.mtproxyPlans = nextPlans
	s.mtproxyHealth = nextHealth
	return warnings, nil
}

func (s *Supervisor) buildMTProxyPlansLocked(bundle controlapi.ConfigBundle, assignedPorts map[string]int) ([]mtproxyProcessPlan, []string, error) {
	inbounds := make([]controlapi.ManagedResource, 0)
	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		if !inbound.IsEnabled {
			continue
		}
		if strings.ToLower(strings.TrimSpace(pointerString(inbound.Protocol))) != "mtproxy" {
			continue
		}
		inbounds = append(inbounds, inbound)
	}
	if len(inbounds) == 0 {
		return nil, nil, nil
	}

	assets, warnings, err := ensureMTProxyAssets(s.cfg.StateDir)
	if err != nil {
		return nil, warnings, err
	}

	clientsByInbound := map[string][]controlapi.ManagedResource{}
	for _, client := range bundle.Resources[string(controlapi.KindClient)] {
		if !client.IsEnabled {
			continue
		}
		inboundID := strings.TrimSpace(stringFromAny(client.Spec["inboundId"]))
		if inboundID == "" {
			continue
		}
		clientsByInbound[inboundID] = append(clientsByInbound[inboundID], client)
	}

	usedPorts := map[int]struct{}{}
	for _, port := range assignedPorts {
		if port > 0 {
			usedPorts[port] = struct{}{}
		}
	}
	for _, port := range s.mtproxyStatsPorts {
		if port > 0 {
			usedPorts[port] = struct{}{}
		}
	}

	plans := make([]mtproxyProcessPlan, 0, len(inbounds))
	for _, inbound := range inbounds {
		settings := mtproxySettingsFromSpec(inbound.Spec)
		secrets := collectMTProxySecrets(clientsByInbound[inbound.ID])
		if len(secrets) == 0 {
			continue
		}
		publicPort := assignedPorts[inbound.ID]
		if inbound.Port != nil && *inbound.Port > 0 {
			publicPort = *inbound.Port
		}
		if publicPort <= 0 {
			return nil, warnings, fmt.Errorf("mtproxy inbound %s has no assigned public port", inbound.Name)
		}
		statsPort := s.mtproxyStatsPorts[inbound.ID]
		if statsPort <= 0 {
			allocated, allocErr := pickMTProxyStatsPort(usedPorts, s.cfg.PortMin, s.cfg.PortMax)
			if allocErr != nil {
				return nil, warnings, allocErr
			}
			statsPort = allocated
			s.mtproxyStatsPorts[inbound.ID] = statsPort
			usedPorts[statsPort] = struct{}{}
		}
		logPath := filepath.Join(s.cfg.StateDir, "logs", fmt.Sprintf("mtproxy-%s.log", inbound.ID))
		manifestPath := filepath.Join(s.cfg.StateDir, "rendered", fmt.Sprintf("mtproxy-%s.json", inbound.ID))
		args := []string{
			"-u", "nobody",
			"-p", strconv.Itoa(statsPort),
			"-H", strconv.Itoa(publicPort),
			"--http-stats",
		}
		for _, secret := range secrets {
			args = append(args, "-S", secret)
		}
		if strings.TrimSpace(settings.ProxyTag) != "" {
			args = append(args, "-P", strings.TrimSpace(settings.ProxyTag))
		}
		if settings.TransportMode == "tls_transport" {
			for _, domain := range settings.TLSDomains {
				if strings.TrimSpace(domain) != "" {
					args = append(args, "--domain", strings.TrimSpace(domain))
				}
			}
		}
		args = append(args,
			"--aes-pwd", assets.ProxySecretPath, assets.ProxyConfigPath,
			"-M", strconv.Itoa(settings.Workers),
		)
		plan := mtproxyProcessPlan{
			InboundID:     inbound.ID,
			ProcessName:   "mtproxy:" + inbound.ID,
			PublicPort:    publicPort,
			StatsPort:     statsPort,
			TransportMode: settings.TransportMode,
			ProxyTag:      settings.ProxyTag,
			TLSDomains:    append([]string{}, settings.TLSDomains...),
			Workers:       settings.Workers,
			PublicAddress: firstNonEmpty(stringFromAny(inbound.Spec["publicAddress"]), settings.PublicAddress),
			Secrets:       append([]string{}, secrets...),
			Args:          args,
			LogPath:       logPath,
			ManifestPath:  manifestPath,
		}
		if err := writeMTProxyManifest(plan); err != nil {
			return nil, warnings, err
		}
		plans = append(plans, plan)
	}
	return plans, warnings, nil
}

func (s *Supervisor) CollectMTProxyStats(nodeID string) ([]controlapi.SessionEvent, map[string]controlapi.MTProxyInboundHealth) {
	s.mu.Lock()
	states := make([]mtproxyPlanState, 0, len(s.mtproxyPlans))
	for inboundID, plan := range s.mtproxyPlans {
		proc := s.process[plan.ProcessName]
		state := mtproxyPlanState{Plan: plan}
		if proc != nil {
			state.Running = proc.running
			state.LastError = proc.lastError
		}
		if health, ok := s.mtproxyHealth[inboundID]; ok {
			state.Plan.TransportMode = firstNonEmpty(state.Plan.TransportMode, health.TransportMode)
		}
		states = append(states, state)
	}
	prev := make(map[string]mtproxyCounters, len(s.mtproxyPrevStats))
	for key, value := range s.mtproxyPrevStats {
		prev[key] = value
	}
	s.mu.Unlock()

	now := time.Now().UTC()
	events := make([]controlapi.SessionEvent, 0, len(states))
	health := make(map[string]controlapi.MTProxyInboundHealth, len(states))
	nextPrev := make(map[string]mtproxyCounters, len(states))

	for _, state := range states {
		entry := controlapi.MTProxyInboundHealth{
			Status:        "running",
			TransportMode: state.Plan.TransportMode,
			ProxyTag:      state.Plan.ProxyTag,
			TLSDomains:    append([]string{}, state.Plan.TLSDomains...),
			Workers:       state.Plan.Workers,
			UpdatedAt:     now,
		}
		if !state.Running {
			entry.Status = "degraded"
			entry.LastError = firstNonEmpty(state.LastError, "mtproxy process not running")
			health[state.Plan.InboundID] = entry
			continue
		}
		counters, err := fetchMTProxyCounters(state.Plan.StatsPort)
		if err != nil {
			entry.Status = "degraded"
			entry.LastError = err.Error()
			health[state.Plan.InboundID] = entry
			continue
		}
		entry.ActiveRPCs = counters.ActiveRPCs
		entry.TotForwardedQueries = counters.TotForwardedQueries
		entry.TotForwardedResponses = counters.TotForwardedResponses
		entry.MTProtoProxyErrors = counters.MTProtoProxyErrors
		entry.HTTPConnections = counters.HTTPConnections
		entry.ExtConnections = counters.ExtConnections
		health[state.Plan.InboundID] = entry
		nextPrev[state.Plan.InboundID] = counters

		delta := mtproxyCounters{
			ActiveRPCs:            counters.ActiveRPCs,
			TotForwardedQueries:   maxInt64(0, counters.TotForwardedQueries-prev[state.Plan.InboundID].TotForwardedQueries),
			TotForwardedResponses: maxInt64(0, counters.TotForwardedResponses-prev[state.Plan.InboundID].TotForwardedResponses),
			MTProtoProxyErrors:    maxInt64(0, counters.MTProtoProxyErrors-prev[state.Plan.InboundID].MTProtoProxyErrors),
			HTTPConnections:       counters.HTTPConnections,
			ExtConnections:        counters.ExtConnections,
		}
		inboundID := state.Plan.InboundID
		events = append(events, controlapi.SessionEvent{
			EventType: "mtproxy_stats",
			NodeID:    nodeID,
			Protocol:  "mtproxy",
			InboundID: &inboundID,
			Status:    "ok",
			Payload: map[string]any{
				"transportMode":           state.Plan.TransportMode,
				"proxyTag":                state.Plan.ProxyTag,
				"tlsDomains":              state.Plan.TLSDomains,
				"workers":                 state.Plan.Workers,
				"activeRpcs":              counters.ActiveRPCs,
				"totForwardedQueries":     counters.TotForwardedQueries,
				"totForwardedResponses":   counters.TotForwardedResponses,
				"mtprotoProxyErrors":      counters.MTProtoProxyErrors,
				"httpConnections":         counters.HTTPConnections,
				"extConnections":          counters.ExtConnections,
				"deltaForwardedQueries":   delta.TotForwardedQueries,
				"deltaForwardedResponses": delta.TotForwardedResponses,
				"deltaProxyErrors":        delta.MTProtoProxyErrors,
			},
			CreatedAt: now,
		})
	}

	s.mu.Lock()
	s.mtproxyHealth = health
	for key := range s.mtproxyPrevStats {
		delete(s.mtproxyPrevStats, key)
	}
	for key, value := range nextPrev {
		s.mtproxyPrevStats[key] = value
	}
	snapshot := s.copyMTProxyHealthLocked()
	s.mu.Unlock()
	return events, snapshot
}

func (s *Supervisor) copyMTProxyHealthLocked() map[string]controlapi.MTProxyInboundHealth {
	out := make(map[string]controlapi.MTProxyInboundHealth, len(s.mtproxyHealth))
	for key, value := range s.mtproxyHealth {
		value.TLSDomains = append([]string{}, value.TLSDomains...)
		out[key] = value
	}
	return out
}

func ensureMTProxyAssets(stateDir string) (mtproxyAssetFiles, []string, error) {
	dir := filepath.Join(stateDir, "mtproxy-assets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return mtproxyAssetFiles{}, nil, err
	}
	files := mtproxyAssetFiles{
		ProxySecretPath: filepath.Join(dir, "proxy-secret"),
		ProxyConfigPath: filepath.Join(dir, "proxy-multi.conf"),
	}
	warnings := []string{}
	if warning, err := refreshMTProxyAsset(files.ProxySecretPath, mtproxyAssetSecretURL); err != nil {
		return files, warnings, err
	} else if warning != "" {
		warnings = append(warnings, warning)
	}
	if warning, err := refreshMTProxyAsset(files.ProxyConfigPath, mtproxyAssetConfigURL); err != nil {
		return files, warnings, err
	} else if warning != "" {
		warnings = append(warnings, warning)
	}
	return files, warnings, nil
}

func refreshMTProxyAsset(path, sourceURL string) (string, error) {
	info, err := os.Stat(path)
	needsRefresh := err != nil || time.Since(info.ModTime()) >= mtproxyStatsFreshness
	if !needsRefresh {
		return "", nil
	}
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return fmt.Sprintf("mtproxy asset refresh failed for %s, using cached copy", filepath.Base(path)), nil
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if _, statErr := os.Stat(path); statErr == nil {
			return fmt.Sprintf("mtproxy asset refresh failed for %s with status %d, using cached copy", filepath.Base(path), resp.StatusCode), nil
		}
		return "", fmt.Errorf("mtproxy asset refresh failed for %s with status %d", filepath.Base(path), resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return "", nil
}

func mtproxySettingsFromSpec(spec map[string]any) controlapi.InboundMTProxySettings {
	values, _ := spec["mtproxySettings"].(map[string]any)
	settings := controlapi.InboundMTProxySettings{
		TransportMode: strings.ToLower(strings.TrimSpace(stringFromAny(values["transportMode"]))),
		ProxyTag:      strings.TrimSpace(stringFromAny(values["proxyTag"])),
		TLSDomains:    stringSliceFromAny(values["tlsDomains"]),
		Workers:       intFromAny(values["workers"]),
		PublicAddress: strings.TrimSpace(stringFromAny(values["publicAddress"])),
	}
	if settings.TransportMode == "" {
		settings.TransportMode = "secure"
	}
	if settings.Workers <= 0 {
		if settings.TransportMode == "tls_transport" {
			settings.Workers = 1
		} else {
			settings.Workers = 2
		}
	}
	if settings.TransportMode == "tls_transport" {
		settings.Workers = 1
	}
	return settings
}

func collectMTProxySecrets(clients []controlapi.ManagedResource) []string {
	out := make([]string, 0, len(clients))
	seen := map[string]struct{}{}
	for _, client := range clients {
		secret := normalizeMTProxyStoredSecret(stringFromAny(client.Spec["secret"]))
		if secret == "" {
			continue
		}
		if _, ok := seen[secret]; ok {
			continue
		}
		seen[secret] = struct{}{}
		out = append(out, secret)
	}
	return out
}

func normalizeMTProxyStoredSecret(secret string) string {
	secret = strings.ToLower(strings.TrimSpace(secret))
	switch {
	case len(secret) == 32 && isMTProxyHex(secret):
		return secret
	case strings.HasPrefix(secret, "dd") && len(secret) == 34 && isMTProxyHex(secret[2:]):
		return secret[2:]
	case strings.HasPrefix(secret, "ee") && len(secret) >= 34 && isMTProxyHex(secret[2:34]):
		return secret[2:34]
	default:
		return ""
	}
}

func isMTProxyHex(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func pickMTProxyStatsPort(used map[int]struct{}, minPort, maxPort int) (int, error) {
	start := maxPort + 1
	if start < 30000 {
		start = 30000
	}
	end := start + 4096
	if minPort > 0 && end < minPort {
		end = minPort + 4096
	}
	for port := start; port < end; port++ {
		if _, ok := used[port]; ok {
			continue
		}
		if !portAvailableLocal(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("failed to allocate mtproxy stats port in %d-%d", start, end)
}

func portAvailableLocal(port int) bool {
	tcp, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = tcp.Close()
	return true
}

func writeMTProxyManifest(plan mtproxyProcessPlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.ManifestPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(plan.ManifestPath, raw, 0o600)
}

func fetchMTProxyCounters(port int) (mtproxyCounters, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/stats", port))
	if err != nil {
		return mtproxyCounters{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return mtproxyCounters{}, fmt.Errorf("mtproxy stats request failed with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mtproxyCounters{}, err
	}
	return parseMTProxyCounters(string(body)), nil
}

func parseMTProxyCounters(raw string) mtproxyCounters {
	counters := mtproxyCounters{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "active_rpcs":
			counters.ActiveRPCs = value
		case "tot_forwarded_queries":
			counters.TotForwardedQueries = value
		case "tot_forwarded_responses":
			counters.TotForwardedResponses = value
		case "mtproto_proxy_errors":
			counters.MTProtoProxyErrors = value
		case "http_connections":
			counters.HTTPConnections = value
		case "ext_connections":
			counters.ExtConnections = value
		}
	}
	return counters
}

func anyMTProxyDegraded(values map[string]controlapi.MTProxyInboundHealth) bool {
	for _, value := range values {
		if strings.TrimSpace(value.Status) != "running" {
			return true
		}
	}
	return false
}

func stringFromAny(raw any) string {
	if raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func intFromAny(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		value, err := typed.Int64()
		if err == nil {
			return int(value)
		}
	case string:
		value, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return value
		}
	}
	return 0
}

func stringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if trimmed := strings.TrimSpace(fmt.Sprint(value)); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return []string{}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
