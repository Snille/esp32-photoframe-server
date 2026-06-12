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

// RotateDeg returns src rotated counter-clockwise by deg (one of 0/90/180/270).
// This is the single rotation primitive the orientation pipeline uses, in both
// directions: 90° here is the inverse of 270°, so rotating the panel-native
// buffer by deg yields the viewing orientation, and rotating a viewing-oriented
// image by (360-deg) yields the native panel layout. Any non-multiple of 90
// falls through to deg=0 (no rotation).
func RotateDeg(src image.Image, deg int) image.Image {
	switch ((deg % 360) + 360) % 360 {
	case 90:
		return rotate90CCW(src)
	case 180:
		b := src.Bounds()
		w, h := b.Dx(), b.Dy()
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 270:
		// CCW 270° == CW 90°.
		b := src.Bounds()
		w, h := b.Dx(), b.Dy()
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				// CW: (x,y) -> (h-1-y, x)
				dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	default:
		return src
	}
}

// LogicalDims returns the viewing-orientation dimensions for a panel of native
// size nativeW×nativeH mounted at the given rotation: 90°/270° swap width and
// height, 0°/180° keep them.
func LogicalDims(nativeW, nativeH, deg int) (int, int) {
	if d := ((deg % 360) + 360) % 360; d == 90 || d == 270 {
		return nativeH, nativeW
	}
	return nativeW, nativeH
}

// ProcessedToThumbnailJPEG turns a frame's processed output into a small JPEG
// preview that truthfully reflects the applied processing (grayscale, palette,
// tone) — unlike the converter's own thumbnail, which is snapshotted pre-dither
// and ignores those filters. format is "png", "epdgz", or anything else (raw
// EPD bytes). width/height are the native panel dimensions of the buffer.
// deg rotates the native-layout buffer back to the viewing orientation (the
// processed output is in native panel layout; the frame is mounted at deg, so
// the viewer sees it rotated by deg). maxLong caps the long side (0 = no
// downscale).
func ProcessedToThumbnailJPEG(processed []byte, format string, width, height, maxLong, deg int) ([]byte, error) {
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

	// Rotate the native panel layout to the viewing orientation.
	src = RotateDeg(src, deg)

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
