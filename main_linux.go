package main

import (
	"os"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/version"
)

// main runs the tray UI on its own. The root daemon that owns tunnels and
// secrets, and that the UI connects to, comes in the next step; until then
// signing in works but nothing is saved and tunnels can't start.
func main() {
	setupLogging()
	logger.Info("Pangolin version %s starting", version.Number)

	if err := runUI(); err != nil {
		logger.Error("Failed to run UI: %v", err)
		os.Exit(1)
	}
}
