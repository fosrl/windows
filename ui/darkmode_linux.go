package ui

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// systemDarkMode reports whether the desktop prefers a dark color scheme, as
// published by xdg-desktop-portal (KDE, GNOME and XFCE all provide it).
func systemDarkMode() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	call := obj.CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0,
		"org.freedesktop.appearance", "color-scheme")
	if call.Err != nil {
		return false
	}
	var v dbus.Variant
	if err := call.Store(&v); err != nil {
		return false
	}
	// 0 = no preference, 1 = prefer dark, 2 = prefer light.
	scheme, ok := v.Value().(uint32)
	return ok && scheme == 1
}
