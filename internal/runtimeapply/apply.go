package runtimeapply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jourloy/0trace0-node/internal/controlapi"
	"github.com/jourloy/0trace0-node/internal/runtime"
)

const (
	xrayBinaryPath    = "/usr/local/bin/xray"
	singboxBinaryPath = "/usr/local/bin/sing-box"
	mtproxyBinaryPath = "/usr/local/bin/mtproto-proxy"

	processGracefulStopTimeout = 2 * time.Second
	processForcedStopTimeout   = 2 * time.Second
)

type Config struct {
	StateDir string
}

type Result struct {
	Revision        string
	AssignedPorts   map[string]int
	Warnings        []string
	Health          map[string]any
	MTProxyInbounds map[string]controlapi.MTProxyInboundHealth
}

type ProcessSnapshot struct {
	Running   bool
	Desired   bool
	LastError string
}

type Snapshot struct {
	Processes       map[string]ProcessSnapshot
	MTProxyInbounds map[string]controlapi.MTProxyInboundHealth
}

type Supervisor struct {
	cfg               Config
	logger            *slog.Logger
	mu                sync.Mutex
	process           map[string]*managedProcess
	mtproxyStatsPorts map[string]int
	mtproxyHealth     map[string]controlapi.MTProxyInboundHealth
	mtproxyPrevStats  map[string]mtproxyCounters
	mtproxyPlans      map[string]mtproxyProcessPlan
}

type managedProcess struct {
	name       string
	binary     string
	args       []string
	configPath string
	logPath    string
	cmd        *exec.Cmd
	exitCh     chan struct{}
	running    bool
	desired    bool
	generation int
	lastError  string
}

func NewSupervisor(cfg Config, logger *slog.Logger) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		cfg:    cfg,
		logger: logger,
		process: map[string]*managedProcess{
			"xray": {
				name:       "xray",
				logPath:    filepath.Join(cfg.StateDir, "logs", "xray.log"),
				configPath: filepath.Join(cfg.StateDir, "rendered", "xray.json"),
			},
			"singbox": {
				name:       "singbox",
				logPath:    filepath.Join(cfg.StateDir, "logs", "singbox.log"),
				configPath: filepath.Join(cfg.StateDir, "rendered", "singbox.json"),
			},
		},
		mtproxyStatsPorts: map[string]int{},
		mtproxyHealth:     map[string]controlapi.MTProxyInboundHealth{},
		mtproxyPrevStats:  map[string]mtproxyCounters{},
		mtproxyPlans:      map[string]mtproxyProcessPlan{},
	}
}

func BundleChecksum(bundle controlapi.ConfigBundle, ports map[string]int) string {
	return bundleChecksum(bundle, ports)
}

func ApplyBundle(bundle *controlapi.ConfigBundle, previousPorts map[string]int, cfg Config) (Result, error) {
	return NewSupervisor(cfg, nil).ApplyBundle(bundle, previousPorts)
}

func (s *Supervisor) ApplyBundle(bundle *controlapi.ConfigBundle, previousPorts map[string]int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, xrayPath, singboxPath, bundlePath, err := renderBundle(bundle, previousPorts, s.cfg)
	if err != nil {
		return result, err
	}

	xrayRequired, singboxRequired := requiredEngines(*bundle)
	result.Health = map[string]any{
		"xray_required":     xrayRequired,
		"singbox_required":  singboxRequired,
		"xray_running":      false,
		"singbox_running":   false,
		"mtproxy_running":   0,
		"xrayConfigPath":    xrayPath,
		"singboxConfigPath": singboxPath,
		"config_applied":    true,
		"cert_pending":      warningContains(result.Warnings, "pending certificate"),
		"host_unresolved":   warningContains(result.Warnings, "host unresolved"),
		"last_apply_error":  "",
	}

	xrayRunning, xrayWarnings, xrayErr := s.reconcileProcessLocked(
		s.process["xray"],
		xrayBinaryPath,
		[]string{"run", "-config", xrayPath},
		xrayRequired,
	)
	result.Warnings = append(result.Warnings, xrayWarnings...)
	if xrayErr != nil {
		result.Health["last_apply_error"] = xrayErr.Error()
		_ = restoreBackup(xrayPath)
		_ = restoreBackup(singboxPath)
		_ = restoreBackup(bundlePath)
		_, _, _ = s.reconcileProcessLocked(s.process["xray"], xrayBinaryPath, []string{"run", "-config", xrayPath}, xrayRequired)
		_, _, _ = s.reconcileProcessLocked(s.process["singbox"], singboxBinaryPath, []string{"run", "-c", singboxPath}, singboxRequired)
		return result, fmt.Errorf("xray start failed: %w", xrayErr)
	}
	result.Health["xray_running"] = xrayRunning

	singboxRunning, singboxWarnings, singboxErr := s.reconcileProcessLocked(
		s.process["singbox"],
		singboxBinaryPath,
		[]string{"run", "-c", singboxPath},
		singboxRequired,
	)
	result.Warnings = append(result.Warnings, singboxWarnings...)
	if singboxErr != nil {
		result.Health["last_apply_error"] = singboxErr.Error()
		_ = restoreBackup(xrayPath)
		_ = restoreBackup(singboxPath)
		_ = restoreBackup(bundlePath)
		_, _, _ = s.reconcileProcessLocked(s.process["xray"], xrayBinaryPath, []string{"run", "-config", xrayPath}, xrayRequired)
		_, _, _ = s.reconcileProcessLocked(s.process["singbox"], singboxBinaryPath, []string{"run", "-c", singboxPath}, singboxRequired)
		return result, fmt.Errorf("sing-box start failed: %w", singboxErr)
	}
	result.Health["singbox_running"] = singboxRunning

	mtproxyWarnings, mtproxyErr := s.reconcileMTProxyLocked(*bundle, result.AssignedPorts)
	result.Warnings = append(result.Warnings, mtproxyWarnings...)
	result.MTProxyInbounds = s.copyMTProxyHealthLocked()
	result.Health["mtproxy_running"] = len(result.MTProxyInbounds)
	result.Health["mtproxy_inbounds"] = result.MTProxyInbounds
	result.Health["mtproxy_degraded"] = anyMTProxyDegraded(result.MTProxyInbounds)
	if mtproxyErr != nil {
		result.Health["last_apply_error"] = mtproxyErr.Error()
		return result, fmt.Errorf("mtproxy start failed: %w", mtproxyErr)
	}

	return result, nil
}

