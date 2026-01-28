//go:build windows

package managers

import (
	"bytes"
	"encoding/gob"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"

	"github.com/fosrl/windows/tunnel"
	"github.com/fosrl/windows/updater"
)

// RunTunnelService is exported so main.go can call it
func RunTunnelService(configJSON string) error {
	return tunnel.RunTunnelService(configJSON)
}

var (
	managerServices     = make(map[*ManagerService]bool)
	managerServicesLock sync.RWMutex
	haveQuit            uint32
	quitManagersChan    = make(chan struct{}, 1)
	activeTunnels       = make(map[string]bool) // Track active tunnel names
	activeTunnelsLock   sync.RWMutex
)

type ManagerService struct {
	events        *os.File
	eventLock     sync.Mutex
	elevatedToken windows.Token
}

func (s *ManagerService) Quit(stopTunnelsOnQuit bool) (alreadyQuit bool, err error) {
	if !atomic.CompareAndSwapUint32(&haveQuit, 0, 1) {
		return true, nil
	}

	// Work around potential race condition of delivering messages to the wrong process by removing from notifications.
	managerServicesLock.Lock()
	s.eventLock.Lock()
	s.events = nil
	s.eventLock.Unlock()
	delete(managerServices, s)
	managerServicesLock.Unlock()

	if stopTunnelsOnQuit {
		// Stop all active tunnels before quitting
		logger.Info("Quit requested with stopTunnelsOnQuit=true, stopping all tunnels")
		activeTunnelsLock.Lock()
		tunnelNames := make([]string, 0, len(activeTunnels))
		for name := range activeTunnels {
			tunnelNames = append(tunnelNames, name)
		}
		activeTunnelsLock.Unlock()

		for _, name := range tunnelNames {
			logger.Info("Stopping tunnel: %s", name)
			if err := UninstallTunnel(name); err != nil {
				logger.Error("Failed to stop tunnel %s: %v", name, err)
				// Continue stopping other tunnels even if one fails
			}
		}
		logger.Info("All tunnels stopped")
	}

	quitManagersChan <- struct{}{}
	return false, nil
}

func (s *ManagerService) UpdateState() UpdateState {
	return updateState
}

func (s *ManagerService) Update() {
	if s.elevatedToken == 0 {
		return
	}
	// Use the existing updater package's DownloadVerifyAndExecute function
	progress := updater.DownloadVerifyAndExecute(uintptr(s.elevatedToken))
	go func() {
		for {
			dp := <-progress
			IPCServerNotifyUpdateProgress(dp)
			if dp.Complete || dp.Error != nil {
				return
			}
		}
	}()
}

func (s *ManagerService) StartTunnel(config tunnel.Config) error {
	// Set up callback to notify on state changes
	tunnel.SetStateChangeCallback(func(state TunnelState) {
		IPCServerNotifyTunnelStateChange(state)
	})

	// Set up callbacks for tunnel service to call install/uninstall
	tunnel.SetInstallTunnelCallback(InstallTunnel)
	tunnel.SetUninstallTunnelCallback(func(name string) error {
		return UninstallTunnel(name)
	})

	err := tunnel.StartTunnel(config)
	if err != nil {
		return err
	}
	// Track this tunnel as active
	activeTunnelsLock.Lock()
	activeTunnels[config.Name] = true
	activeTunnelsLock.Unlock()
	// Notify UI of initial state change (starting)
	state := tunnel.GetState()
	IPCServerNotifyTunnelStateChange(state)
	return nil
}

func (s *ManagerService) StopTunnel() error {
	// Set up callback to notify on state changes
	tunnel.SetStateChangeCallback(func(state TunnelState) {
		IPCServerNotifyTunnelStateChange(state)
	})

	// Set up callbacks for tunnel service to call install/uninstall
	tunnel.SetInstallTunnelCallback(InstallTunnel)
	tunnel.SetUninstallTunnelCallback(func(name string) error {
		return UninstallTunnel(name)
	})

	err := tunnel.StopTunnel()
	if err != nil {
		return err
	}
	// Remove tunnel from active list
	// Get the tunnel name from the tunnel package
	tunnelName := tunnel.GetTunnelName()
	if tunnelName != "" {
		activeTunnelsLock.Lock()
		delete(activeTunnels, tunnelName)
		activeTunnelsLock.Unlock()
	}
	// Notify UI of initial state change (stopping)
	state := tunnel.GetState()
	IPCServerNotifyTunnelStateChange(state)
	return nil
}

func (s *ManagerService) ServeConn(reader io.Reader, writer io.Writer) {
	decoder := gob.NewDecoder(reader)
	encoder := gob.NewEncoder(writer)
	for {
		var methodType MethodType
		err := decoder.Decode(&methodType)
		if err != nil {
			return
		}
		switch methodType {
		case QuitMethodType:
			var stopTunnelsOnQuit bool
			err := decoder.Decode(&stopTunnelsOnQuit)
			if err != nil {
				return
			}
			alreadyQuit, retErr := s.Quit(stopTunnelsOnQuit)
			err = encoder.Encode(alreadyQuit)
			if err != nil {
				return
			}
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case UpdateStateMethodType:
			updateState := s.UpdateState()
			err = encoder.Encode(updateState)
			if err != nil {
				return
			}
		case UpdateMethodType:
			s.Update()
		case StartTunnelMethodType:
			var config tunnel.Config
			err := decoder.Decode(&config)
			if err != nil {
				return
			}
			retErr := s.StartTunnel(config)
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case StopTunnelMethodType:
			retErr := s.StopTunnel()
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		default:
			return
		}
	}
}

func IPCServerListen(reader, writer, events *os.File, elevatedToken windows.Token) {
	service := &ManagerService{
		events:        events,
		elevatedToken: elevatedToken,
	}

	go func() {
		managerServicesLock.Lock()
		managerServices[service] = true
		managerServicesLock.Unlock()
		service.ServeConn(reader, writer)
		managerServicesLock.Lock()
		service.eventLock.Lock()
		service.events = nil
		service.eventLock.Unlock()
		delete(managerServices, service)
		managerServicesLock.Unlock()
	}()
}

func notifyAll(notificationType NotificationType, adminOnly bool, ifaces ...any) {
	if len(managerServices) == 0 {
		return
	}

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := encoder.Encode(notificationType)
	if err != nil {
		return
	}
	for _, iface := range ifaces {
		err = encoder.Encode(iface)
		if err != nil {
			return
		}
	}

	managerServicesLock.RLock()
	for m := range managerServices {
		if m.elevatedToken == 0 && adminOnly {
			continue
		}
		go func(m *ManagerService) {
			m.eventLock.Lock()
			defer m.eventLock.Unlock()
			if m.events != nil {
				m.events.SetWriteDeadline(time.Now().Add(time.Second))
				m.events.Write(buf.Bytes())
			}
		}(m)
	}
	managerServicesLock.RUnlock()
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func IPCServerNotifyUpdateFound(state UpdateState) {
	notifyAll(UpdateFoundNotificationType, false, state)
}

func IPCServerNotifyUpdateProgress(dp updater.DownloadProgress) {
	notifyAll(UpdateProgressNotificationType, true, dp.Activity, dp.BytesDownloaded, dp.BytesTotal, errToString(dp.Error), dp.Complete)
}

func IPCServerNotifyManagerStopping() {
	notifyAll(ManagerStoppingNotificationType, false)
	time.Sleep(time.Millisecond * 200)
}

func IPCServerNotifyTunnelStateChange(state TunnelState) {
	notifyAll(TunnelStateChangeNotificationType, false, state)
}
