package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
)

// composeOverlayIcon draws the transitional badge onto every frame of an .ico
// file and returns a new .ico with all frames stored as PNG. Keeping each size
// lets Windows pick the hand-tuned 16/32px frame instead of scaling down the
// 256px one, which looks jagged in the tray.
func composeOverlayIcon(ico []byte) ([]byte, error) {
	if len(ico) < 6 {
		return nil, errors.New("icon too short")
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	var frames []*image.RGBA
	for i := 0; i < count; i++ {
		entry := 6 + 16*i
		if entry+16 > len(ico) {
			break
		}
		size := int(binary.LittleEndian.Uint32(ico[entry+8 : entry+12]))
		offset := int(binary.LittleEndian.Uint32(ico[entry+12 : entry+16]))
		if offset < 0 || size < 8 || offset+size > len(ico) {
			continue
		}
		img, err := decodeICOFrame(ico[offset : offset+size])
		if err != nil {
			continue
		}
		drawBadge(img)
		frames = append(frames, img)
	}
	if len(frames) == 0 {
		return nil, errors.New("icon has no usable frame")
	}
	return encodeICO(frames)
}

// drawBadge draws an antialiased yellow circle with a white outline in the
// bottom-right corner of img. The old overlay covered 65% of the icon; a badge
// of that size would hide the logo at 16px, so it is about half that.
func drawBadge(img *image.RGBA) {
	b := img.Bounds()
	size := float64(b.Dx())
	r := size * 0.25
	outline := math.Max(size*0.06, 1)
	cx := float64(b.Max.X) - r - size/32
	cy := float64(b.Max.Y) - r - size/32
	fill := color.RGBA{R: 255, G: 200, A: 255}
	edge := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	// Supersample each pixel to get smooth edges at small sizes.
	const n = 4
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var inFill, inEdge int
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					dx := float64(x) + (float64(sx)+0.5)/n - cx
					dy := float64(y) + (float64(sy)+0.5)/n - cy
					d := math.Sqrt(dx*dx + dy*dy)
					switch {
					case d <= r-outline:
						inFill++
					case d <= r:
						inEdge++
					}
				}
			}
			if inFill+inEdge == 0 {
				continue
			}
			// Mix fill and outline by their share of the samples, then blend the
			// result over the icon by total coverage.
			total := float64(inFill + inEdge)
			src := color.RGBA{
				R: uint8((float64(fill.R)*float64(inFill) + float64(edge.R)*float64(inEdge)) / total),
				G: uint8((float64(fill.G)*float64(inFill) + float64(edge.G)*float64(inEdge)) / total),
				B: uint8((float64(fill.B)*float64(inFill) + float64(edge.B)*float64(inEdge)) / total),
				A: 255,
			}
			img.SetRGBA(x, y, blendOver(img.RGBAAt(x, y), src, total/(n*n)))
		}
	}
}

// blendOver composites opaque src at the given coverage over dst. Both are
// premultiplied, as image.RGBA stores them.
func blendOver(dst, src color.RGBA, coverage float64) color.RGBA {
	inv := 1 - coverage
	return color.RGBA{
		R: uint8(float64(src.R)*coverage + float64(dst.R)*inv + 0.5),
		G: uint8(float64(src.G)*coverage + float64(dst.G)*inv + 0.5),
		B: uint8(float64(src.B)*coverage + float64(dst.B)*inv + 0.5),
		A: uint8(255*coverage + float64(dst.A)*inv + 0.5),
	}
}

// decodeICOFrame decodes one ICO image, either PNG or a 32-bit BI_RGB DIB.
func decodeICOFrame(data []byte) (*image.RGBA, error) {
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		src, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		b := src.Bounds()
		img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				img.Set(x, y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return img, nil
	}

	if len(data) < 40 || binary.LittleEndian.Uint32(data[0:4]) < 40 {
		return nil, errors.New("unsupported icon frame")
	}
	headerSize := int(binary.LittleEndian.Uint32(data[0:4]))
	w := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(data[8:12]))) / 2 // XOR + AND masks
	bpp := binary.LittleEndian.Uint16(data[14:16])
	compression := binary.LittleEndian.Uint32(data[16:20])
	if bpp != 32 || compression != 0 || w <= 0 || h <= 0 {
		return nil, errors.New("unsupported icon frame format")
	}
	if headerSize+w*h*4 > len(data) {
		return nil, errors.New("icon frame truncated")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	pixels := data[headerSize:]
	for y := 0; y < h; y++ {
		row := pixels[(h-1-y)*w*4:] // rows are stored bottom-up
		for x := 0; x < w; x++ {
			p := row[x*4:]
			img.Set(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: p[3]})
		}
	}
	return img, nil
}

// encodeICO packs images into an .ico file with PNG-compressed frames.
func encodeICO(frames []*image.RGBA) ([]byte, error) {
	encoded := make([][]byte, len(frames))
	for i, img := range frames {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		encoded[i] = buf.Bytes()
	}

	var out bytes.Buffer
	header := make([]byte, 6)
	binary.LittleEndian.PutUint16(header[2:], 1) // type: icon
	binary.LittleEndian.PutUint16(header[4:], uint16(len(frames)))
	out.Write(header)

	offset := 6 + 16*len(frames)
	for i, img := range frames {
		entry := make([]byte, 16)
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if w < 256 {
			entry[0] = byte(w)
		}
		if h < 256 {
			entry[1] = byte(h)
		}
		binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
		binary.LittleEndian.PutUint16(entry[6:], 32) // bits per pixel
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(encoded[i])))
		binary.LittleEndian.PutUint32(entry[12:], uint32(offset))
		out.Write(entry)
		offset += len(encoded[i])
	}
	for _, data := range encoded {
		out.Write(data)
	}
	return out.Bytes(), nil
}
