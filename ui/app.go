package ui

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"runtime/debug"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/icons"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/secrets"
	"github.com/fosrl/windows/tunnel"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:frontend/dist
var frontendDist embed.FS

// ErrWebView2Missing is returned by Run when the WebView2 Runtime is not installed.
var ErrWebView2Missing = errors.New("the Microsoft Edge WebView2 Runtime is not installed")

var app *application.App

// Deps are the managers the UI works with.
type Deps struct {
	Auth     *auth.AuthManager
	Config   *config.ConfigManager
	Accounts *config.AccountManager
	API      *api.APIClient
	Secrets  *secrets.SecretManager
}

// Run starts the tray UI and blocks until the UI quits.
func Run(d Deps) error {
	if err := checkWebView(); err != nil {
		return err
	}

	authManager = d.Auth
	configManager = d.Config
	accountManager = d.Accounts
	apiClient = d.API
	tunnelManager = tunnel.NewManager(d.Auth, d.Config, d.Accounts, d.Secrets, managers.NewIPCAdapter())
	tunnelManager.SetGatewayResolver(resolveSavedExitNode)
	tunnelManager.SetServerSettingsFetcher(fetchServerSettings)
	skipOnboardingForUsedInstall()

	assets, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		return fmt.Errorf("frontend assets: %w", err)
	}

	beforeAppStart()

	notifier = notifications.New()
	opts := application.Options{
		Name:        config.AppName,
		Description: "Pangolin client",
		Icon:        icons.Orange,
		Services: []application.Service{
			application.NewService(&MenuService{}),
			application.NewService(&PreferencesService{}),
			application.NewService(&StatusService{}),
			application.NewService(&LogsService{}),
			application.NewService(&LoginService{}),
			application.NewService(&AccountsService{}),
			application.NewService(&OnboardingService{}),
			application.NewService(&AppService{}),
			application.NewService(&UpdateService{}),
			application.NewService(&CLIService{}),
			application.NewService(&notifierService{}),
		},
		Assets: application.AssetOptions{
			Handler:        application.BundledAssetFileServer(assets),
			DisableLogging: true,
		},
		Logger:   slog.New(newtLogHandler{}),
		LogLevel: slog.LevelWarn,
		ErrorHandler: func(err error) {
			logger.Error("wails: %v", err)
		},
		PanicHandler: func(p *application.PanicDetails) {
			logger.Error("UI panic: %v\n%s", p.Error, p.StackTrace)
		},
	}
	setPlatformOptions(&opts)
	app = application.New(opts)

	setupSystemTray()
	registerIPCCallbacks()

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go onStarted()
	})

	return app.Run()
}

func onStarted() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic during UI startup: %v\n%s", r, debug.Stack())
		}
	}()

	updateTrayForState(currentTunnelState())
	refreshCLIInstallState()
	go checkStartupUpdate()
	go watchAuthState()

	if onboardingNeeded() {
		// Nothing connects until setup is finished.
		go autoOpenOnboarding()
	} else if managers.IPCClientAlwaysOn() {
		go resumeAlwaysOn(authManager)
	} else if configManager != nil && configManager.GetAutoConnectAtLogin() {
		go autoConnect(authManager)
	}
	publish()
}

// newtLogHandler forwards Wails' slog output to the shared Pangolin log.
type newtLogHandler struct {
	attrs []slog.Attr
}

func (h newtLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h newtLogHandler) Handle(_ context.Context, r slog.Record) error {
	msg := r.Message
	appendAttr := func(a slog.Attr) bool {
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	}
	for _, a := range h.attrs {
		appendAttr(a)
	}
	r.Attrs(appendAttr)
	if r.Level >= slog.LevelError {
		logger.Error("wails: %s", msg)
	} else {
		logger.Warn("wails: %s", msg)
	}
	return nil
}

func (h newtLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return newtLogHandler{attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h newtLogHandler) WithGroup(string) slog.Handler { return h }
