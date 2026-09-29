//go:build windows

package ui

import (
	"fmt"
	"image/color"
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/icons"
	"github.com/fosrl/windows/tunnel"
)

// overlayTrayIcon lazily composes the gray icon with a colored badge.
type overlayTrayIcon struct {
	once sync.Once
	fill color.RGBA
	icon []byte
}

func (o *overlayTrayIcon) get() []byte {
	o.once.Do(func() {
		var err error
		o.icon, err = composeOverlayIcon(icons.Gray, o.fill)
		if err != nil {
			logger.Error("Failed to create transitional tray icon: %v", err)
		}
	})
	if o.icon == nil {
		return icons.Gray
	}
	return o.icon
}

var (
	registeringIcon   = &overlayTrayIcon{fill: badgeYellow}
	disconnectingIcon = &overlayTrayIcon{fill: badgeGray}
)

// trayIconForState returns the tray icon for a tunnel state. Transitional
// states get the gray icon with a badge in the bottom-right corner: gray while
// disconnecting, yellow otherwise.
func trayIconForState(state tunnel.State) []byte {
	switch state {
	case tunnel.StateRunning:
		return icons.Orange
	case tunnel.StateStopped:
		return icons.Gray
	case tunnel.StateStopping:
		return disconnectingIcon.get()
	}
	return registeringIcon.get()
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
