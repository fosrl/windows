//go:build windows

package tunnel

import (
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/olm/olm"
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

	olm *olm.Olm
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

	// Build and start the tunnel
	if err := s.buildTunnel(config); err != nil {
		logger.Error("Tunnel service: Failed to build tunnel: %v", err)
		SetState(StateStopped)
		notifyStateChange(StateStopped)
		return false, 1
	}

	// Handle service control requests
	for c := range r {
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
			s.destroyTunnel(config)

			SetState(StateStopped)
			notifyStateChange(StateStopped)
			return false, 0
		default:
			logger.Info("Tunnel service: Unexpected control request: %d", c.Cmd)
		}
	}

	// Channel closed, exit service
	// Perform cleanup before exiting (unexpected channel close)
	logger.Info("Tunnel service: Service control channel closed")
	s.destroyTunnel(config)
	return false, 0
}
