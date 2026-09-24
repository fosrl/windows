package ui

// Tray popup geometry in DIPs. The window is wide enough for the menu plus one
// cascading submenu and has a transparent margin for the drop shadow.
const (
	trayPanelWidth    = 260
	trayShadowPadding = 8
	trayWindowWidth   = 2*trayPanelWidth + 2*trayShadowPadding
)

// TrayLayout tells the popup which side submenus open on and which edge the
// menu is anchored to.
type TrayLayout struct {
	SubmenuSide string `json:"submenuSide"` // "left" or "right"
	Anchor      string `json:"anchor"`      // "top" or "bottom"
	PanelWidth  int    `json:"panelWidth"`
	Padding     int    `json:"padding"`
}

type trayRect struct {
	X, Y, Width, Height int
}

// placeTrayWindow positions the popup for a click at (cursorX, cursorY) the
// way TrackPopupMenu does: the menu's left edge at the cursor unless that runs
// off the work area, and opening upward when there is no room below. The
// cursor is usually on the taskbar, outside the work area, so the window is
// moved (never shrunk) to stay inside it; shrinking would clip the menu.
// height is the full window height the menu needs, including shadow padding.
func placeTrayWindow(cursorX, cursorY, height int, work trayRect) (trayRect, TrayLayout) {
	right := work.X + work.Width
	bottom := work.Y + work.Height
	panelOuter := trayPanelWidth + 2*trayShadowPadding

	if height > work.Height {
		height = work.Height
	}

	// Horizontal placement of the menu panel's outer box (including padding).
	panelX := cursorX - trayShadowPadding
	if panelX+panelOuter > right {
		panelX = cursorX - trayPanelWidth - trayShadowPadding
	}
	if panelX+panelOuter > right {
		panelX = right - panelOuter
	}
	if panelX < work.X {
		panelX = work.X
	}

	layout := TrayLayout{PanelWidth: trayPanelWidth, Padding: trayShadowPadding}
	windowX := panelX
	if panelX+panelOuter+trayPanelWidth <= right {
		layout.SubmenuSide = "right"
	} else {
		layout.SubmenuSide = "left"
		windowX = panelX - trayPanelWidth
	}

	// Open downward from the cursor when the whole menu fits below it;
	// otherwise open upward with the menu's bottom at the cursor, kept above
	// the bottom of the work area.
	windowY := cursorY - trayShadowPadding
	layout.Anchor = "top"
	if windowY+height > bottom {
		layout.Anchor = "bottom"
		windowBottom := cursorY + trayShadowPadding
		if windowBottom > bottom {
			windowBottom = bottom
		}
		windowY = windowBottom - height
	}
	if windowY < work.Y {
		windowY = work.Y
	}

	return trayRect{X: windowX, Y: windowY, Width: trayWindowWidth, Height: height}, layout
}
