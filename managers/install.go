//go:build windows

package managers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var cachedServiceManager *mgr.Mgr

func serviceManager() (*mgr.Mgr, error) {
	if cachedServiceManager != nil {
		return cachedServiceManager, nil
	}
	m, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	cachedServiceManager = m
	return cachedServiceManager, nil
}

var ErrManagerAlreadyRunning = errors.New("Manager already installed and running")

func InstallManager() error {
	m, err := serviceManager()
	if err != nil {
		return err
	}
	path, err := os.Executable()
	if err != nil {
		return nil
	}

	// TODO: Do we want to bail if executable isn't being run from the right location?

	serviceName := config.AppName + "Manager"
	service, err := m.OpenService(serviceName)
	if err == nil {
		status, err := service.Query()
		if err != nil {
			service.Close()
			return err
		}
		if status.State != svc.Stopped {
			service.Close()
			if status.State == svc.StartPending {
				// We were *just* started by something else, so return success here, assuming the other program
				// starting this does the right thing. This can happen when, e.g., the updater relaunches the
				// manager service and then invokes the executable to raise the UI.
				return nil
			}
			return ErrManagerAlreadyRunning
		}
		err = service.Delete()
		service.Close()
		if err != nil {
			return err
		}
		for {
			service, err = m.OpenService(serviceName)
			if err != nil {
				break
			}
			service.Close()
			time.Sleep(time.Second / 3)
		}
	}

	svcConfig := mgr.Config{
		ServiceType:  windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
		DisplayName:  config.AppName + " Manager",
	}

	service, err = m.CreateService(serviceName, path, svcConfig, "/managerservice")
	if err != nil {
		return err
	}
	service.Start()
	return service.Close()
}

func UninstallManager() error {
	m, err := serviceManager()
	if err != nil {
		return err
	}
	serviceName := config.AppName + "Manager"
	service, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	service.Control(svc.Stop)
	err = service.Delete()
	err2 := service.Close()
	if err != nil {
		return err
	}
	return err2
}

// InstallTunnel creates a Windows service for a tunnel
func InstallTunnel(configJSON string) error {
	m, err := serviceManager()
	if err != nil {
		return err
	}
	path, err := os.Executable()
	if err != nil {
		return err
	}

	// Parse config to get tunnel name
	var tunnelConfig map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &tunnelConfig); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	name, ok := tunnelConfig["name"].(string)
	if !ok || name == "" {
		name = "pangolin-tunnel" // Default name
	}

	// Create service name (Windows service names have restrictions)
	serviceName := config.AppName + "Tunnel$" + sanitizeServiceName(name)
	if len(serviceName) > 80 {
		serviceName = serviceName[:80]
	}

	// Check if service already exists
	service, err := m.OpenService(serviceName)
	if err == nil {
		status, err := service.Query()
		if err != nil && err != windows.ERROR_SERVICE_MARKED_FOR_DELETE {
			service.Close()
			return err
		}
		if status.State != svc.Stopped && err != windows.ERROR_SERVICE_MARKED_FOR_DELETE {
			// A service that is already stopping can't take another Stop; just
			// wait for it to finish.
			if status.State != svc.StopPending {
				if _, ctrlErr := service.Control(svc.Stop); ctrlErr != nil && !stopInProgress(ctrlErr) {
					service.Close()
					return fmt.Errorf("failed to stop running tunnel service: %w", ctrlErr)
				}
			}
			if !waitForServiceStopped(service, serviceStopTimeout) {
				service.Close()
				return errors.New("timed out waiting for tunnel service to stop")
			}
		}
		err = service.Delete()
		service.Close()
		if err != nil && err != windows.ERROR_SERVICE_MARKED_FOR_DELETE {
			return err
		}
		// Wait for service to be fully deleted
		for {
			service, err = m.OpenService(serviceName)
			if err != nil && err != windows.ERROR_SERVICE_MARKED_FOR_DELETE {
				break
			}
			if service != nil {
				service.Close()
			}
			time.Sleep(time.Second / 3)
		}
	}

	// Save config to temp file to pass to service
	configDir := filepath.Join(os.Getenv("ProgramData"), config.AppName, "Tunnels")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	configPath := filepath.Join(configDir, name+".json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	svcConfig := mgr.Config{
		ServiceType:  windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:    mgr.StartManual,
		ErrorControl: mgr.ErrorNormal,
		Dependencies: []string{"Nsi", "TcpIp"},
		DisplayName:  config.AppName + " Tunnel: " + name,
		SidType:      windows.SERVICE_SID_TYPE_UNRESTRICTED,
	}

	// Create service with /tunnelservice argument and config path
	service, err = m.CreateService(serviceName, path, svcConfig, "/tunnelservice", configPath)
	if err != nil {
		return err
	}

	err = service.Start()
	service.Close()
	return err
}

// UninstallTunnel removes a Windows service for a tunnel
func UninstallTunnel(name string) error {
	m, err := serviceManager()
	if err != nil {
		return err
	}

	serviceName := config.AppName + "Tunnel$" + sanitizeServiceName(name)
	if len(serviceName) > 80 {
		serviceName = serviceName[:80]
	}

	service, err := m.OpenService(serviceName)
	if err != nil {
		if err == windows.ERROR_SERVICE_DOES_NOT_EXIST {
			return nil // Already uninstalled
		}
		return err
	}
	// Wait for the service to actually stop, so the tunnel only reads as
	// stopped once another one can be started.
	if _, ctrlErr := service.Control(svc.Stop); ctrlErr == nil || stopInProgress(ctrlErr) {
		if !waitForServiceStopped(service, serviceStopTimeout) {
			logger.Error("Timed out waiting for tunnel service %s to stop", serviceName)
		}
	}
	err = service.Delete()
	service.Close()
	if err != nil && err != windows.ERROR_SERVICE_MARKED_FOR_DELETE {
		return err
	}
	waitForServiceDeleted(m, serviceName, serviceDeleteTimeout)

	// Clean up config file
	configDir := filepath.Join(os.Getenv("ProgramData"), config.AppName, "Tunnels")
	configPath := filepath.Join(configDir, name+".json")
	os.Remove(configPath) // Best effort cleanup

	return nil
}

const (
	serviceStopTimeout   = 30 * time.Second
	serviceDeleteTimeout = 10 * time.Second
)

// stopInProgress reports whether a Stop control failed only because the
// service is already stopping or stopped.
func stopInProgress(err error) bool {
	return errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) || errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE)
}

// waitForServiceDeleted waits until a service marked for deletion is gone.
func waitForServiceDeleted(m *mgr.Mgr, name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		service, err := m.OpenService(name)
		if err != nil && err != windows.ERROR_SERVICE_MARKED_FOR_DELETE {
			return
		}
		if service != nil {
			service.Close()
		}
		time.Sleep(time.Second / 3)
	}
}

func waitForServiceStopped(service *mgr.Service, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return false
		}
		if status.State == svc.Stopped {
			return true
		}
		time.Sleep(time.Second / 3)
	}
	return false
}

// sanitizeServiceName removes invalid characters from service name
func sanitizeServiceName(name string) string {
	// Windows service names can only contain: letters, numbers, and: -_()[]{}
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}
