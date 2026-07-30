package service

import "testing"

func TestBoardFromAssetName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"esp32-photoframe-seeedstudio_xiao_ee02.bin", "seeedstudio_xiao_ee02"},
		{"esp32-photoframe-dfrobot_firebeetle_esp32e.bin", "dfrobot_firebeetle_esp32e"},
		// The merged factory image must never be matched: a frame that pulled it
		// as an OTA image would flash padding over its NVS and lose its config.
		{"photoframe-firmware-seeedstudio_xiao_ee02-merged.bin", ""},
		{"esp32-photoframe-seeedstudio_xiao_ee02.elf", ""},
		{"README.md", ""},
		{"", ""},
	}

	for _, tc := range cases {
		if got := boardFromAssetName(tc.name); got != tc.want {
			t.Errorf("boardFromAssetName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
