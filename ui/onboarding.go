//go:build windows

package ui

import (
	"os"
	"sync"

	"github.com/fosrl/newt/logger"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Setup (onboarding), as in the macOS app's MacOnboardingFlowView: welcome,
// privacy, and a closing page when there is no account yet. The macOS system
// extension and VPN configuration steps have no Windows counterpart.

// OnboardingState is what the setup window needs to pick and render its pages.
type OnboardingState struct {
	SeenWelcome         bool `json:"seenWelcome"`
	AcknowledgedPrivacy bool `json:"acknowledgedPrivacy"`
	HasAccounts         bool `json:"hasAccounts"`
	// Opened is bumped each time the window is shown, so it starts over at the
	// first unfinished page.
	Opened int `json:"opened"`
}

var (
	onboardingWindow *application.WebviewWindow
	onboardingMu     sync.Mutex
	onboardingOpened int
	// onboardingAutoOpened keeps setup from opening itself more than once per launch.
	onboardingAutoOpened bool
)

// onboardingNeeded reports whether setup still has unfinished steps. Until it
// is done, the tray offers "Open Pangolin Setup…" instead of connecting.
func onboardingNeeded() bool {
	if configManager == nil {
		return false
	}
	return !configManager.GetOnboardingSeenWelcome() || !configManager.GetOnboardingAcknowledgedPrivacy()
}

func currentOnboardingState() OnboardingState {
	onboardingMu.Lock()
	opened := onboardingOpened
	onboardingMu.Unlock()
	return onboardingState(opened)
}

func onboardingState(opened int) OnboardingState {
	s := OnboardingState{Opened: opened}
	if configManager != nil {
		s.SeenWelcome = configManager.GetOnboardingSeenWelcome()
		s.AcknowledgedPrivacy = configManager.GetOnboardingAcknowledgedPrivacy()
	}
	if accountManager != nil {
		s.HasAccounts = len(accountManager.Accounts) > 0
	}
	return s
}

// skipOnboardingForUsedInstall marks setup finished where Pangolin was used
// before setup existed: there are accounts, a user config file, or the UI's
// WebView2 data from an earlier launch. It only applies when setup has never
// been shown, so someone who closed it partway still finishes it. Call it
// before any window is created, which creates the WebView2 data.
func skipOnboardingForUsedInstall() {
	if configManager == nil || !configManager.OnboardingNeverStarted() {
		return
	}
	usedBefore := (accountManager != nil && len(accountManager.Accounts) > 0) || configManager.UserConfigExists()
	if dir := webviewUserDataPath(); dir != "" {
		if _, err := os.Stat(dir); err == nil {
			usedBefore = true
		}
	}
	if !usedBefore {
		return
	}
	logger.Info("Pangolin was used on this computer before; skipping setup")
	configManager.SetOnboardingSeenWelcome(true)
	configManager.SetOnboardingAcknowledgedPrivacy(true)
}

// autoOpenOnboarding opens setup at launch when it isn't finished.
func autoOpenOnboarding() {
	if !onboardingNeeded() {
		return
	}
	if configManager != nil {
		configManager.MarkOnboardingStarted()
	}
	onboardingMu.Lock()
	already := onboardingAutoOpened
	onboardingAutoOpened = true
	onboardingMu.Unlock()
	if !already {
		showOnboardingWindow()
	}
}

// onboardingBackground is the setup window color from the macOS app.
func onboardingBackground() application.RGBA {
	if w32.IsCurrentlyDarkMode() {
		return application.NewRGB(0x16, 0x16, 0x18)
	}
	return application.NewRGB(0xfd, 0xfd, 0xfd)
}

// showOnboardingWindow opens the setup window, or brings it to the front.
func showOnboardingWindow() {
	onboardingMu.Lock()
	defer onboardingMu.Unlock()

	onboardingAutoOpened = true
	if onboardingWindow == nil {
		onboardingWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:                "onboarding",
			Title:               "Pangolin Setup",
			URL:                 "/#/onboarding",
			Width:               560,
			Height:              520,
			DisableResize:       true,
			Hidden:              true,
			MinimiseButtonState: application.ButtonHidden,
			MaximiseButtonState: application.ButtonHidden,
			BackgroundColour:    onboardingBackground(),
		})
		// Closing only hides the window; finished steps are kept and setup
		// resumes at the first unfinished one.
		onboardingWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			e.Cancel()
			go closeOnboardingWindow()
		})
	}

	if !onboardingWindow.IsVisible() {
		onboardingOpened++
		onboardingWindow.Center()
	}
	onboardingWindow.EmitEvent("onboarding:opened", onboardingState(onboardingOpened))
	onboardingWindow.Show()
	if onboardingWindow.IsMinimised() {
		onboardingWindow.Restore()
	}
	onboardingWindow.Focus()
}

func closeOnboardingWindow() {
	onboardingMu.Lock()
	w := onboardingWindow
	onboardingMu.Unlock()
	if w != nil {
		w.Hide()
	}
}

func markOnboardingWelcomeSeen() {
	if configManager != nil {
		configManager.SetOnboardingSeenWelcome(true)
	}
	publish()
}

func markOnboardingPrivacyAcknowledged() {
	if configManager != nil {
		configManager.SetOnboardingAcknowledgedPrivacy(true)
	}
	publish()
}
