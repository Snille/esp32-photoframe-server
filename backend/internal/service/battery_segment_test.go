package service

import "testing"

// mkpts builds a pt series with x spaced hoursApart hours, oldest first.
func mkpts(hoursApart float64, ys ...float64) []pt {
	pts := make([]pt, len(ys))
	for i, y := range ys {
		pts[i] = pt{x: float64(i) * hoursApart / 24.0, y: y}
	}
	return pts
}

func TestTrimToCurrentSegment_ActiveChargeStillRising(t *testing.T) {
	// Matches the real dev-bench data that exposed this bug (PhotoFrame-02,
	// 5-min cadence): a steady overnight discharge, then plugged in and still
	// climbing at the end of the window (segStart used to land on the very
	// last point via peakIdx, leaving <2 points and reporting "insufficient"
	// for a frame that had clearly been charging for hours).
	pts := mkpts(0.25, 53, 51, 49, 47, 45, 43, 41, 45, 50, 56, 62, 67, 71, 75, 78, 80)
	segStart, activeCharge := trimToCurrentSegment(pts)
	if !activeCharge {
		t.Fatalf("activeCharge = false, want true (still rising at window end)")
	}
	// segStart should land at the trough (index 6, y=41) where the climb
	// began, not collapse to (len(pts)-1).
	if want := 6; segStart != want {
		t.Errorf("segStart = %d, want %d (trough of the climb)", segStart, want)
	}
	if got := len(pts[segStart:]); got < 2 {
		t.Errorf("trimmed segment has %d points, want >=2 to regress a slope", got)
	}
}

func TestTrimToCurrentSegment_CompletedRechargeThenDischarging(t *testing.T) {
	// A charge that completed mid-window, with real discharging afterward —
	// the classic "mid-window recharge" case this function was first built
	// for. segStart must land at the peak, not before it (the stale pre-charge
	// decline must not pollute the current discharge slope).
	pts := mkpts(1, 60, 50, 40, 90, 95, 96, 94, 91, 88, 85)
	segStart, activeCharge := trimToCurrentSegment(pts)
	if activeCharge {
		t.Fatalf("activeCharge = true, want false (charge finished, now discharging)")
	}
	if want := 5; segStart != want {
		t.Errorf("segStart = %d, want %d (the peak, index of y=96)", segStart, want)
	}
}

func TestTrimToCurrentSegment_PlainDischargeUntouched(t *testing.T) {
	// No recharge anywhere in the window — should keep everything.
	pts := mkpts(1, 90, 85, 80, 75, 70, 65, 60)
	segStart, activeCharge := trimToCurrentSegment(pts)
	if activeCharge {
		t.Fatalf("activeCharge = true, want false (monotonic discharge)")
	}
	if segStart != 0 {
		t.Errorf("segStart = %d, want 0 (nothing to trim)", segStart)
	}
}

func TestTrimToCurrentSegment_TooFewPoints(t *testing.T) {
	for _, pts := range [][]pt{nil, {{x: 0, y: 50}}} {
		segStart, activeCharge := trimToCurrentSegment(pts)
		if segStart != 0 || activeCharge {
			t.Errorf("trimToCurrentSegment(%v) = (%d, %v), want (0, false)", pts, segStart, activeCharge)
		}
	}
}
