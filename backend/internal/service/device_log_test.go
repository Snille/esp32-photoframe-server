package service

import (
	"testing"
	"time"
)

func TestRetentionCutoff(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		value int
		unit  string
		want  time.Time
	}{
		{"30 days", 30, "days", now.AddDate(0, 0, -30)},
		{"6 months", 6, "months", now.AddDate(0, -6, 0)},
		{"2 years", 2, "years", now.AddDate(-2, 0, 0)},
		{"zero value defaults to 6 months", 0, "months", now.AddDate(0, -6, 0)},
		{"negative value defaults to 6 months", -5, "days", now.AddDate(0, -6, 0)},
		{"unknown unit falls back to months", 3, "fortnights", now.AddDate(0, -3, 0)},
		{"empty unit falls back to months", 4, "", now.AddDate(0, -4, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RetentionCutoff(tt.value, tt.unit)
			// Allow a small tolerance for the time.Now() call inside RetentionCutoff
			// vs. the one taken in this test.
			diff := got.Sub(tt.want)
			if diff < -time.Second || diff > time.Second {
				t.Errorf("RetentionCutoff(%d, %q) = %v, want ~%v (diff %v)", tt.value, tt.unit, got, tt.want, diff)
			}
		})
	}
}
