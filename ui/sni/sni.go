//go:build linux

// Package sni is a StatusNotifierItem tray icon, the protocol KDE, XFCE and
// GNOME's AppIndicator extension use for tray icons, with a dbusmenu context
// menu. Unlike Wails' Linux tray it reports where the icon was clicked, which
// the popup needs to open beside it.
package sni

import (
	"fmt"
	"image"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	itemPath      = dbus.ObjectPath("/StatusNotifierItem")
	menuPath      = dbus.ObjectPath("/MenuBar")
	itemIface     = "org.kde.StatusNotifierItem"
	watcherName   = "org.kde.StatusNotifierWatcher"
	watcherPath   = dbus.ObjectPath("/StatusNotifierWatcher")
	watcherIface  = "org.kde.StatusNotifierWatcher"
	menuIface     = "com.canonical.dbusmenu"
	propsIface    = "org.freedesktop.DBus.Properties"
	dbusName      = "org.freedesktop.DBus"
	busPath       = dbus.ObjectPath("/org/freedesktop/DBus")
	nameOwnerRule = "type='signal',sender='org.freedesktop.DBus',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='" + watcherName + "'"
)

// MenuItem is an entry in the icon's context menu.
type MenuItem struct {
	Label   string
	OnClick func()
}

// Options configure a tray icon.
type Options struct {
	ID    string // stable application id, e.g. "pangolin-client"
	Title string
	// Activate is called on a primary click with the click position in X11
	// screen coordinates, as the tray host reports it (0, 0 if unknown).
	Activate func(x, y int)
	Menu     []MenuItem
	// HostChanged is called whenever a tray host appears or goes away.
	HostChanged func(available bool)
}

// Item is a registered tray icon.
type Item struct {
	conn  *dbus.Conn
	name  string
	opts  Options
	props *prop.Properties

	mu      sync.Mutex
	closed  bool
	signals chan *dbus.Signal
}

type pixmap struct {
	W, H int32
	Data []byte
}

type tooltip struct {
	IconName   string
	IconPixmap []pixmap
	Title      string
	Body       string
}

// New registers a tray icon on the session bus. It works without a tray host
// and appears once one starts.
func New(opts Options) (*Item, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	it := &Item{
		conn: conn,
		name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()),
		opts: opts,
	}
	if err := it.export(); err != nil {
		conn.Close()
		return nil, err
	}
	reply, err := conn.RequestName(it.name, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("could not own %s: %v", it.name, err)
	}

	it.signals = make(chan *dbus.Signal, 8)
	conn.Signal(it.signals)
	conn.BusObject().Call(dbusName+".AddMatch", 0, nameOwnerRule)
	conn.BusObject().Call(dbusName+".AddMatch", 0,
		"type='signal',interface='"+watcherIface+"',member='StatusNotifierHostRegistered'")
	conn.BusObject().Call(dbusName+".AddMatch", 0,
		"type='signal',interface='"+watcherIface+"',member='StatusNotifierHostUnregistered'")
	go it.watch()

	it.register()
	return it, nil
}

func (it *Item) export() error {
	itemMethods := &itemObject{it: it}
	if err := it.conn.Export(itemMethods, itemPath, itemIface); err != nil {
		return err
	}
	props, err := prop.Export(it.conn, itemPath, prop.Map{
		itemIface: {
			"Category":            {Value: "ApplicationStatus", Emit: prop.EmitFalse},
			"Id":                  {Value: it.opts.ID, Emit: prop.EmitFalse},
			"Title":               {Value: it.opts.Title, Emit: prop.EmitFalse},
			"Status":              {Value: "Active", Emit: prop.EmitFalse},
			"WindowId":            {Value: int32(0), Emit: prop.EmitFalse},
			"IconName":            {Value: "", Emit: prop.EmitFalse},
			"IconPixmap":          {Value: []pixmap{}, Emit: prop.EmitFalse},
			"OverlayIconName":     {Value: "", Emit: prop.EmitFalse},
			"OverlayIconPixmap":   {Value: []pixmap{}, Emit: prop.EmitFalse},
			"AttentionIconName":   {Value: "", Emit: prop.EmitFalse},
			"AttentionIconPixmap": {Value: []pixmap{}, Emit: prop.EmitFalse},
			"AttentionMovieName":  {Value: "", Emit: prop.EmitFalse},
			"ToolTip":             {Value: tooltip{Title: it.opts.Title, IconPixmap: []pixmap{}}, Emit: prop.EmitFalse},
			"ItemIsMenu":          {Value: false, Emit: prop.EmitFalse},
			"Menu":                {Value: menuPath, Emit: prop.EmitFalse},
		},
	})
	if err != nil {
		return err
	}
	it.props = props
	node := &introspect.Node{
		Name: string(itemPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       itemIface,
				Methods:    introspect.Methods(itemMethods),
				Properties: props.Introspection(itemIface),
				Signals: []introspect.Signal{
					{Name: "NewTitle"}, {Name: "NewIcon"}, {Name: "NewAttentionIcon"},
					{Name: "NewOverlayIcon"}, {Name: "NewToolTip"},
					{Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}},
				},
			},
		},
	}
	if err := it.conn.Export(introspect.NewIntrospectable(node), itemPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	return exportMenu(it.conn, it.opts.Menu)
}

