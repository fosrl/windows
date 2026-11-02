package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/version"
)

const usage = `Usage: pangolin-client [--autostart]
       pangolin-client --daemon
       pangolin-client --tunnel <config.json>
`

// daemonUnavailableWarning is shown when the UI can't reach the daemon.
const daemonUnavailableWarning = "The Pangolin background service isn't running, so Pangolin can't connect " +
	"or remember your sign-in.\n\nStart it with:\n\nsudo systemctl enable --now pangolin-client"

func main() {
	setupLogging()
	logger.Info("Pangolin version %s starting", version.Number)

	args := os.Args[1:]
	switch {
	case len(args) == 1 && args[0] == "--daemon":
		// Run by the pangolin-client systemd service.
		if err := managers.Run(); err != nil {
			logger.Fatal("Daemon failed: %v", err)
		}
		return

	case len(args) == 2 && args[0] == "--tunnel":
		// Run by the daemon, one process per tunnel.
		configJSON, err := os.ReadFile(args[1])
		if err != nil {
			logger.Fatal("Failed to read tunnel config: %v", err)
		}
		if err := managers.RunTunnelService(string(configJSON)); err != nil {
			logger.Fatal("Tunnel failed: %v", err)
		}
		return

	case len(args) == 1 && args[0] == "--autostart":
		// Run from the XDG autostart entry at sign-in: only open the UI if
		// the user asked for it, as the Windows service does at logon.
		if !config.LaunchUIAtLoginEnabledIn(config.UserDataDir()) {
			logger.Info("Open at login is off; exiting")
			return
		}

	case len(args) > 0:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var warning string
	reader, writer, events, err := managers.AttachUI()
	switch {
	case errors.Is(err, managers.ErrUIAlreadyRunning):
		logger.Info("Pangolin is already running; asking it to show itself")
		managers.RequestUIAction(managers.UIActionShow)
		return
	case err != nil:
		logger.Error("Failed to connect to the Pangolin daemon: %v", err)
		warning = daemonUnavailableWarning
	default:
		managers.InitializeIPCClient(reader, writer, events)
		logger.Info("Connected to the Pangolin daemon")
	}

	if err := runUI(warning); err != nil {
		logger.Error("Failed to run UI: %v", err)
		os.Exit(1)
	}
}
