//go:build windows

package ui

import (
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Phases of the CLI install window.
const (
	cliPhaseConfirm    = "confirm"
	cliPhaseInstalling = "installing"
	cliPhaseDone       = "done"
	cliPhaseError      = "error"
)

// CLIInfo is what the CLI install window shows.
type CLIInfo struct {
	Phase string `json:"phase"`
	Error string `json:"error"`
}

var (
	cliMu     sync.Mutex
	cliWindow *application.WebviewWindow
	cliInfo   = CLIInfo{Phase: cliPhaseConfirm}
)

// CLIService backs the CLI install window.
type CLIService struct{}

func (CLIService) State() CLIInfo {
	cliMu.Lock()
	defer cliMu.Unlock()
	return cliInfo
}

func (CLIService) Install() { installCLI() }
func (CLIService) Close()   { hideCLIWindow() }

// showCLIWindow opens the CLI install window from the menu, or focuses it if
// it is already open. A finished install starts over at the confirmation.
func showCLIWindow() {
	cliMu.Lock()
	if cliInfo.Phase != cliPhaseInstalling {
		cliInfo = CLIInfo{Phase: cliPhaseConfirm}
	}
	if cliWindow == nil {
		// Closing only hides the window, so a running install can be shown again.
		cliWindow = newDialogWindow("cli", "Install Pangolin CLI", "cli", 200, hideCLIWindow)
	}
	cliWindow.EmitEvent(eventCLIState, cliInfo)
	cliWindow.Show()
	cliWindow.Focus()
	cliMu.Unlock()
}

func hideCLIWindow() {
	cliMu.Lock()
	defer cliMu.Unlock()
	if cliWindow != nil {
		cliWindow.Hide()
	}
}

// setCLIInfo changes the CLI install state and sends it to the window.
func setCLIInfo(info CLIInfo) {
	cliMu.Lock()
	cliInfo = info
	w := cliWindow
	cliMu.Unlock()
	if w != nil {
		w.EmitEvent(eventCLIState, info)
	}
}

// installCLI downloads and runs the CLI installer through the manager,
// showing its progress and result in the window.
func installCLI() {
	cliMu.Lock()
	if cliInfo.Phase == cliPhaseInstalling {
		cliMu.Unlock()
		return
	}
	cliMu.Unlock()

	logger.Info("Starting Pangolin CLI installer via manager...")
	setCLIInstallInProgress(true)
	setCLIInfo(CLIInfo{Phase: cliPhaseInstalling})

	err := managers.IPCClientInstallCLI()
	setCLIInstallInProgress(false)
	if err != nil {
		logger.Error("Failed to install Pangolin CLI: %v", err)
		setCLIInfo(CLIInfo{Phase: cliPhaseError, Error: err.Error()})
	} else {
		setCLIInfo(CLIInfo{Phase: cliPhaseDone})
		refreshCLIInstallState()
	}
	// Bring the result forward if the window was hidden during the install.
	cliMu.Lock()
	if cliWindow != nil {
		cliWindow.Show()
		cliWindow.Focus()
	}
	cliMu.Unlock()
}
