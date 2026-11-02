//go:build !gtk3

package ui

// The Linux UI is built on GTK3: GTK4 can't position windows, which the tray
// popup needs, and the cgo in this package links GTK3. Build with -tags gtk3.
var _ = pangolinNeedsTheGtk3BuildTag
