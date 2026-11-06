package managers

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fosrl/newt/logger"

	"github.com/fosrl/windows-linux/fingerprint"
	"github.com/fosrl/windows-linux/managers/secretstore"
	"github.com/fosrl/windows-linux/tunnel"
	"github.com/fosrl/windows-linux/updater"
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
	activeTunnels       = make(map[string]uint32) // Active tunnel names and the session that started each
	activeTunnelsLock   sync.RWMutex
	alwaysOnUsers       = make(map[string]bool)
	alwaysOnUsersLock   sync.Mutex

	postureRefresherOnce sync.Once
	secretStore          = secretstore.NewStore()
)

type ManagerService struct {
	events        *os.File
	eventLock     sync.Mutex
	elevatedToken userToken
	// clientUserKey identifies the connected user: a SID on Windows, a UID on Linux.
	clientUserKey string
	// session is the UI's Windows session ID, or its UID on Linux.
	session uint32
}

func setUserAlwaysOn(sid string, enabled bool) {
	if sid == "" {
		return
	}
	alwaysOnUsersLock.Lock()
	defer alwaysOnUsersLock.Unlock()
	if enabled {
		alwaysOnUsers[sid] = true
	} else {
		delete(alwaysOnUsers, sid)
	}
}

func userAlwaysOn(sid string) bool {
	alwaysOnUsersLock.Lock()
	defer alwaysOnUsersLock.Unlock()
	return alwaysOnUsers[sid]
}

func (s *ManagerService) SetAlwaysOn(enabled bool) {
	setUserAlwaysOn(s.clientUserKey, enabled)
	logger.Info("Always-On for %s: %v", s.clientUserKey, enabled)
}

func (s *ManagerService) AlwaysOnEnabled() bool {
	return userAlwaysOn(s.clientUserKey)
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
	state, _ := currentUpdateState()
	return state
}

func (s *ManagerService) UpdateVersion() string {
	_, version := currentUpdateState()
	return version
}

func (s *ManagerService) CheckForUpdates() (UpdateState, error) {
	update, err := updater.CheckForUpdate()
	if err != nil {
		return s.UpdateState(), err
	}
	if update != nil {
		state, _ := setFoundUpdate(update.Version())
		IPCServerNotifyUpdateFound(state)
		return state, nil
	}
	updateLock.Lock()
	defer updateLock.Unlock()
	if !updateState.HasUpdate() {
		updateState = UpdateStateUnknown
	}
	return updateState, nil
}

// AutoDownloadUpdatesTurnedOn is called after a user turns on automatic update
// downloads in Preferences, so an available update downloads now.
func (s *ManagerService) AutoDownloadUpdatesTurnedOn() {
	logger.Info("Automatic update downloads turned on from Preferences")
	autoDownloadUpdatesEnabled()
}

