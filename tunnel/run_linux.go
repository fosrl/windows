package tunnel

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/fosrl/newt/logger"
)

// RunTunnelService runs one tunnel in this process until SIGTERM or SIGINT.
// On Linux the daemon starts each tunnel as a child process.
func RunTunnelService(configJSON string) error {
	logger.Info("Tunnel service: Starting with config")

	config, err := ConfigFromJSON(configJSON)
	if err != nil {
		logger.Error("Tunnel service: Failed to parse config: %v", err)
		return err
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stop)

	SetState(StateRegistering)
	notifyStateChange(StateRegistering)

	s := &tunnelService{configJSON: configJSON}
	if err := s.buildTunnel(config); err != nil {
		logger.Error("Tunnel service: Failed to build tunnel: %v", err)
		SetState(StateStopped)
		notifyStateChange(StateStopped)
		return err
	}

	sig := <-stop
	logger.Info("Tunnel service: Stopping on %v", sig)
	SetState(StateStopping)
	notifyStateChange(StateStopping)
	s.destroyTunnel(config)
	SetState(StateStopped)
	notifyStateChange(StateStopped)
	return nil
}
