//go:build windows

package ui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/icons"
	"github.com/fosrl/windows/tunnel"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
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
//
// They call MessageBox directly rather than going through Wails so the app icon
// can be put in the title bar: MessageBox windows have none by default, so a
// CBT hook catches the box as it activates and sets one.

const hcbtActivate = 5

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")

	// Only touched on the main thread, where every message box is shown.
	dialogHook     uintptr
	dialogHookProc = windows.NewCallback(dialogHookCallback)

	dialogIconsOnce sync.Once
	dialogIconSmall w32.HICON
	dialogIconBig   w32.HICON
)

func dialogHookCallback(code, wParam, lParam uintptr) uintptr {
	hook := dialogHook
	if int32(code) == hcbtActivate && w32.GetClassName(w32.HWND(wParam)) == "#32770" {
		setDialogIcon(w32.HWND(wParam))
		procUnhookWindowsHookEx.Call(hook)
		dialogHook = 0
	}
	ret, _, _ := procCallNextHookEx.Call(hook, code, wParam, lParam)
	return ret
}

func setDialogIcon(hwnd w32.HWND) {
	dialogIconsOnce.Do(func() {
		var err error
		if dialogIconSmall, err = w32.CreateSmallHIconFromImage(icons.Orange); err != nil {
			logger.Error("Failed to create dialog icon: %v", err)
		}
		if dialogIconBig, err = w32.CreateLargeHIconFromImage(icons.Orange); err != nil {
			logger.Error("Failed to create dialog icon: %v", err)
		}
	})
	if dialogIconSmall == 0 {
		return
	}
	// Windows hides the caption icon of windows with a modal dialog frame.
	exStyle := w32.GetWindowLongPtr(hwnd, w32.GWL_EXSTYLE)
	w32.SetWindowLongPtr(hwnd, w32.GWL_EXSTYLE, exStyle&^w32.WS_EX_DLGMODALFRAME)
	w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_SMALL, uintptr(dialogIconSmall))
	if dialogIconBig != 0 {
		w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_BIG, uintptr(dialogIconBig))
	}
	w32.SetWindowPos(hwnd, 0, 0, 0, 0, 0,
		w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOZORDER|w32.SWP_NOACTIVATE|w32.SWP_FRAMECHANGED)
}

// messageBox shows a message box on the main thread and returns the ID of the
// button that closed it.
func messageBox(owner application.Window, title, message string, flags uint32) int32 {
	var result int32
	application.InvokeSync(func() {
		var hwnd windows.HWND
		if owner != nil {
			if native := owner.NativeWindow(); native != nil {
				hwnd = windows.HWND(uintptr(native))
			}
		}
		dialogHook, _, _ = procSetWindowsHookEx.Call(w32.WH_CBT, dialogHookProc, 0, uintptr(windows.GetCurrentThreadId()))
		var err error
		result, err = windows.MessageBox(hwnd, windows.StringToUTF16Ptr(message), windows.StringToUTF16Ptr(title),
			flags|windows.MB_SYSTEMMODAL|windows.MB_SETFOREGROUND)
		if err != nil {
			logger.Error("Failed to show dialog %q: %v", title, err)
		}
		if dialogHook != 0 {
			procUnhookWindowsHookEx.Call(dialogHook)
			dialogHook = 0
		}
	})
	return result
}

func showInfo(owner application.Window, title, message string) {
	messageBox(owner, title, message, windows.MB_OK|windows.MB_ICONINFORMATION)
}

func showWarning(owner application.Window, title, message string) {
	messageBox(owner, title, message, windows.MB_OK|windows.MB_ICONWARNING)
}

func showError(owner application.Window, title, message string) {
	messageBox(owner, title, message, windows.MB_OK|windows.MB_ICONERROR)
}

// confirm shows a Yes/No question and reports whether Yes was chosen.
func confirm(owner application.Window, title, message string, defaultYes bool) bool {
	flags := uint32(windows.MB_YESNO)
	if !defaultYes {
		flags |= windows.MB_DEFBUTTON2
	}
	return messageBox(owner, title, message, flags) == w32.IDYES
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
