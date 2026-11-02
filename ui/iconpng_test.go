package ui

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/fosrl/windows/icons"
)

func TestICOToPNG(t *testing.T) {
	for _, tc := range []struct{ size, want int }{{1, 16}, {40, 48}, {64, 64}, {1000, 256}} {
		data, err := icoToPNG(icons.Orange, tc.size)
		if err != nil {
			t.Fatalf("size %d: %v", tc.size, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("size %d: decode: %v", tc.size, err)
		}
		if got := img.Bounds().Dx(); got != tc.want {
			t.Errorf("size %d: got %dpx frame, want %d", tc.size, got, tc.want)
		}
	}
}
