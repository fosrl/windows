//go:build windows

package tunnel

import (
	"github.com/fosrl/newt/logger"
)

// destroyTunnel performs cleanup and tears down the tunnel
func (s *tunnelService) destroyTunnel(config Config) {
	logger.Debug("Destroy tunnel called")

	s.olm.StopApi()
	s.olm.StopTunnel()

	s.olm = nil

	logger.Debug("Destroy tunnel completed successfully")
}
