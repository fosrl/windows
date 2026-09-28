//go:build windows

package ui

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/tunnel"
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
	notifierReady.Store(true)
	return nil
}

func (notifierService) ServiceShutdown() error {
	if notifierReady.Load() {
		return notifier.ServiceShutdown()
	}
	return nil
}

// The message dialogs below block until dismissed. Call them off the main thread.

func attachOwner(d *application.MessageDialog, owner application.Window) *application.MessageDialog {
	if owner != nil {
		d.AttachToWindow(owner)
	}
	return d
}

func showInfo(owner application.Window, title, message string) {
	attachOwner(app.Dialog.Info(), owner).SetTitle(title).SetMessage(message).Show()
}

func showWarning(owner application.Window, title, message string) {
	attachOwner(app.Dialog.Warning(), owner).SetTitle(title).SetMessage(message).Show()
}

func showError(owner application.Window, title, message string) {
	attachOwner(app.Dialog.Error(), owner).SetTitle(title).SetMessage(message).Show()
}

// confirm shows a Yes/No question and reports whether Yes was chosen.
func confirm(owner application.Window, title, message string, defaultYes bool) bool {
	accepted := false
	d := attachOwner(app.Dialog.Question(), owner).SetTitle(title).SetMessage(message)
	yes := d.AddButton("Yes").OnClick(func() { accepted = true })
	no := d.AddButton("No")
	if defaultYes {
		d.SetDefaultButton(yes)
	} else {
		d.SetDefaultButton(no)
	}
	d.SetCancelButton(no)
	d.Show()
	return accepted
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
