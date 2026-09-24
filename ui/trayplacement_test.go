package ui

import "testing"

func TestPlaceTrayWindowTaskbarBottom(t *testing.T) {
	// 1920x1080 screen with a 48px taskbar at the bottom; the click is on the
	// tray icon, below the work area.
	work := trayRect{X: 0, Y: 0, Width: 1920, Height: 1032}
	rect, layout := placeTrayWindow(1800, 1056, 420, work)

	if rect.Height != 420 {
		t.Fatalf("window must keep the full menu height, got %d", rect.Height)
	}
	if rect.Y+rect.Height != 1032 {
		t.Fatalf("window should sit on top of the taskbar, bottom at %d", rect.Y+rect.Height)
	}
	if layout.Anchor != "bottom" || layout.SubmenuSide != "left" {
		t.Fatalf("unexpected layout %+v", layout)
	}
	if rect.X+rect.Width > 1920 {
		t.Fatalf("window runs off the right edge: %+v", rect)
	}
}

func TestPlaceTrayWindowTaskbarTop(t *testing.T) {
	work := trayRect{X: 0, Y: 48, Width: 1920, Height: 1032}
	rect, layout := placeTrayWindow(1800, 24, 420, work)
	if rect.Y != 48 || rect.Height != 420 || layout.Anchor != "top" {
		t.Fatalf("got %+v %+v", rect, layout)
	}
}

func TestPlaceTrayWindowTaskbarLeft(t *testing.T) {
	work := trayRect{X: 48, Y: 0, Width: 1872, Height: 1080}
	rect, layout := placeTrayWindow(24, 1040, 420, work)
	if rect.X < 48 || rect.Y+rect.Height > 1080 || rect.Height != 420 {
		t.Fatalf("got %+v", rect)
	}
	if layout.SubmenuSide != "right" || layout.Anchor != "bottom" {
		t.Fatalf("unexpected layout %+v", layout)
	}
}

func TestPlaceTrayWindowOpensDownWhenRoom(t *testing.T) {
	work := trayRect{X: 0, Y: 0, Width: 1920, Height: 1032}
	rect, layout := placeTrayWindow(500, 100, 420, work)
	if layout.Anchor != "top" || rect.Y != 100-trayShadowPadding || layout.SubmenuSide != "right" {
		t.Fatalf("got %+v %+v", rect, layout)
	}
}

func TestPlaceTrayWindowTallerThanScreen(t *testing.T) {
	work := trayRect{X: 0, Y: 0, Width: 1280, Height: 700}
	rect, _ := placeTrayWindow(1200, 720, 900, work)
	if rect.Y != 0 || rect.Height != 700 {
		t.Fatalf("got %+v", rect)
	}
}
