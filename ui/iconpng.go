package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
)

// icoToPNG returns the smallest frame of an .ico that is at least size pixels
// wide (or the largest frame, if none is) encoded as PNG. Linux trays and
// window icons take PNG, while the repo ships .ico files.
func icoToPNG(ico []byte, size int) ([]byte, error) {
	var best *image.RGBA
	forEachICOFrame(ico, func(img *image.RGBA) {
		if best == nil || betterFrame(img.Bounds().Dx(), best.Bounds().Dx(), size) {
			best = img
		}
	})
	if best == nil {
		return nil, errors.New("icon has no usable frame")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, best); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// betterFrame reports whether a frame w pixels wide suits size better than one
// cur pixels wide: the smallest frame that is big enough, else the biggest.
func betterFrame(w, cur, size int) bool {
	if (w >= size) != (cur >= size) {
		return w >= size
	}
	if w >= size {
		return w < cur
	}
	return w > cur
}

// forEachICOFrame calls fn with each frame of an .ico it can decode.
func forEachICOFrame(ico []byte, fn func(*image.RGBA)) {
	if len(ico) < 6 {
		return
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	for i := 0; i < count; i++ {
		entry := 6 + 16*i
		if entry+16 > len(ico) {
			break
		}
		n := int(binary.LittleEndian.Uint32(ico[entry+8 : entry+12]))
		offset := int(binary.LittleEndian.Uint32(ico[entry+12 : entry+16]))
		if offset < 0 || n < 8 || offset+n > len(ico) {
			continue
		}
		if img, err := decodeICOFrame(ico[offset : offset+n]); err == nil {
			fn(img)
		}
	}
}
