package runtimeapply

import (
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReconcileProcessLockedWaitsForExitAndRestartsOnSamePort(t *testing.T) {
	supervisor := NewSupervisor(Config{StateDir: t.TempDir()}, nil)
	processName := "helper-restart"
	proc := &managedProcess{
		name:    processName,
		logPath: filepath.Join(t.TempDir(), "logs", "helper-restart.log"),
	}
	supervisor.process[processName] = proc

	addr := reserveTCPAddress(t)
	readyOne := filepath.Join(t.TempDir(), "ready-one")
	readyTwo := filepath.Join(t.TempDir(), "ready-two")

	configureHelperProcess(t, "graceful", addr, readyOne, 1200*time.Millisecond)
	_, _, err := reconcileTestProcessLocked(supervisor, proc, helperProcessArgs(), true)
	if err != nil {
		t.Fatalf("first reconcileProcessLocked returned error: %v", err)
	}
	waitForFile(t, readyOne, 3*time.Second)

	startedAt := time.Now()
	configureHelperProcess(t, "graceful", addr, readyTwo, 1200*time.Millisecond)
	running, _, err := reconcileTestProcessLocked(supervisor, proc, helperProcessArgs(), true)
	if err != nil {
		t.Fatalf("second reconcileProcessLocked returned error: %v", err)
	}
	if !running {
		t.Fatal("second reconcileProcessLocked reported not running, want running")
	}
	if elapsed := time.Since(startedAt); elapsed < time.Second {
		t.Fatalf("restart elapsed = %s, want at least 1s to prove graceful wait", elapsed)
	}
	waitForFile(t, readyTwo, 3*time.Second)

	if _, _, err := stopTestProcessLocked(supervisor, proc); err != nil {
		t.Fatalf("final stopProcessLocked returned error: %v", err)
	}
}

func TestStopProcessLockedReturnsCleanlyWhenAlreadyStopped(t *testing.T) {
	supervisor := NewSupervisor(Config{StateDir: t.TempDir()}, nil)
	proc := &managedProcess{name: "helper-stopped"}
	supervisor.process[proc.name] = proc

	_, _, err := stopTestProcessLocked(supervisor, proc)
	if err != nil {
		t.Fatalf("stopProcessLocked returned error: %v", err)
	}
	if proc.running {
		t.Fatal("proc.running = true, want false")
	}
	if proc.desired {
		t.Fatal("proc.desired = true, want false")
	}
	if proc.cmd != nil {
		t.Fatal("proc.cmd is not nil after stop")
	}
}

func TestStopProcessLockedKillsProcessThatIgnoresInterrupt(t *testing.T) {
	supervisor := NewSupervisor(Config{StateDir: t.TempDir()}, nil)
	processName := "helper-ignore"
	proc := &managedProcess{
		name:    processName,
		logPath: filepath.Join(t.TempDir(), "logs", "helper-ignore.log"),
	}
	supervisor.process[processName] = proc

	addr := reserveTCPAddress(t)
	ready := filepath.Join(t.TempDir(), "ready-ignore")
	configureHelperProcess(t, "ignore-sigint", addr, ready, 0)
	_, _, err := reconcileTestProcessLocked(supervisor, proc, helperProcessArgs(), true)
	if err != nil {
		t.Fatalf("reconcileProcessLocked returned error: %v", err)
	}
	waitForFile(t, ready, 3*time.Second)

	startedAt := time.Now()
	_, _, err = stopTestProcessLocked(supervisor, proc)
	if err != nil {
		t.Fatalf("stopProcessLocked returned error: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed < processGracefulStopTimeout {
		t.Fatalf("stop elapsed = %s, want at least graceful timeout %s", elapsed, processGracefulStopTimeout)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s still busy after forced stop: %v", addr, err)
	}
	_ = listener.Close()
}

func TestSupervisorHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	mode := os.Getenv("GO_HELPER_MODE")
	addr := os.Getenv("GO_HELPER_ADDR")
	readyFile := os.Getenv("GO_HELPER_READY_FILE")
	delayMs, err := strconv.Atoi(os.Getenv("GO_HELPER_DELAY_MS"))
	if err != nil {
		t.Fatalf("parse delay: %v", err)
	}

	if mode == "ignore-sigint" {
		signalIgnoreInterrupt()
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	if err := os.WriteFile(readyFile, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write ready file: %v", err)
	}

	if mode == "ignore-sigint" {
		for {
			time.Sleep(time.Hour)
		}
	}

	waitForInterrupt()
	if delayMs > 0 {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}
	_ = listener.Close()
}

func helperProcessArgs() []string {
	return []string{"-test.run=TestSupervisorHelperProcess"}
}

func configureHelperProcess(t *testing.T, mode, addr, readyFile string, delay time.Duration) {
	t.Helper()
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_MODE", mode)
	t.Setenv("GO_HELPER_ADDR", addr)
	t.Setenv("GO_HELPER_READY_FILE", readyFile)
	t.Setenv("GO_HELPER_DELAY_MS", strconv.Itoa(int(delay/time.Millisecond)))
}

func reconcileTestProcessLocked(s *Supervisor, proc *managedProcess, args []string, required bool) (bool, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reconcileProcessLocked(proc, os.Args[0], args, required)
}

func stopTestProcessLocked(s *Supervisor, proc *managedProcess) (bool, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.stopProcessLocked(proc)
	return proc.running, nil, err
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserveTCPAddress listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func signalIgnoreInterrupt() {
	signal.Ignore(os.Interrupt)
}

func waitForInterrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	<-signals
}
