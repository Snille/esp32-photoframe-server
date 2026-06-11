package imageops

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
)

// epdPalette maps a 4bpp Spectra-6 EPD colour index to a clean display colour.
// The index order matches the converter's paletteToArray:
// 0=black 1=white 2=yellow 3=red 4=reserved 5=blue 6=green. Indices 7..15 are
// never emitted but map to white so a stray nibble can't panic.
var epdPalette = [16]color.RGBA{
	{0, 0, 0, 255},       // 0 black
	{255, 255, 255, 255}, // 1 white
	{255, 255, 0, 255},   // 2 yellow
	{255, 0, 0, 255},     // 3 red
	{0, 0, 0, 255},       // 4 reserved
	{0, 0, 255, 255},     // 5 blue
	{0, 255, 0, 255},     // 6 green
	{255, 255, 255, 255}, {255, 255, 255, 255}, {255, 255, 255, 255},
	{255, 255, 255, 255}, {255, 255, 255, 255}, {255, 255, 255, 255},
	{255, 255, 255, 255}, {255, 255, 255, 255}, {255, 255, 255, 255},
}

// EPDToImage decodes a 4bpp Spectra-6 panel buffer (2 pixels per byte, high
// nibble first, row-major width×height) into an RGBA image.
func EPDToImage(raw []byte, width, height int) (*image.RGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid dimensions %dx%d", width, height)
	}
	if need := (width*height + 1) / 2; len(raw) < need {
		return nil, fmt.Errorf("epd buffer too small: have %d, need %d", len(raw), need)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			b := raw[i/2]
			idx := b & 0x0f
			if i%2 == 0 {
				idx = b >> 4
			}
			img.SetRGBA(x, y, epdPalette[idx])
		}
	}
	return img, nil
}

// rotate90CCW returns src rotated 90° counter-clockwise.
func rotate90CCW(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// CCW: (x,y) -> (y, w-1-x)
			dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// ProcessedToThumbnailJPEG turns a frame's processed output into a small JPEG
// preview that truthfully reflects the applied processing (grayscale, palette,
// tone) — unlike the converter's own thumbnail, which is snapshotted pre-dither
// and ignores those filters. format is "png", "epdgz", or anything else (raw
// EPD bytes). width/height are the native panel dimensions of the buffer.
// rotateCCW un-rotates the native-layout buffer back to the viewing orientation
// (the processed output is rotated into native panel layout when the viewing
// orientation differs). maxLong caps the long side (0 = no downscale).
func ProcessedToThumbnailJPEG(processed []byte, format string, width, height, maxLong int, rotateCCW bool) ([]byte, error) {
	var src image.Image
	switch format {
	case "png":
		m, err := png.Decode(bytes.NewReader(processed))
		if err != nil {
			return nil, fmt.Errorf("decode png: %w", err)
		}
		src = m
	case "epdgz":
		gz, err := gzip.NewReader(bytes.NewReader(processed))
		if err != nil {
			return nil, fmt.Errorf("epdgz reader: %w", err)
		}
		raw, err := io.ReadAll(gz)
		gz.Close()
		if err != nil {
			return nil, fmt.Errorf("epdgz inflate: %w", err)
		}
		if src, err = EPDToImage(raw, width, height); err != nil {
			return nil, err
		}
	default: // raw EPD bytes
		m, err := EPDToImage(processed, width, height)
		if err != nil {
			return nil, err
		}
		src = m
	}

	// Un-rotate native panel layout back to the viewing orientation.
	if rotateCCW {
		src = rotate90CCW(src)
	}

	// Downscale the long side to maxLong. BiLinear blends the dither dots into
	// the colours the eye perceives, which reads cleanly at thumbnail size.
	b := src.Bounds()
	dw, dh := b.Dx(), b.Dy()
	if maxLong > 0 && (dw > maxLong || dh > maxLong) {
		if dw >= dh {
			dh, dw = dh*maxLong/dw, maxLong
		} else {
			dw, dh = dw*maxLong/dh, maxLong
		}
		dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
		xdraw.BiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
		src = dst
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 82}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
