package service

import (
	"testing"
	"time"
)

func TestDeviceOTAPending(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }

	cases := []struct {
		name         string
		otaStartedAt *time.Time
		lastSeenAt   *time.Time
		want         bool
	}{
		{
			name:         "never triggered",
			otaStartedAt: nil,
			lastSeenAt:   ago(time.Minute),
			want:         false,
		},
		{
			// The case this exists for: a sleeping frame can be a whole rotation
			// interval away from reporting, and until it does its stale version
			// would keep the update on offer.
			name:         "just triggered, frame has not checked in since",
			otaStartedAt: ago(2 * time.Minute),
			lastSeenAt:   ago(20 * time.Minute),
			want:         true,
		},
		{
			// Resolves itself — whether the install worked or not. A success
			// reports the new version, a failure reports the old one and the
			// offer legitimately returns.
			name:         "frame checked in after the trigger",
			otaStartedAt: ago(10 * time.Minute),
			lastSeenAt:   ago(2 * time.Minute),
			want:         false,
		},
		{
			name:         "check-in exactly at the trigger counts as an answer",
			otaStartedAt: ago(5 * time.Minute),
			lastSeenAt:   ago(5 * time.Minute),
			want:         false,
		},
		{
			// A frame that never comes back must not hide its button forever.
			name:         "stale trigger past the cap",
			otaStartedAt: ago(otaPendingMaxWait + time.Minute),
			lastSeenAt:   ago(otaPendingMaxWait + 30*time.Minute),
			want:         false,
		},
		{
			name:         "never seen at all, freshly triggered",
			otaStartedAt: ago(time.Minute),
			lastSeenAt:   nil,
			want:         true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeviceOTAPending(tc.otaStartedAt, tc.lastSeenAt); got != tc.want {
				t.Fatalf("DeviceOTAPending() = %v, want %v", got, tc.want)
			}
		})
	}
}
