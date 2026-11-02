package managers

// UIAction is something another process asks the running UI to do.
type UIAction uint32

const (
	// UIActionOpenUpdate opens the update window, e.g. from the update toast's Open button.
	UIActionOpenUpdate UIAction = iota + 1
	// UIActionShow opens a window when the app is launched again while the UI
	// is already running, so it is reachable even if the tray icon is missing.
	UIActionShow
)
