package service

import "testing"

func TestResolveFirmwareSource(t *testing.T) {
	cases := []struct {
		name          string
		device        string
		globalDefault string
		want          string
	}{
		// Nothing configured anywhere: GitHub, the conservative side — the
		// server can only point at a real release, never substitute the binary.
		{"nothing set", "", "", FirmwareSourceGitHub},
		{"global says server", "", "server", FirmwareSourceServer},
		{"device overrides global", "github", "server", FirmwareSourceGitHub},
		{"device opts into server", "server", "github", FirmwareSourceServer},
		{"device unset follows global", "", "github", FirmwareSourceGitHub},
		// Anything unrecognised must not silently mean "server".
		{"garbage device value falls through to global", "nonsense", "server", FirmwareSourceServer},
		{"garbage everywhere", "nonsense", "rubbish", FirmwareSourceGitHub},
		{"case and whitespace tolerated", " Server ", "", FirmwareSourceServer},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveFirmwareSource(tc.device, tc.globalDefault); got != tc.want {
				t.Fatalf("ResolveFirmwareSource(%q, %q) = %q, want %q",
					tc.device, tc.globalDefault, got, tc.want)
			}
		})
	}
}