// register tells the watcher about the icon, if a watcher is running.
func (it *Item) register() {
	watcher := it.conn.Object(watcherName, watcherPath)
	if call := watcher.Call(watcherIface+".RegisterStatusNotifierItem", 0, it.name); call.Err != nil {
		it.notifyHost(false)
		return
	}
	it.notifyHost(it.HostAvailable())
}

// HostAvailable reports whether a tray is showing StatusNotifierItems.
func (it *Item) HostAvailable() bool {
	v, err := it.conn.Object(watcherName, watcherPath).GetProperty(watcherIface + ".IsStatusNotifierHostRegistered")
	if err != nil {
		return false
	}
	b, _ := v.Value().(bool)
	return b
}

func (it *Item) notifyHost(available bool) {
	if it.opts.HostChanged != nil {
		go it.opts.HostChanged(available)
	}
}

// watch re-registers when the watcher restarts, as when a panel restarts.
func (it *Item) watch() {
	for sig := range it.signals {
		switch sig.Name {
		case dbusName + ".NameOwnerChanged":
			if len(sig.Body) == 3 {
				if newOwner, _ := sig.Body[2].(string); newOwner != "" {
					it.register()
				} else {
					it.notifyHost(false)
				}
			}
		case watcherIface + ".StatusNotifierHostRegistered":
			it.notifyHost(true)
		case watcherIface + ".StatusNotifierHostUnregistered":
			it.notifyHost(it.HostAvailable())
		}
	}
}

// SetIcon sets the icon from images of different sizes.
func (it *Item) SetIcon(images []image.Image) {
	pixmaps := make([]pixmap, 0, len(images))
	for _, img := range images {
		pixmaps = append(pixmaps, toPixmap(img))
	}
	it.props.SetMust(itemIface, "IconPixmap", pixmaps)
	it.emit("NewIcon")
}

// SetTooltip sets the tooltip text.
func (it *Item) SetTooltip(text string) {
	it.props.SetMust(itemIface, "ToolTip", tooltip{Title: text, IconPixmap: []pixmap{}})
	it.emit("NewToolTip")
}

// SetPassive hides the icon (Passive status) or shows it again. Hosts hide
// passive items, which is how the icon gives way to the GNOME extension's.
func (it *Item) SetPassive(passive bool) {
	status := "Active"
	if passive {
		status = "Passive"
	}
	it.props.SetMust(itemIface, "Status", status)
	_ = it.conn.Emit(itemPath, itemIface+".NewStatus", status)
}

func (it *Item) emit(signal string) {
	_ = it.conn.Emit(itemPath, itemIface+"."+signal)
}

// Close removes the icon.
func (it *Item) Close() {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.closed {
		return
	}
	it.closed = true
	it.conn.Close()
}

// toPixmap converts an image to ARGB32 in network byte order, as SNI wants.
func toPixmap(img image.Image) pixmap {
	b := img.Bounds()
	data := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			// Un-premultiply: SNI pixmaps are straight alpha.
			if a > 0 && a < 0xffff {
				r, g, bl = r*0xffff/a, g*0xffff/a, bl*0xffff/a
			}
			data = append(data, byte(a>>8), byte(r>>8), byte(g>>8), byte(bl>>8))
		}
	}
	return pixmap{W: int32(b.Dx()), H: int32(b.Dy()), Data: data}
}

type itemObject struct{ it *Item }

func (o *itemObject) Activate(x, y int32) *dbus.Error {
	if o.it.opts.Activate != nil {
		go o.it.opts.Activate(int(x), int(y))
	}
	return nil
}

// SecondaryActivate (middle click) opens the popup too.
func (o *itemObject) SecondaryActivate(x, y int32) *dbus.Error {
	return o.Activate(x, y)
}

// ContextMenu is only called by hosts that don't show the dbusmenu
// themselves; the popup is the closest thing.
func (o *itemObject) ContextMenu(x, y int32) *dbus.Error {
	return o.Activate(x, y)
}

func (o *itemObject) Scroll(delta int32, orientation string) *dbus.Error { return nil }
