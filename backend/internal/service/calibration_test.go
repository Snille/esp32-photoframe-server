package service

import "testing"

func TestCalibrationNeedsRestore(t *testing.T) {
	cases := []struct {
		name     string
		reported float64
		stored   float64
		want     bool
	}{
		{
			// The case this exists for: the frame came up on the factory default
			// after a re-flash wiped its NVS, and we still hold the real value.
			name: "frame lost its calibration", reported: 1.0, stored: 1.0125, want: true,
		},
		{
			// The frame is the source of truth — never push over a value it has.
			name: "frame has its own calibration", reported: 1.0125, stored: 1.0300, want: false,
		},
		{
			name: "nothing stored to give back", reported: 1.0, stored: 0, want: false,
		},
		{
			// A stored factory default is not worth restoring over a factory default.
			name: "stored value is itself the default", reported: 1.0, stored: 1.0, want: false,
		},
		{
			// Older firmware / boards with no divider send nothing.
			name: "frame reported nothing", reported: 0, stored: 1.0125, want: false,
		},
		{
			// The frame stores the scale as fixed-point, so an exact 1.0 comes
			// back with float fuzz; it must still count as the default.
			name: "float fuzz around the default", reported: 1.0000004768, stored: 1.0125, want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CalibrationNeedsRestore(tc.reported, tc.stored); got != tc.want {
				t.Fatalf("CalibrationNeedsRestore(%v, %v) = %v, want %v",
					tc.reported, tc.stored, got, tc.want)
			}
		})
	}
}
