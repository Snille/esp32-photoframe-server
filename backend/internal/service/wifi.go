package service

import (
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
)

// ParseWifiHeaders reads the Wi-Fi link quality a frame reports on a pull:
// X-Wifi-RSSI (dBm of the connected AP) and X-Wifi-Kbps (throughput of the
// previous download). Implausible values come back as 0 = "not reported", so a
// garbled header never lands in the log or on the photo.
func ParseWifiHeaders(rssiStr, kbpsStr string) (rssi, kbps int) {
	if v, err := strconv.Atoi(strings.TrimSpace(rssiStr)); err == nil && v < 0 && v >= -120 {
		rssi = v
	}
	if v, err := strconv.Atoi(strings.TrimSpace(kbpsStr)); err == nil && v > 0 && v <= 1000000 {
		kbps = v
	}
	return rssi, kbps
}

// WifiSignalLevel buckets an RSSI into a coarse quality level, 0 (unusable)
// to 4 (excellent). Thresholds are the usual rule-of-thumb bands for 2.4 GHz
// IoT links: an ESP32 still pulls an image fine around -75, gets flaky past
// -80 and drops out near -90. Returns -1 for "not reported".
func WifiSignalLevel(rssi int) int {
	switch {
	case rssi == 0:
		return -1
	case rssi >= -55:
		return 4
	case rssi >= -67:
		return 3
	case rssi >= -75:
		return 2
	case rssi >= -82:
		return 1
	default:
		return 0
	}
}

// WifiSignalQuality names an RSSI's level for HA and the Devices list.
func WifiSignalQuality(rssi int) string {
	switch WifiSignalLevel(rssi) {
	case 4:
		return "excellent"
	case 3:
		return "good"
	case 2:
		return "fair"
	case 1:
		return "weak"
	case 0:
		return "poor"
	default:
		return ""
	}
}

// FormatWifiOverlay returns the on-photo chip text and the signal level
// (see WifiSignalLevel) for an RSSI. Empty text means there is nothing to draw.
func FormatWifiOverlay(rssi int) (text string, level int) {
	level = WifiSignalLevel(rssi)
	if level < 0 {
		return "", -1
	}
	return fmt.Sprintf("%d dBm", rssi), level
}

// WifiLitBands is how many of the icon's four bands are drawn solid for a
// signal level, from the bottom up: all four for an excellent signal, only the
// bottom one for a weak or poor one. The rest are drawn faint, so the empty
// part of the fan still shows and the icon keeps its shape.
func WifiLitBands(level int) int {
	if level < 1 {
		return 1
	}
	return level
}

// wifiBandPaths are the four bands of the Wi-Fi icon, innermost first: a 90°
// fan opening upwards from an apex at the bottom centre, split into concentric
// bands with a small gap between them. Drawn as SVG instead of a font glyph so
// unlit bands can be faint rather than outlined, and so the icon renders the
// same in the frame and in the web preview (which builds the same paths).
var wifiBandPaths = func() []string {
	const cx, cy = 15.0, 21.0
	radii := [][2]float64{{0, 4.1}, {5.3, 9.4}, {10.6, 14.7}, {15.9, 20}}
	pt := func(r float64, left bool) (float64, float64) {
		d := r * math.Sqrt2 / 2
		if left {
			return cx - d, cy - d
		}
		return cx + d, cy - d
	}
	paths := make([]string, 0, len(radii))
	for _, rr := range radii {
		inner, outer := rr[0], rr[1]
		lx, ly := pt(outer, true)
		rx, ry := pt(outer, false)
		d := fmt.Sprintf("M%.2f %.2fA%.2f %.2f 0 0 1 %.2f %.2f", lx, ly, outer, outer, rx, ry)
		if inner == 0 {
			d += fmt.Sprintf("L%.2f %.2fZ", cx, cy)
		} else {
			ix, iy := pt(inner, false)
			jx, jy := pt(inner, true)
			d += fmt.Sprintf("L%.2f %.2fA%.2f %.2f 0 0 0 %.2f %.2fZ", ix, iy, inner, inner, jx, jy)
		}
		paths = append(paths, d)
	}
	return paths
}()

// WifiIconSVG renders the Wi-Fi icon for a signal level as inline SVG, lit
// bands solid and the rest faint, in the chip's text colour.
func WifiIconSVG(level int) template.HTML {
	lit := WifiLitBands(level)
	var b strings.Builder
	b.WriteString(`<svg class="wifi-icon" viewBox="0 0 30 22" aria-hidden="true">`)
	for i, d := range wifiBandPaths {
		opacity := "1"
		if i >= lit {
			opacity = "0.28"
		}
		fmt.Fprintf(&b, `<path d="%s" fill="currentColor" opacity="%s"/>`, d, opacity)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
