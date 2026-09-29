//go:build windows

package ui

import (
	"fmt"
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/icons"
	"github.com/fosrl/windows/tunnel"
)

var (
	overlayIconOnce sync.Once
	overlayIcon     []byte
)

// trayIconForState returns the tray icon for a tunnel state. Transitional
// states get the gray icon with a yellow badge in the bottom-right corner.
func trayIconForState(state tunnel.State) []byte {
	switch state {
	case tunnel.StateRunning:
		return icons.Orange
	case tunnel.StateStopped:
		return icons.Gray
	}
	overlayIconOnce.Do(func() {
		var err error
		overlayIcon, err = composeOverlayIcon(icons.Gray)
		if err != nil {
			logger.Error("Failed to create transitional tray icon: %v", err)
		}
	})
	if overlayIcon == nil {
		return icons.Gray
	}
	return overlayIcon
}

func trayTooltipForState(state tunnel.State) string {
	return fmt.Sprintf("%s: %s", config.AppName, state.DisplayText())
}

func updateTrayForState(state tunnel.State) {
	if systemTray == nil {
		return
	}
	systemTray.SetIcon(trayIconForState(state))
	systemTray.SetTooltip(trayTooltipForState(state))
}
