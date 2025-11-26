//go:build windows

package tunnel

import (
	"context"
	"time"

	"github.com/fosrl/newt/logger"

	olmpkg "github.com/fosrl/olm/olm"
	configpkg "github.com/fosrl/windows/config"
)

// buildTunnel builds the tunnel
// olmTerminateCancel is called when OLM terminates to signal the service to stop
func buildTunnel(config Config, olmTerminateCancel context.CancelFunc) error {
	logger.Debug("Build tunnel called: config: %+v", config)

	// Create context for OLM
	olmContext := context.Background()

	// Create OLM GlobalConfig with hardcoded values from Swift
	olmInitConfig := olmpkg.GlobalConfig{
		LogLevel:   configpkg.LogLevel,
		EnableAPI:  true,
		SocketPath: OLMNamedPipePath,
		Version:    "1",
		OnConnected: func() {
			logger.Info("Tunnel: OLM connected")
		},
		OnRegistered: func() {
			logger.Info("Tunnel: OLM registered")
		},
		OnTerminated: func() {
			logger.Info("Tunnel: OLM terminated")
			// Signal the service to stop itself
			if olmTerminateCancel != nil {
				olmTerminateCancel()
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

	olmpkg.StartApi()

	logger.Info("Starting OLM tunnel...")
	go func() {
		olmpkg.StartTunnel(olmConfig)
		logger.Info("OLM tunnel stopped")
	}()

	logger.Debug("Build tunnel completed successfully")
	return nil
}
