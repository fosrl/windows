package ui

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <stdlib.h>

static int pangolinMessageBox(void *parent, int type, int buttons, int defaultYes,
		const char *title, const char *message) {
	GtkWidget *d = gtk_message_dialog_new(parent ? GTK_WINDOW(parent) : NULL,
		parent ? GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT : 0,
		(GtkMessageType)type, (GtkButtonsType)buttons, "%s", message);
	gtk_window_set_title(GTK_WINDOW(d), title);
	gtk_window_set_keep_above(GTK_WINDOW(d), TRUE);
	if (buttons == GTK_BUTTONS_YES_NO) {
		gtk_dialog_set_default_response(GTK_DIALOG(d),
			defaultYes ? GTK_RESPONSE_YES : GTK_RESPONSE_NO);
	}
	int r = gtk_dialog_run(GTK_DIALOG(d));
	gtk_widget_destroy(d);
	return r;
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The message dialogs below block until dismissed. Call them off the main
// thread. They use GtkMessageDialog directly rather than Wails' dialogs,
// which don't report a dismissal and so can't be waited on.

// messageBox shows a GTK message dialog on the main thread and returns its
// response ID.
func messageBox(owner application.Window, title, message string, msgType, buttons C.int, defaultYes bool) C.int {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	var def C.int
	if defaultYes {
		def = 1
	}
	var result C.int
	application.InvokeSync(func() {
		var parent unsafe.Pointer
		if owner != nil {
			parent = owner.NativeWindow()
		}
		result = C.pangolinMessageBox(parent, msgType, buttons, def, cTitle, cMessage)
	})
	return result
}

func showInfo(owner application.Window, title, message string) {
	messageBox(owner, title, message, C.GTK_MESSAGE_INFO, C.GTK_BUTTONS_OK, false)
}

func showWarning(owner application.Window, title, message string) {
	messageBox(owner, title, message, C.GTK_MESSAGE_WARNING, C.GTK_BUTTONS_OK, false)
}

func showError(owner application.Window, title, message string) {
	messageBox(owner, title, message, C.GTK_MESSAGE_ERROR, C.GTK_BUTTONS_OK, false)
}

// confirm shows a Yes/No question and reports whether Yes was chosen.
func confirm(owner application.Window, title, message string, defaultYes bool) bool {
	return messageBox(owner, title, message, C.GTK_MESSAGE_QUESTION, C.GTK_BUTTONS_YES_NO, defaultYes) == C.GTK_RESPONSE_YES
}
