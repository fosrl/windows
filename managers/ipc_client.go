//go:build windows

package managers

import (
	"encoding/gob"
	"errors"
	"os"
	"sync"

	"github.com/fosrl/windows/tunnel"
	"github.com/fosrl/windows/updater"
)

// TunnelConfig is exported for use in UI
type TunnelConfig = tunnel.Config

type NotificationType int

const (
	ManagerStoppingNotificationType NotificationType = iota
	UpdateFoundNotificationType
	UpdateProgressNotificationType
	TunnelStateChangeNotificationType
)

type MethodType int

const (
	QuitMethodType MethodType = iota
	UpdateStateMethodType
	UpdateMethodType
	StartTunnelMethodType
	StopTunnelMethodType
	StopAllTunnelsMethodType
)

var (
	rpcEncoder *gob.Encoder
	rpcDecoder *gob.Decoder
	rpcMutex   sync.Mutex
)

type ManagerStoppingCallback struct {
	cb func()
}

var managerStoppingCallbacks = make(map[*ManagerStoppingCallback]bool)

type UpdateFoundCallback struct {
	cb func(updateState UpdateState)
}

var updateFoundCallbacks = make(map[*UpdateFoundCallback]bool)

type UpdateProgressCallback struct {
	cb func(dp updater.DownloadProgress)
}

var updateProgressCallbacks = make(map[*UpdateProgressCallback]bool)

type TunnelStateChangeCallback struct {
	cb func(state TunnelState)
}

var tunnelStateChangeCallbacks = make(map[*TunnelStateChangeCallback]bool)

func InitializeIPCClient(reader, writer, events *os.File) {
	rpcDecoder = gob.NewDecoder(reader)
	rpcEncoder = gob.NewEncoder(writer)
	go func() {
		decoder := gob.NewDecoder(events)
		for {
			var notificationType NotificationType
			err := decoder.Decode(&notificationType)
			if err != nil {
				return
			}
			switch notificationType {
			case ManagerStoppingNotificationType:
				for cb := range managerStoppingCallbacks {
					cb.cb()
				}
			case UpdateFoundNotificationType:
				var state UpdateState
				err = decoder.Decode(&state)
				if err != nil {
					continue
				}
				for cb := range updateFoundCallbacks {
					cb.cb(state)
				}
			case UpdateProgressNotificationType:
				var dp updater.DownloadProgress
				err = decoder.Decode(&dp.Activity)
				if err != nil {
					continue
				}
				err = decoder.Decode(&dp.BytesDownloaded)
				if err != nil {
					continue
				}
				err = decoder.Decode(&dp.BytesTotal)
				if err != nil {
					continue
				}
				var errStr string
				err = decoder.Decode(&errStr)
				if err != nil {
					continue
				}
				if len(errStr) > 0 {
					dp.Error = errors.New(errStr)
				}
				err = decoder.Decode(&dp.Complete)
				if err != nil {
					continue
				}
				for cb := range updateProgressCallbacks {
					cb.cb(dp)
				}
			case TunnelStateChangeNotificationType:
				var state TunnelState
				err = decoder.Decode(&state)
				if err != nil {
					continue
				}
				for cb := range tunnelStateChangeCallbacks {
					cb.cb(state)
				}
			}
		}
	}()
}

func rpcDecodeError() error {
	var str string
	err := rpcDecoder.Decode(&str)
	if err != nil {
		return err
	}
	if len(str) == 0 {
		return nil
	}
	return errors.New(str)
}

func IPCClientQuit(stopTunnelsOnQuit bool) (alreadyQuit bool, err error) {
	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	err = rpcEncoder.Encode(QuitMethodType)
	if err != nil {
		return
	}
	err = rpcEncoder.Encode(stopTunnelsOnQuit)
	if err != nil {
		return
	}
	err = rpcDecoder.Decode(&alreadyQuit)
	if err != nil {
		return
	}
	err = rpcDecodeError()
	return
}

func IPCClientUpdateState() (updateState UpdateState, err error) {
	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	err = rpcEncoder.Encode(UpdateStateMethodType)
	if err != nil {
		return
	}
	err = rpcDecoder.Decode(&updateState)
	if err != nil {
		return
	}
	return
}

func IPCClientUpdate() error {
	// Always stop any running tunnel services first
	// Ignore errors from StopTunnel as it's safe to call even if no tunnel is running
	_ = IPCClientStopTunnel()

	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	return rpcEncoder.Encode(UpdateMethodType)
}

func IPCClientRegisterManagerStopping(cb func()) *ManagerStoppingCallback {
	s := &ManagerStoppingCallback{cb}
	managerStoppingCallbacks[s] = true
	return s
}

func (cb *ManagerStoppingCallback) Unregister() {
	delete(managerStoppingCallbacks, cb)
}

func IPCClientRegisterUpdateFound(cb func(updateState UpdateState)) *UpdateFoundCallback {
	s := &UpdateFoundCallback{cb}
	updateFoundCallbacks[s] = true
	return s
}

func (cb *UpdateFoundCallback) Unregister() {
	delete(updateFoundCallbacks, cb)
}

func IPCClientRegisterUpdateProgress(cb func(dp updater.DownloadProgress)) *UpdateProgressCallback {
	s := &UpdateProgressCallback{cb}
	updateProgressCallbacks[s] = true
	return s
}

func (cb *UpdateProgressCallback) Unregister() {
	delete(updateProgressCallbacks, cb)
}

func IPCClientStartTunnel(config TunnelConfig) error {
	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	err := rpcEncoder.Encode(StartTunnelMethodType)
	if err != nil {
		return err
	}
	err = rpcEncoder.Encode(config)
	if err != nil {
		return err
	}
	err = rpcDecodeError()
	return err
}

func IPCClientStopTunnel() error {
	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	err := rpcEncoder.Encode(StopTunnelMethodType)
	if err != nil {
		return err
	}
	err = rpcDecodeError()
	return err
}

func IPCClientStopAllTunnels() error {
	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	err := rpcEncoder.Encode(StopAllTunnelsMethodType)
	if err != nil {
		return err
	}
	err = rpcDecodeError()
	return err
}

func IPCClientRegisterTunnelStateChange(cb func(state TunnelState)) *TunnelStateChangeCallback {
	s := &TunnelStateChangeCallback{cb}
	tunnelStateChangeCallbacks[s] = true
	return s
}

func (cb *TunnelStateChangeCallback) Unregister() {
	delete(tunnelStateChangeCallbacks, cb)
}
