//go:build windows

package tunnel

import (
	"context"
	"time"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows/svc"
)

// RunTunnelService runs the tunnel as a Windows service
// Tunnels are always started via the UI and run as Windows services
func RunTunnelService(configJSON string) error {
	logger.Info("Tunnel service: Starting with config")

	// Tunnels always run as Windows services (started via UI)
	logger.Info("Tunnel service: Running as Windows service")
	return svc.Run("", &tunnelService{configJSON: configJSON})
}

type tunnelService struct {
	configJSON string
}

func (s *tunnelService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	changes <- svc.Status{State: svc.StartPending}
	logger.Info("Tunnel service: Service starting")

	// Parse and log config
	config, err := ConfigFromJSON(s.configJSON)
	if err != nil {
		logger.Error("Tunnel service: Failed to parse config: %v", err)
		return false, 1
	}

	// Set state to registering when service starts (before OLM initialization)
	SetState(StateRegistering)
	notifyStateChange(StateRegistering)
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	// Create context for OLM termination monitoring
	olmCtx, olmCancel := context.WithCancel(context.Background())
	defer olmCancel()

	// Build and start the tunnel
	if err := buildTunnel(config, olmCancel); err != nil {
		logger.Error("Tunnel service: Failed to build tunnel: %v", err)
		SetState(StateStopped)
		notifyStateChange(StateStopped)
		return false, 1
	}

	// Handle service control requests and OLM termination
	for {
		select {
		case <-olmCtx.Done():
			// OLM terminated, stop the service
			logger.Info("Tunnel service: OLM terminated, stopping service")
			SetState(StateStopping)
			notifyStateChange(StateStopping)
			changes <- svc.Status{State: svc.StopPending}

			// Destroy the tunnel (cleanup)
			destroyTunnel(config)

			SetState(StateStopped)
			notifyStateChange(StateStopped)
			return false, 0
		case c, ok := <-r:
			if !ok {
				// Channel closed, exit service
				// Perform cleanup before exiting (unexpected channel close)
				logger.Info("Tunnel service: Service control channel closed")
				destroyTunnel(config)
				return false, 0
			}
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
				time.Sleep(100 * time.Millisecond)
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logger.Info("Tunnel service: Service stopping")
				SetState(StateStopping)
				notifyStateChange(StateStopping)
				changes <- svc.Status{State: svc.StopPending}

				// Destroy the tunnel (cleanup)
				destroyTunnel(config)

				SetState(StateStopped)
				notifyStateChange(StateStopped)
				return false, 0
			default:
				logger.Info("Tunnel service: Unexpected control request: %d", c.Cmd)
			}
		}
	}
}
