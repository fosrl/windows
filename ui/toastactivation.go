//go:build windows

package ui

import (
	"encoding/base64"
	"encoding/json"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Toast clicks are delivered through COM. The UI usually runs elevated, and
// COM won't hand a click from the (non-elevated) shell to an elevated
// process, so Windows starts a new pangolin.exe with -Embedding instead. That
// process receives the click here and forwards it to the running UI through
// the manager service.

var (
	ole32                     = windows.NewLazySystemDLL("ole32.dll")
	procCoRegisterClassObject = ole32.NewProc("CoRegisterClassObject")
	procCoRevokeClassObject   = ole32.NewProc("CoRevokeClassObject")
	procAllowSetForeground    = user32.NewProc("AllowSetForegroundWindow")
)

// IsToastActivation reports whether COM started this process to deliver a toast click.
func IsToastActivation(args []string) bool {
	for _, a := range args[1:] {
		if strings.EqualFold(a, "-Embedding") || strings.EqualFold(a, "/Embedding") {
			return true
		}
	}
	return false
}

// HandleToastActivation receives the toast click COM started this process for
// and forwards it to the running UI.
func HandleToastActivation() {
	args, ok := receiveToastActivation(10 * time.Second)
	if !ok {
		return
	}
	var payload struct {
		Action  string `json:"action"`
		Options struct {
			CategoryID string `json:"categoryId"`
		} `json:"payload"`
	}
	if data, err := base64.StdEncoding.DecodeString(args); err != nil || json.Unmarshal(data, &payload) != nil {
		logger.Error("Toast activation: could not decode arguments")
		return
	}
	logger.Info("Toast activation: action %q, category %q", payload.Action, payload.Options.CategoryID)

	if payload.Options.CategoryID == updateNotificationCategory &&
		(payload.Action == updateNotificationOpen || payload.Action == notifications.DefaultActionIdentifier) {
		// The shell lets the process it activated take the foreground; pass
		// that on so the update window comes to the front.
		const asfwAny = 0xFFFFFFFF
		procAllowSetForeground.Call(asfwAny)
		if managers.RequestUIAction(managers.UIActionOpenUpdate) {
			return
		}
	}
	// Other toasts only bring the app up.
	managers.RequestUILaunch()
}

// receiveToastActivation registers the app's toast activator and waits for
// COM to deliver the click, returning its arguments.
func receiveToastActivation(timeout time.Duration) (string, bool) {
	clsid, err := toastActivatorCLSID()
	if err != nil {
		logger.Error("Toast activation: %v", err)
		return "", false
	}

	received := make(chan string, 1)
	wintoast.SetActivationCallback(func(_ string, args string, _ []wintoast.UserData) {
		select {
		case received <- args:
		default:
		}
	})

	registered := make(chan bool, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		// The main thread is a single-threaded apartment with no message loop
		// yet, so serve the activator from the multithreaded apartment.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
			logger.Error("Toast activation: CoInitializeEx failed: %v", err)
			registered <- false
			return
		}
		defer windows.CoUninitialize()

		const clsctxLocalServer, regclsMultipleUse = 0x4, 1
		var cookie uint32
		hr, _, _ := procCoRegisterClassObject.Call(
			uintptr(unsafe.Pointer(&clsid)),
			uintptr(unsafe.Pointer(wintoast.ClassFactory)),
			clsctxLocalServer, regclsMultipleUse,
			uintptr(unsafe.Pointer(&cookie)),
		)
		if hr != 0 {
			logger.Error("Toast activation: CoRegisterClassObject failed: 0x%08x", uint32(hr))
			registered <- false
			return
		}
		registered <- true
		<-done
		procCoRevokeClassObject.Call(uintptr(cookie))
	}()

	if !<-registered {
		return "", false
	}
	select {
	case args := <-received:
		return args, true
	case <-time.After(timeout):
		logger.Error("Toast activation: timed out waiting for the click")
		return "", false
	}
}

// toastActivatorCLSID reads the activator CLSID the notification service registered for the app.
func toastActivatorCLSID() (windows.GUID, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+config.AppName, registry.QUERY_VALUE)
	if err != nil {
		return windows.GUID{}, err
	}
	defer k.Close()
	s, _, err := k.GetStringValue("CustomActivator")
	if err != nil {
		return windows.GUID{}, err
	}
	return windows.GUIDFromString(s)
}