func (s *ManagerService) Update() {
	// Install the silently downloaded update, if any, instead of downloading again.
	staged := takeStagedUpdate()
	prepareUpdateRestart()
	progress := updater.DownloadVerifyAndExecute(uintptr(s.elevatedToken), staged)
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

func (s *ManagerService) IsCLIInstalled() bool {
	return IsCLIInstalled()
}

func (s *ManagerService) InstallCLI() error {
	token, err := authorizeCLIInstall(s)
	if err != nil {
		return err
	}
	return InstallCLI(uintptr(token))
}

func (s *ManagerService) StartTunnel(config tunnel.Config) error {
	snapshot, ok := fingerprint.CachedDevicePosture()
	if !ok {
		logger.Debug("IPC server: StartTunnel device posture cache miss, refreshing")
		fingerprint.RefreshPostureMemory()
		snapshot, ok = fingerprint.CachedDevicePosture()
	}
	if ok {
		snapshot = postureForClient(snapshot, s.clientUserKey)
		fpBytes, errFP := json.Marshal(snapshot.Fingerprint)
		postBytes, errPost := json.Marshal(snapshot.Postures)
		if errFP == nil && errPost == nil && len(fpBytes) > 0 && len(postBytes) > 0 {
			config.InitialFingerprint = fpBytes
			config.InitialPostures = postBytes
		}
	}

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
	activeTunnels[config.Name] = s.session
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

func (s *ManagerService) StopAllTunnels() error {
	tunnel.SetStateChangeCallback(func(state TunnelState) {
		IPCServerNotifyTunnelStateChange(state)
	})
	tunnel.SetInstallTunnelCallback(InstallTunnel)
	tunnel.SetUninstallTunnelCallback(func(name string) error {
		return UninstallTunnel(name)
	})

	stopActiveTunnels()
	return nil
}

// stopSessionTunnels stops the tunnels started from a session, so a tunnel
// does not stay connected with a user's credentials after they sign out.
func stopSessionTunnels(session uint32) {
	activeTunnelsLock.Lock()
	tunnelNames := make([]string, 0, len(activeTunnels))
	for name, owner := range activeTunnels {
		if owner == session {
			tunnelNames = append(tunnelNames, name)
		}
	}
	activeTunnelsLock.Unlock()

	for _, name := range tunnelNames {
		logger.Info("Session %d signed out, stopping tunnel: %s", session, name)
		if err := UninstallTunnel(name); err != nil {
			logger.Error("Failed to stop tunnel %s: %v", name, err)
			continue
		}
		activeTunnelsLock.Lock()
		if activeTunnels[name] == session {
			delete(activeTunnels, name)
		}
		activeTunnelsLock.Unlock()
		if tunnel.GetTunnelName() == name {
			tunnel.SetState(tunnel.StateStopped)
		}
		IPCServerNotifyTunnelStateChange(tunnel.StateStopped)
	}
}

// stopActiveTunnels stops every tunnel the manager started.
func stopActiveTunnels() {
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
		} else {
			activeTunnelsLock.Lock()
			delete(activeTunnels, name)
			activeTunnelsLock.Unlock()
		}
	}
}

func (s *ManagerService) GetUserSecrets(userID string) (secretstore.UserSecrets, error) {
	if s.clientUserKey == "" {
		logger.Error("IPC server: GetUserSecrets() clientUserKey empty (userId=%s)", userID)
		return secretstore.UserSecrets{}, errors.New("manager IPC client is not bound to a windows user")
	}
	secrets, err := secretStore.Load(s.clientUserKey, userID)
	if err != nil {
		logger.Error("IPC server: GetUserSecrets() failed (userId=%s): %v", userID, err)
		return secrets, err
	}
	logger.Debug("IPC server: GetUserSecrets (userId=%s, hasSessionToken=%v, hasOlmId=%v, hasOlmSecret=%v)",
		userID, secrets.SessionToken != "", secrets.OlmId != "", secrets.OlmSecret != "")
	return secrets, err
}

func (s *ManagerService) SaveUserSecrets(userID string, update secretstore.SecretsUpdate) error {
	if s.clientUserKey == "" {
		return errors.New("manager IPC client is not bound to a windows user")
	}
	err := secretStore.Save(s.clientUserKey, userID, update)
	if err != nil {
		logger.Error("IPC server: SaveUserSecrets() failed (userId=%s): %v", userID, err)
	}
	return err
}

func (s *ManagerService) DeleteUserSecrets(userID string, flags secretstore.DeleteSecretsFlags) error {
	if s.clientUserKey == "" {
		return errors.New("manager IPC client is not bound to a windows user")
	}
	err := secretStore.Delete(s.clientUserKey, userID, flags)
	return err
}

func (s *ManagerService) GetDevicePosture() (fingerprint.DevicePostureSnapshot, error) {
	snapshot, ok := fingerprint.CachedDevicePosture()
	if !ok {
		logger.Debug("IPC server: GetDevicePosture() cache miss, refreshing")
		fingerprint.RefreshPostureMemory()
		snapshot, ok = fingerprint.CachedDevicePosture()
	}
	if !ok {
		return fingerprint.DevicePostureSnapshot{}, errors.New("device posture cache is not available")
	}
	return snapshot, nil
}

