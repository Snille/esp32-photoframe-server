package service

import (
	"strings"
	"testing"
)

func TestParseWifiHeaders(t *testing.T) {
	cases := []struct {
		rssi, kbps         string
		wantRSSI, wantKbps int
	}{
		{"-62", "1840", -62, 1840},
		{" -71 ", "", -71, 0},
		{"", "", 0, 0},
		{"0", "0", 0, 0},    // 0 dBm / 0 kbit/s are "not reported"
		{"12", "-5", 0, 0},  // positive RSSI / negative speed are garbage
		{"-200", "x", 0, 0}, // out of range / not a number
		{"-120", "1000000", -120, 1000000},
	}
	for _, c := range cases {
		r, k := ParseWifiHeaders(c.rssi, c.kbps)
		if r != c.wantRSSI || k != c.wantKbps {
			t.Errorf("ParseWifiHeaders(%q, %q) = %d, %d; want %d, %d", c.rssi, c.kbps, r, k, c.wantRSSI, c.wantKbps)
		}
	}
}

func TestFormatWifiOverlay(t *testing.T) {
	cases := []struct {
		rssi      int
		wantText  string
		wantLevel int
		lit       int
		quality   string
	}{
		{0, "", -1, 1, ""},
		{-50, "-50 dBm", 4, 4, "excellent"},
		{-55, "-55 dBm", 4, 4, "excellent"},
		{-60, "-60 dBm", 3, 3, "good"},
		{-70, "-70 dBm", 2, 2, "fair"},
		{-80, "-80 dBm", 1, 1, "weak"},
		{-90, "-90 dBm", 0, 1, "poor"},
	}
	for _, c := range cases {
		text, level := FormatWifiOverlay(c.rssi)
		if text != c.wantText || level != c.wantLevel {
			t.Errorf("FormatWifiOverlay(%d) = %q, %d; want %q, %d", c.rssi, text, level, c.wantText, c.wantLevel)
		}
		if lit := WifiLitBands(level); lit != c.lit {
			t.Errorf("WifiLitBands(%d) = %d; want %d", level, lit, c.lit)
		}
		if q := WifiSignalQuality(c.rssi); q != c.quality {
			t.Errorf("WifiSignalQuality(%d) = %q; want %q", c.rssi, q, c.quality)
		}
	}
}

func TestWifiIconSVG(t *testing.T) {
	svg := string(WifiIconSVG(1))
	if n := strings.Count(svg, "<path"); n != 4 {
		t.Fatalf("want 4 bands, got %d", n)
	}
	if n := strings.Count(svg, `opacity="1"`); n != 1 {
		t.Errorf("weak signal: want 1 lit band, got %d", n)
	}
	if n := strings.Count(string(WifiIconSVG(4)), `opacity="1"`); n != 4 {
		t.Errorf("excellent signal: want 4 lit bands, got %d", n)
	}
	// Print the band paths so the web preview's copy can be checked against them.
	for i, d := range wifiBandPaths {
		t.Logf("band %d: %s", i, d)
	}
}
