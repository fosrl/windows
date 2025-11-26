//go:build windows

package tunnel

import (
	"context"
	"time"

	"github.com/fosrl/newt/logger"

	olmpkg "github.com/fosrl/olm/olm"
)

// buildTunnel builds the tunnel
func buildTunnel(config Config) error {
	logger.Debug("Build tunnel called: config: %+v", config)

	// Create context for OLM
	olmContext := context.Background()

	// Create OLM GlobalConfig with hardcoded values from Swift
	olmInitConfig := olmpkg.GlobalConfig{
		LogLevel:   "debug",
		EnableAPI:  true,
		SocketPath: "/var/run/olm.sock",
		Version:    "1",
		OnConnected: func() {
			logger.Info("OLM connected")
			// Update state to running when OLM connects
			SetState(StateRunning)
			notifyStateChange(StateRunning)
		},
		OnRegistered: func() {
			logger.Info("OLM registered")
			// Update state to registered when OLM registers
			SetState(StateRegistered)
			notifyStateChange(StateRegistered)
		},
		OnTerminated: func() {
			logger.Info("OLM terminated")
			// Force tunnel to disconnected state
			SetState(StateStopped)
			notifyStateChange(StateStopped)
			// This will uninstall the Windows service
			if err := StopTunnel(); err != nil {
				logger.Error("Failed to stop tunnel after OLM termination: %v", err)
			}
		},
	}

	// Initialize OLM with context and GlobalConfig
	olmpkg.Init(olmContext, olmInitConfig)

	olmConfig := olmpkg.TunnelConfig{
		Endpoint:             config.Endpoint,
		ID:                   config.ID,
		Secret:               config.Secret,
		MTU:                  config.MTU,
		DNS:                  config.DNS,
		Holepunch:            config.Holepunch,
		PingIntervalDuration: time.Duration(config.PingIntervalSeconds) * time.Second,
		PingTimeoutDuration:  time.Duration(config.PingTimeoutSeconds) * time.Second,
		UserToken:            config.UserToken,
		OrgID:                config.OrgID,
		InterfaceName:        config.InterfaceName,
		UpstreamDNS:          config.UpstreamDNS,
	}

	logger.Info("Starting OLM tunnel...")
	go func() {
		olmpkg.StartTunnel(olmConfig)
		logger.Info("OLM tunnel stopped")
	}()

	logger.Debug("Build tunnel completed successfully")
	return nil
}