func (s *ManagerService) ServeConn(reader io.Reader, writer io.Writer) {
	decoder := gob.NewDecoder(reader)
	encoder := gob.NewEncoder(writer)
	for {
		var methodType MethodType
		err := decoder.Decode(&methodType)
		if err != nil {
			logger.Debug("IPC server: ServeConn decode method type failed: %v", err)
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
		case SetAlwaysOnMethodType:
			var enabled bool
			err := decoder.Decode(&enabled)
			if err != nil {
				return
			}
			s.SetAlwaysOn(enabled)
		case AlwaysOnEnabledMethodType:
			err = encoder.Encode(s.AlwaysOnEnabled())
			if err != nil {
				return
			}
		case CheckForUpdatesMethodType:
			state, retErr := s.CheckForUpdates()
			err = encoder.Encode(state)
			if err != nil {
				return
			}
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case AutoDownloadUpdatesTurnedOnMethodType:
			s.AutoDownloadUpdatesTurnedOn()
		case UpdateVersionMethodType:
			err = encoder.Encode(s.UpdateVersion())
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
		case StopAllTunnelsMethodType:
			retErr := s.StopAllTunnels()
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case IsCLIInstalledMethodType:
			installed := s.IsCLIInstalled()
			err = encoder.Encode(installed)
			if err != nil {
				return
			}
		case InstallCLIMethodType:
			retErr := s.InstallCLI()
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case GetUserSecretsMethodType:
			var userID string
			err := decoder.Decode(&userID)
			if err != nil {
				return
			}
			secrets, retErr := s.GetUserSecrets(userID)
			err = encoder.Encode(secrets)
			if err != nil {
				return
			}
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case SaveUserSecretsMethodType:
			var userID string
			var update secretstore.SecretsUpdate
			err := decoder.Decode(&userID)
			if err != nil {
				return
			}
			err = decoder.Decode(&update)
			if err != nil {
				return
			}
			retErr := s.SaveUserSecrets(userID, update)
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case DeleteUserSecretsMethodType:
			var userID string
			var flags secretstore.DeleteSecretsFlags
			err := decoder.Decode(&userID)
			if err != nil {
				return
			}
			err = decoder.Decode(&flags)
			if err != nil {
				return
			}
			retErr := s.DeleteUserSecrets(userID, flags)
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		case GetDevicePostureMethodType:
			snapshot, retErr := s.GetDevicePosture()
			err = encoder.Encode(snapshot)
			if err != nil {
				return
			}
			err = encoder.Encode(errToString(retErr))
			if err != nil {
				return
			}
		default:
			logger.Error("IPC server: ServeConn unknown method type %d, closing connection", methodType)
			return
		}
	}
}

// IPCServerListen serves one UI's IPC connection in the background. session
// is the UI's Windows session ID, or its UID on Linux. The returned channel
// is closed once the UI disconnects.
func IPCServerListen(reader, writer, events *os.File, elevatedToken userToken, clientUserKey string, session uint32) <-chan struct{} {
	postureRefresherOnce.Do(func() {
		go func() {
			fingerprint.RefreshPostureMemory()
			ticker := time.NewTicker(3 * time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				fingerprint.RefreshPostureMemory()
			}
		}()
	})

	service := &ManagerService{
		events:        events,
		elevatedToken: elevatedToken,
		clientUserKey: clientUserKey,
		session:       session,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
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
	return done
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
	notifyAll(UpdateProgressNotificationType, false, dp.Activity, dp.BytesDownloaded, dp.BytesTotal, errToString(dp.Error), dp.Complete)
}

// IPCServerNotifyUIAction asks the UI in sessionID to perform action. Every UI
// receives it and only the one in that session acts on it.
func IPCServerNotifyUIAction(sessionID uint32, action UIAction) {
	notifyAll(UIActionNotificationType, false, sessionID, action)
}

func IPCServerNotifyManagerStopping() {
	notifyAll(ManagerStoppingNotificationType, false)
	time.Sleep(time.Millisecond * 200)
}

func IPCServerNotifyTunnelStateChange(state TunnelState) {
	notifyAll(TunnelStateChangeNotificationType, false, state)
}
