//go:build windows

package tunnel

import (
	"github.com/fosrl/newt/logger"

	olmpkg "github.com/fosrl/olm/olm"
)

// destroyTunnel performs cleanup and tears down the tunnel
func destroyTunnel(config Config) {
	logger.Debug("Destroy tunnel called")

	olmpkg.StopApi()
	olmpkg.StopTunnel()

	logger.Debug("Destroy tunnel completed successfully")
}