func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	processes := make(map[string]ProcessSnapshot, len(s.process))
	for key, value := range s.process {
		processes[key] = ProcessSnapshot{
			Running:   value.running,
			Desired:   value.desired,
			LastError: value.lastError,
		}
	}

	return Snapshot{
		Processes:       processes,
		MTProxyInbounds: s.copyMTProxyHealthLocked(),
	}
}

func renderBundle(bundle *controlapi.ConfigBundle, previousPorts map[string]int, cfg Config) (Result, string, string, string, error) {
	result := Result{
		AssignedPorts: map[string]int{},
		Warnings:      make([]string, 0),
		Health:        map[string]any{},
	}

	_ = previousPorts
	assigned, err := runtime.AssignPorts(*bundle)
	if err != nil {
		return result, "", "", "", err
	}

	xrayConfig, xrayWarnings, err := runtime.RenderXray(*bundle, assigned)
	if err != nil {
		return result, "", "", "", err
	}
	singboxConfig, singboxWarnings, err := runtime.RenderSingbox(*bundle, assigned)
	if err != nil {
		return result, "", "", "", err
	}

	result.AssignedPorts = assigned
	result.Revision = bundleChecksum(*bundle, assigned)
	result.Warnings = append(result.Warnings, xrayWarnings...)
	result.Warnings = append(result.Warnings, singboxWarnings...)

	renderedDir := filepath.Join(cfg.StateDir, "rendered")
	if err := os.MkdirAll(renderedDir, 0o755); err != nil {
		return result, "", "", "", err
	}

	xrayPath := filepath.Join(renderedDir, "xray.json")
	singboxPath := filepath.Join(renderedDir, "singbox.json")
	bundlePath := filepath.Join(renderedDir, "bundle.json")

	if err := swapWithBackup(xrayPath, xrayConfig); err != nil {
		return result, "", "", "", err
	}
	if err := swapWithBackup(singboxPath, singboxConfig); err != nil {
		_ = restoreBackup(xrayPath)
		return result, "", "", "", err
	}
	bundleRaw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return result, "", "", "", err
	}
	if err := swapWithBackup(bundlePath, bundleRaw); err != nil {
		_ = restoreBackup(xrayPath)
		_ = restoreBackup(singboxPath)
		return result, "", "", "", err
	}

	if warnings, err := validateRenderedConfig(xrayBinaryPath, []string{"run", "-test", "-config", xrayPath}); err != nil {
		_ = restoreBackup(xrayPath)
		_ = restoreBackup(singboxPath)
		_ = restoreBackup(bundlePath)
		return result, "", "", "", fmt.Errorf("xray validation failed: %w", err)
	} else {
		result.Warnings = append(result.Warnings, warnings...)
	}

	if warnings, err := validateRenderedConfig(singboxBinaryPath, []string{"check", "-c", singboxPath}); err != nil {
		_ = restoreBackup(xrayPath)
		_ = restoreBackup(singboxPath)
		_ = restoreBackup(bundlePath)
		return result, "", "", "", fmt.Errorf("sing-box validation failed: %w", err)
	} else {
		result.Warnings = append(result.Warnings, warnings...)
	}

	return result, xrayPath, singboxPath, bundlePath, nil
}

