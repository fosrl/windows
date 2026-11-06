package ui

import (
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// The Pangolin GNOME Shell extension (packaging/linux/gnome-shell-extension)
// shows the panel button on GNOME, where the AppIndicator extension only
// opens the popup on a double click. The app serves this D-Bus interface for
// it; while the extension holds shellExtensionBusName, the
// StatusNotifierItem goes passive so there is one icon.

const (
	shellBusName          = "net.pangolin.Client"
	shellPath             = dbus.ObjectPath("/net/pangolin/Client")
	shellIface            = "net.pangolin.Client1"
	shellExtensionBusName = "net.pangolin.Client.ShellExtension"
)

var (
	shellMu      sync.Mutex
	shellProps   *prop.Properties
	shellExtOn   bool
	shellIcon    []byte
	shellTooltip string
)

type shellObject struct{}

// Activate toggles the popup for a click at (x, y) in the shell's stage
// coordinates, on a stage stageWidth x stageHeight in size.
func (shellObject) Activate(x, y, stageWidth, stageHeight int32) *dbus.Error {
	gx, gy := int(x), int(y)
	if rw, rh := rootWindowSize(); stageWidth > 0 && stageHeight > 0 && rw > 0 && rh > 0 {
		gx = int(int64(x) * int64(rw) / int64(stageWidth))
		gy = int(int64(y) * int64(rh) / int64(stageHeight))
	}
	logger.Debug("GNOME extension: activate at %d,%d on a %dx%d stage (GTK %d,%d)", x, y, stageWidth, stageHeight, gx, gy)
	setTrayClick(gx, gy, true)
	go toggleTrayPopup()
	return nil
}

// ClickAway closes the popup; the extension calls it for clicks that don't
// move focus, such as on the desktop background.
func (shellObject) ClickAway() *dbus.Error {
	logger.Debug("GNOME extension: click away")
	go hideTrayPopup()
	return nil
}

func (shellObject) Quit() *dbus.Error {
	go func() {
		hideTrayPopup()
		quit()
	}()
	return nil
}

func startShellBridge() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		logger.Error("GNOME extension bridge: no session bus: %v", err)
		return
	}
	if err := conn.Export(shellObject{}, shellPath, shellIface); err != nil {
		logger.Error("GNOME extension bridge: %v", err)
		return
	}
	shellMu.Lock()
	icon, tooltip := shellIcon, shellTooltip
	shellMu.Unlock()
	if icon == nil {
		icon = []byte{}
	}
	props, err := prop.Export(conn, shellPath, prop.Map{
		shellIface: {
			"Icon":         {Value: icon, Emit: prop.EmitTrue},
			"Tooltip":      {Value: tooltip, Emit: prop.EmitTrue},
			"PopupVisible": {Value: false, Emit: prop.EmitTrue},
		},
	})
	if err != nil {
		logger.Error("GNOME extension bridge: %v", err)
		return
	}
	node := &introspect.Node{
		Name: string(shellPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: shellIface, Methods: introspect.Methods(shellObject{}), Properties: props.Introspection(shellIface)},
		},
	}
	_ = conn.Export(introspect.NewIntrospectable(node), shellPath, "org.freedesktop.DBus.Introspectable")
	if reply, err := conn.RequestName(shellBusName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		logger.Error("GNOME extension bridge: could not own %s: %v", shellBusName, err)
	}
	shellMu.Lock()
	shellProps = props
	shellMu.Unlock()

	// Follow the extension coming and going.
	var owner string
	_ = conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, shellExtensionBusName).Store(&owner)
	setShellExtensionActive(owner != "")
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',sender='org.freedesktop.DBus',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='"+shellExtensionBusName+"'")
	go func() {
		for sig := range signals {
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(sig.Body) == 3 {
				newOwner, _ := sig.Body[2].(string)
				setShellExtensionActive(newOwner != "")
			}
		}
	}()
}

func setShellExtensionActive(active bool) {
	shellMu.Lock()
	changed := shellExtOn != active
	shellExtOn = active
	shellMu.Unlock()
	if changed {
		logger.Info("Pangolin GNOME extension active: %v", active)
	}
	trayGeomMu.Lock()
	item := trayItem
	trayGeomMu.Unlock()
	if item != nil {
		item.SetPassive(active)
	}
}

func shellExtensionActive() bool {
	shellMu.Lock()
	defer shellMu.Unlock()
	return shellExtOn
}

// setShellIcon publishes the tray icon (as a PNG) and tooltip to the extension.
func setShellIcon(ico []byte, tooltip string) {
	png, err := icoToPNG(ico, 64)
	if err != nil {
		return
	}
	shellMu.Lock()
	shellIcon, shellTooltip = png, tooltip
	props := shellProps
	shellMu.Unlock()
	if props != nil {
		props.SetMust(shellIface, "Icon", png)
		props.SetMust(shellIface, "Tooltip", tooltip)
	}
}

func setShellPopupVisible(visible bool) {
	shellMu.Lock()
	props := shellProps
	shellMu.Unlock()
	if props != nil {
		props.SetMust(shellIface, "PopupVisible", visible)
	}
}
