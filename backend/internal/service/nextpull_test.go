package service

import (
	"testing"
	"time"
)

func TestParsePosixOffsetSeconds(t *testing.T) {
	cases := []struct {
		tz   string
		want int
		ok   bool
	}{
		{"UTC-2", 7200, true},   // POSIX inverted: local = UTC+2
		{"UTC+5", -18000, true}, // local = UTC-5
		{"UTC0", 0, true},
		{"CET-1CEST,M3.5.0,M10.5.0/3", 3600, true}, // std CET = UTC+1
		{"<+05>-5", 18000, true},                   // local = UTC+5
		{"EST5EDT,M3.2.0,M11.1.0", -18000, true},   // std EST = UTC-5
		{"UTC", 0, false},                          // no numeric offset
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := parsePosixOffsetSeconds(c.tz)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parsePosixOffsetSeconds(%q) = %d,%v; want %d,%v", c.tz, got, ok, c.want, c.ok)
		}
	}
}

func TestComputeNextPull_ObservedCadence(t *testing.T) {
	// Reproduces the prod bug scenario: 15-min interval, frame in UTC+2, daytime.
	// Last serve 09:08:34 local (07:08:34 UTC). Next pull must be ~one interval
	// later (09:23:34 local), NOT pushed to the top of the hour.
	pc := pollConfig{
		rotateInterval: 900,
		autoRotate:     true,
		aligned:        true,
		sleepEnabled:   true,
		sleepStart:     1320, // 22:00
		sleepEnd:       420,  // 07:00
		timezone:       "UTC-2",
	}
	lastSeen := time.Date(2026, 6, 22, 7, 8, 34, 0, time.UTC)
	now := time.Date(2026, 6, 22, 7, 14, 0, 0, time.UTC)
	got := computeNextPullAt(lastSeen, now, pc)
	want := lastSeen.Add(900 * time.Second) // 07:23:34 UTC
	if !got.Equal(want) {
		t.Errorf("daytime next pull = %s; want %s", got.UTC(), want.UTC())
	}
}

func TestComputeNextPull_SleepWindowPush(t *testing.T) {
	// A wake that would land inside the quiet window (22:00–07:00 local) is
	// resumed at the local end-of-window (07:00 local = 05:00 UTC for UTC+2).
	pc := pollConfig{
		rotateInterval: 900,
		sleepEnabled:   true,
		sleepStart:     1320, // 22:00
		sleepEnd:       420,  // 07:00
		timezone:       "UTC-2",
	}
	// Last serve 23:50 local (21:50 UTC) -> candidate 00:05 local, inside window.
	lastSeen := time.Date(2026, 6, 22, 21, 50, 0, 0, time.UTC)
	now := time.Date(2026, 6, 22, 21, 55, 0, 0, time.UTC)
	got := computeNextPullAt(lastSeen, now, pc)
	// Expect 07:00 local next day = 05:00 UTC on 2026-06-23.
	want := time.Date(2026, 6, 23, 5, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("sleep-window next pull = %s; want %s", got.UTC(), want.UTC())
	}
}

func TestComputeNextPull_NoSleepSchedule(t *testing.T) {
	pc := pollConfig{rotateInterval: 600, sleepEnabled: false}
	lastSeen := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 22, 12, 3, 0, 0, time.UTC)
	got := computeNextPullAt(lastSeen, now, pc)
	want := lastSeen.Add(600 * time.Second)
	if !got.Equal(want) {
		t.Errorf("no-schedule next pull = %s; want %s", got.UTC(), want.UTC())
	}
}
