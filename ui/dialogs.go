package ui

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/tunnel"
	"github.com/fosrl/windows/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

var (
	notifier      *notifications.NotificationService
	notifierReady atomic.Bool
	notificationN atomic.Uint64
)

// notifierService starts the toast notification service without letting a
// failure (for example a registry error) stop the UI from starting.
type notifierService struct{}

func (notifierService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := notifier.ServiceStartup(ctx, options); err != nil {
		logger.Error("Notifications are unavailable: %v", err)
		return nil
	}
	if err := notifier.RegisterNotificationCategory(notifications.NotificationCategory{
		ID:      updateNotificationCategory,
		Actions: []notifications.NotificationAction{{ID: updateNotificationOpen, Title: "Open"}},
	}); err != nil {
		logger.Error("Failed to register update notification actions: %v", err)
	}
	notifier.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			logger.Error("Notification response error: %v", result.Error)
			return
		}
		r := result.Response
		if r.CategoryID == updateNotificationCategory &&
			(r.ActionIdentifier == updateNotificationOpen || r.ActionIdentifier == notifications.DefaultActionIdentifier) {
			go showUpdateWindow()
		}
	})
	notifierReady.Store(true)
	return nil
}

func (notifierService) ServiceShutdown() error {
	if notifierReady.Load() {
		return notifier.ServiceShutdown()
	}
	return nil
}

// notify shows a toast, replacing the old tray balloon notifications.
func notify(title, message string) {
	if !notifierReady.Load() {
		logger.Info("%s: %s", title, message)
		return
	}
	id := fmt.Sprintf("pangolin-%d", notificationN.Add(1))
	if err := notifier.SendNotification(notifications.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  message,
	}); err != nil {
		logger.Error("Failed to show notification %q: %v", title, err)
	}
}

const (
	updateNotificationCategory = "pangolin-update"
	updateNotificationOpen     = "open"
)

// notifyUpdateAvailable shows a toast with an Open button that opens the update
// window. Without toasts, the window opens directly.
func notifyUpdateAvailable(version string) {
	if !notifierReady.Load() {
		showUpdateWindow()
		return
	}
	body := "A new version of Pangolin is ready to install."
	if version != "" {
		body = fmt.Sprintf("Pangolin %s is ready to install.", version)
	}
	if manual, _ := updater.ManualUpdate(); manual != "" {
		body = "A new version of Pangolin is available."
		if version != "" {
			body = fmt.Sprintf("Pangolin %s is available.", version)
		}
	}
	if err := notifier.SendNotificationWithActions(notifications.NotificationOptions{
		ID:         "pangolin-update",
		Title:      "Update Available",
		Body:       body,
		CategoryID: updateNotificationCategory,
	}); err != nil {
		logger.Error("Failed to show update notification: %v", err)
		showUpdateWindow()
	}
}

func showConnectionErrorNotification(title, message string) {
	logger.Error("%s: %s", title, message)
	notify(title, message)
}

func notifyConnectionError(err error, fallbackTitle string) {
	title := fallbackTitle
	message := err.Error()
	if connErr, ok := err.(*tunnel.ConnectionError); ok {
		title = connErr.Title
		message = connErr.Message
	}
	setConnectionError(message)
	showConnectionErrorNotification(title, message)
}
