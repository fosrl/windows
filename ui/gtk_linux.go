package ui

/*
#cgo pkg-config: gtk+-3.0 gtk+-x11-3.0
#include <gtk/gtk.h>
#include <gdk/gdkx.h>

static double pangolinScreenResolution(void) {
	GdkScreen *s = gdk_screen_get_default();
	return s ? gdk_screen_get_resolution(s) : -1;
}

static int pangolinIsComposited(void) {
	GdkScreen *s = gdk_screen_get_default();
	return s ? gdk_screen_is_composited(s) : 1;
}

static void pangolinRootSize(int *w, int *h) {
	GdkScreen *s = gdk_screen_get_default();
	G_GNUC_BEGIN_IGNORE_DEPRECATIONS
	*w = s ? gdk_screen_get_width(s) : 0;
	*h = s ? gdk_screen_get_height(s) : 0;
	G_GNUC_END_IGNORE_DEPRECATIONS
}

static int pangolinWindowScale(void) {
	GdkScreen *s = gdk_screen_get_default();
	GdkWindow *root = s ? gdk_screen_get_root_window(s) : NULL;
	return root ? gdk_window_get_scale_factor(root) : 1;
}

static int pangolinIsX11(void) {
	GdkDisplay *d = gdk_display_get_default();
	return d && GDK_IS_X11_DISPLAY(d);
}

static void pangolinPopupHints(void *w) {
	GtkWindow *win = GTK_WINDOW(w);
	gtk_window_set_skip_taskbar_hint(win, TRUE);
	gtk_window_set_skip_pager_hint(win, TRUE);
	gtk_window_set_keep_above(win, TRUE);
	if (!gtk_widget_get_mapped(GTK_WIDGET(win))) {
		gtk_window_set_type_hint(win, GDK_WINDOW_TYPE_HINT_UTILITY);
	}
}

// Present with a fresh X server timestamp: window managers (Mutter in
// particular) refuse focus to a window presented without a recent one.
static void pangolinPresentNow(void *w) {
	GtkWindow *win = GTK_WINDOW(w);
	GdkWindow *gw = gtk_widget_get_window(GTK_WIDGET(win));
	guint32 t = GDK_CURRENT_TIME;
	if (gw && GDK_IS_X11_WINDOW(gw)) {
		t = gdk_x11_get_server_time(gw);
	}
	gtk_window_present_with_time(win, t);
}

static int pangolinPointer(int *x, int *y) {
	GdkDisplay *d = gdk_display_get_default();
	if (!d) return 0;
	GdkSeat *seat = gdk_display_get_default_seat(d);
	if (!seat) return 0;
	GdkDevice *p = gdk_seat_get_pointer(seat);
	if (!p) return 0;
	gdk_device_get_position(p, NULL, x, y);
	return 1;
}

static void pangolinPreferDark(int dark) {
	GtkSettings *s = gtk_settings_get_default();
	if (s) g_object_set(s, "gtk-application-prefer-dark-theme", dark ? TRUE : FALSE, NULL);
}
*/
import "C"

import (
	"math"
	"os"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// These run their GTK calls on the main thread; called from the main thread,
// InvokeSync runs them inline.

// uiScale is how much to scale web content and window sizes. Under XWayland
// on KDE, and on X11 with a raised DPI, GTK windows are sized in physical
// pixels at scale 1 and only the font DPI says how big things should be.
// When GTK scales windows itself (GDK_SCALE), it reports the unscaled DPI, so
// this stays 1. PANGOLIN_UI_SCALE overrides it.
func uiScale() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("PANGOLIN_UI_SCALE"), 64); err == nil && v >= 0.5 && v <= 4 {
		return v
	}
	var dpi float64
	application.InvokeSync(func() { dpi = float64(C.pangolinScreenResolution()) })
	if dpi <= 0 {
		return 1
	}
	// Round to a quarter, so 143.9 DPI is 1.5 and 100 DPI is 1.
	scale := math.Round(dpi/96*4) / 4
	return math.Min(math.Max(scale, 1), 4)
}

// gtkScale is GTK's own integer window scale (GDK_SCALE). GTK positions
// windows in units of this many X pixels.
func gtkScale() int {
	var s C.int
	application.InvokeSync(func() { s = C.pangolinWindowScale() })
	if s < 1 {
		return 1
	}
	return int(s)
}

func screenComposited() bool {
	var c C.int
	application.InvokeSync(func() { c = C.pangolinIsComposited() })
	return c != 0
}

// rootWindowSize is the size of the whole X screen in GTK units.
func rootWindowSize() (int, int) {
	var w, h C.int
	application.InvokeSync(func() { C.pangolinRootSize(&w, &h) })
	return int(w), int(h)
}

func runningOnX11() bool {
	var x C.int
	application.InvokeSync(func() { x = C.pangolinIsX11() })
	return x != 0
}

// pointerPosition returns the pointer in X screen coordinates. Under XWayland
// it is only current while the pointer is over an X window.
func pointerPosition() (int, int, bool) {
	var x, y, ok C.int
	application.InvokeSync(func() { ok = C.pangolinPointer(&x, &y) })
	return int(x), int(y), ok != 0
}

func setPopupHints(w application.Window) {
	application.InvokeSync(func() {
		if native := w.NativeWindow(); native != nil {
			C.pangolinPopupHints(native)
		}
	})
}

func presentNow(w application.Window) {
	application.InvokeSync(func() {
		if native := w.NativeWindow(); native != nil {
			C.pangolinPresentNow(native)
		}
	})
}

func setPreferDark(dark bool) {
	d := C.int(0)
	if dark {
		d = 1
	}
	application.InvokeAsync(func() { C.pangolinPreferDark(d) })
}
