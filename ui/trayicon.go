//go:build windows

package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/icons"
	"github.com/fosrl/windows/tunnel"
)

var (
	overlayIconOnce sync.Once
	overlayIcon     []byte
)

// trayIconForState returns the tray icon for a tunnel state. Transitional
// states get the gray icon with a yellow badge in the bottom-right corner.
func trayIconForState(state tunnel.State) []byte {
	switch state {
	case tunnel.StateRunning:
		return icons.Orange
	case tunnel.StateStopped:
		return icons.Gray
	}
	overlayIconOnce.Do(func() {
		var err error
		overlayIcon, err = composeOverlayIcon(icons.Gray)
		if err != nil {
			logger.Error("Failed to create transitional tray icon: %v", err)
		}
	})
	if overlayIcon == nil {
		return icons.Gray
	}
	return overlayIcon
}

func trayTooltipForState(state tunnel.State) string {
	return fmt.Sprintf("%s: %s", config.AppName, state.DisplayText())
}

func updateTrayForState(state tunnel.State) {
	if systemTray == nil {
		return
	}
	systemTray.SetIcon(trayIconForState(state))
	systemTray.SetTooltip(trayTooltipForState(state))
}

// composeOverlayIcon draws the transitional badge onto the largest PNG frame
// of an .ico file and returns it as PNG bytes.
func composeOverlayIcon(ico []byte) ([]byte, error) {
	base, err := largestPNGFrame(ico)
	if err != nil {
		return nil, err
	}
	b := base.Bounds()
	img := image.NewRGBA(b)
	draw.Draw(img, b, base, b.Min, draw.Src)

	// The old overlay covered 65% of the icon; a badge of that size would hide
	// the logo at 16px, so draw a circle of about half that in the same corner.
	size := b.Dx()
	diameter := float64(size) * 0.5
	outline := float64(size) * 0.06
	cx := float64(b.Max.X) - diameter/2 - 1
	cy := float64(b.Max.Y) - diameter/2 - 1
	fill := color.RGBA{R: 255, G: 200, A: 255}
	edge := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d2 := dx*dx + dy*dy
			r := diameter / 2
			switch {
			case d2 <= (r-outline)*(r-outline):
				img.Set(x, y, fill)
			case d2 <= r*r:
				img.Set(x, y, edge)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func largestPNGFrame(ico []byte) (image.Image, error) {
	if len(ico) < 6 {
		return nil, errors.New("icon too short")
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	var best image.Image
	bestSize := 0
	for i := 0; i < count; i++ {
		entry := 6 + 16*i
		if entry+16 > len(ico) {
			break
		}
		size := int(binary.LittleEndian.Uint32(ico[entry+8 : entry+12]))
		offset := int(binary.LittleEndian.Uint32(ico[entry+12 : entry+16]))
		if offset+size > len(ico) || size < 8 || !bytes.HasPrefix(ico[offset:], []byte("\x89PNG")) {
			continue
		}
		img, err := png.Decode(bytes.NewReader(ico[offset : offset+size]))
		if err != nil {
			continue
		}
		if w := img.Bounds().Dx(); w > bestSize {
			best, bestSize = img, w
		}
	}
	if best == nil {
		return nil, errors.New("icon has no PNG frame")
	}
	return best, nil
}