func requiredEngines(bundle controlapi.ConfigBundle) (bool, bool) {
	xrayRequired := false
	singboxRequired := false
	for _, inbound := range bundle.Resources[string(controlapi.KindInbound)] {
		switch strings.ToLower(strings.TrimSpace(pointerString(inbound.Protocol))) {
		case "trojan", "vless", "http", "socks5":
			xrayRequired = true
		case "hysteria2", "wireguard":
			singboxRequired = true
		}
	}
	for _, outbound := range bundle.Resources[string(controlapi.KindOutbound)] {
		switch strings.ToLower(strings.TrimSpace(pointerString(outbound.Protocol))) {
		case "http_proxy", "socks_proxy", "trojan_chain", "vless_chain":
			xrayRequired = true
		case "wireguard_tunnel", "selector", "fallback", "hysteria2_chain":
			singboxRequired = true
		}
	}
	return xrayRequired, singboxRequired
}

func (s *Supervisor) reconcileProcessLocked(proc *managedProcess, binary string, args []string, required bool) (bool, []string, error) {
	warnings := []string{}
	if proc == nil {
		return false, warnings, nil
	}
	if !required {
		if err := s.stopProcessLocked(proc); err != nil {
			return false, warnings, err
		}
		proc.lastError = ""
		return false, warnings, nil
	}

	path, err := exec.LookPath(binary)
	if err != nil {
		proc.desired = false
		proc.running = false
		proc.lastError = fmt.Sprintf("%s binary not found", proc.name)
		warnings = append(warnings, fmt.Sprintf("%s runtime skipped: %s not found", proc.name, binary))
		return false, warnings, nil
	}

	if err := s.stopProcessLocked(proc); err != nil {
		return false, warnings, err
	}
	if err := os.MkdirAll(filepath.Dir(proc.logPath), 0o755); err != nil {
		return false, warnings, err
	}
	logFile, err := os.OpenFile(proc.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, warnings, err
	}
	command := exec.Command(path, args...)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		proc.lastError = err.Error()
		return false, warnings, err
	}

	exitCh := make(chan struct{})
	proc.binary = path
	proc.args = append([]string{}, args...)
	proc.cmd = command
	proc.exitCh = exitCh
	proc.running = true
	proc.desired = true
	proc.lastError = ""
	proc.generation++
	generation := proc.generation
	go s.watchProcess(proc.name, command, logFile, generation, exitCh)
	return true, warnings, nil
}

func (s *Supervisor) stopProcessLocked(proc *managedProcess) error {
	if proc == nil {
		return nil
	}
	if proc.cmd == nil || proc.cmd.Process == nil {
		proc.cmd = nil
		proc.exitCh = nil
		proc.running = false
		proc.desired = false
		return nil
	}
	proc.desired = false
	command := proc.cmd
	process := command.Process
	exitCh := proc.exitCh
	generation := proc.generation

	if err := process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if !waitForExitSignal(exitCh, processGracefulStopTimeout) {
		if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		if !waitForExitSignal(exitCh, processForcedStopTimeout) {
			return fmt.Errorf("process %s did not exit after SIGINT and SIGKILL", proc.name)
		}
	}

	if proc.generation == generation && proc.cmd == command && proc.exitCh == exitCh {
		proc.cmd = nil
		proc.exitCh = nil
		proc.running = false
	}
	return nil
}

func (s *Supervisor) watchProcess(name string, cmd *exec.Cmd, logFile *os.File, generation int, exitCh chan struct{}) {
	err := cmd.Wait()
	close(exitCh)
	_ = logFile.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	proc := s.process[name]
	if proc == nil || proc.generation != generation || proc.cmd != cmd || proc.exitCh != exitCh {
		return
	}
	proc.cmd = nil
	proc.exitCh = nil
	proc.running = false
	if err != nil {
		proc.lastError = err.Error()
		s.logger.Warn("runtime process exited", "process", name, "error", err)
	} else {
		proc.lastError = ""
		s.logger.Info("runtime process exited", "process", name)
	}
	if !proc.desired {
		return
	}
	time.AfterFunc(2*time.Second, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.process[name]
		if current == nil || !current.desired || current.running || current.generation != generation {
			return
		}
		_, warnings, restartErr := s.reconcileProcessLocked(current, current.binary, current.args, true)
		for _, warning := range warnings {
			s.logger.Warn("runtime restart warning", "process", name, "warning", warning)
		}
		if restartErr != nil {
			current.lastError = restartErr.Error()
			s.logger.Error("runtime restart failed", "process", name, "error", restartErr)
		}
	})
}

func waitForExitSignal(exitCh <-chan struct{}, timeout time.Duration) bool {
	if exitCh == nil {
		return true
	}
	select {
	case <-exitCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

func validateRenderedConfig(binary string, args []string) ([]string, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return []string{fmt.Sprintf("validation skipped: %s not found", binary)}, nil
	}
	command := exec.Command(path, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, string(output))
	}
	return []string{}, nil
}

func swapWithBackup(path string, content []byte) error {
	backupPath := path + ".bak"
	if current, err := os.ReadFile(path); err == nil {
		if writeErr := os.WriteFile(backupPath, current, 0o600); writeErr != nil {
			return writeErr
		}
	}
	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tempFile, path)
}

func restoreBackup(path string) error {
	backupPath := path + ".bak"
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func bundleChecksum(bundle controlapi.ConfigBundle, ports map[string]int) string {
	payload := map[string]any{
		"bundle": bundle,
		"ports":  ports,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func warningContains(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
