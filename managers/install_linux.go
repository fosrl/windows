package managers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
)

// On Linux each tunnel runs as a child process of the daemon,
// "pangolin-client --tunnel <config>", in place of a Windows service.

const tunnelStopTimeout = 30 * time.Second

// errNotImplemented marks features that aren't available on Linux yet.
var errNotImplemented = errors.New("not implemented on Linux yet")

type tunnelProcess struct {
	cmd        *exec.Cmd
	configPath string
	exited     chan struct{}
}

var (
	tunnelProcs     = make(map[string]*tunnelProcess)
	tunnelProcsLock sync.Mutex
)

func tunnelConfigDir() string {
	return filepath.Join(config.RuntimeDir(), "tunnels")
}

// InstallTunnel starts a tunnel process for configJSON, replacing any running
// tunnel of the same name.
func InstallTunnel(configJSON string) error {
	var tunnelConfig struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(configJSON), &tunnelConfig); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	name := tunnelConfig.Name
	if name == "" {
		name = "pangolin-tunnel"
	}
	if err := UninstallTunnel(name); err != nil {
		return fmt.Errorf("failed to stop running tunnel: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := tunnelConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	configPath := filepath.Join(dir, sanitizeTunnelName(name)+".json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	cmd := exec.Command(exe, "--tunnel", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Tear the tunnel down if the daemon dies without stopping it.
		Pdeathsig: syscall.SIGTERM,
	}
	if err := cmd.Start(); err != nil {
		os.Remove(configPath)
		return fmt.Errorf("failed to start tunnel process: %w", err)
	}
	proc := &tunnelProcess{cmd: cmd, configPath: configPath, exited: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		logger.Info("Tunnel process %q (pid %d) exited: %v", name, cmd.Process.Pid, err)
		close(proc.exited)
		tunnelProcsLock.Lock()
		if tunnelProcs[name] == proc {
			delete(tunnelProcs, name)
		}
		tunnelProcsLock.Unlock()
		os.Remove(configPath)
	}()

	tunnelProcsLock.Lock()
	tunnelProcs[name] = proc
	tunnelProcsLock.Unlock()
	logger.Info("Started tunnel process %q (pid %d)", name, cmd.Process.Pid)
	return nil
}

// UninstallTunnel stops the tunnel process called name and waits for it to
// exit, so the tunnel only reads as stopped once another can start.
func UninstallTunnel(name string) error {
	tunnelProcsLock.Lock()
	proc := tunnelProcs[name]
	tunnelProcsLock.Unlock()
	if proc == nil {
		return nil
	}
	if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		logger.Error("Failed to signal tunnel process %q: %v", name, err)
	}
	select {
	case <-proc.exited:
	case <-time.After(tunnelStopTimeout):
		logger.Error("Timed out waiting for tunnel process %q to stop, killing it", name)
		_ = proc.cmd.Process.Kill()
		<-proc.exited
	}
	return nil
}

// stopAllTunnelProcesses stops every tunnel, for daemon shutdown.
func stopAllTunnelProcesses() {
	tunnelProcsLock.Lock()
	names := make([]string, 0, len(tunnelProcs))
	for name := range tunnelProcs {
		names = append(names, name)
	}
	tunnelProcsLock.Unlock()
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Go(func() { _ = UninstallTunnel(name) })
	}
	wg.Wait()
}

func sanitizeTunnelName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}
