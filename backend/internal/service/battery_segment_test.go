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

func TestTheilSenSlope_MatchesOLSOnCleanLinearData(t *testing.T) {
	// A perfectly linear series has only one slope, so Theil-Sen (the median
	// of all pairwise slopes) and OLS must agree exactly.
	pts := mkpts(24, 90, 85, 80, 75, 70, 65, 60) // -5 %/day
	if got := theilSenSlope(pts); got < -5.001 || got > -4.999 {
		t.Errorf("theilSenSlope = %v, want -5", got)
	}
}

func TestTheilSenSlope_ResistsASingleOutlier(t *testing.T) {
	// Same clean -5%/day decline, but one reading collapses to near-zero for
	// a single sample (the kind of WiFi-TX rail sag that occasionally slips
	// past despiking) before recovering back onto the trend line. An OLS fit
	// gets dragged hard toward that one point; Theil-Sen, whose result is a
	// median over all pairwise slopes, should barely move.
	clean := mkpts(24, 90, 85, 80, 75, 70, 65, 60)
	withOutlier := append([]pt(nil), clean...)
	withOutlier[0].y = 9 // one bad reading among seven, at the high-leverage end

	cleanSlope := theilSenSlope(clean)
	dirtySlope := theilSenSlope(withOutlier)
	if diff := dirtySlope - cleanSlope; diff < -1.5 || diff > 1.5 {
		t.Errorf("theilSenSlope moved by %.2f (%.2f -> %.2f) from a single outlier, want <1.5 %%/day of drift", diff, cleanSlope, dirtySlope)
	}

	// An OLS slope on the same data, for contrast — documents just how much
	// worse the estimator this replaced would have done here.
	var n, sumX, sumY, sumXY, sumXX float64
	for _, p := range withOutlier {
		n++
		sumX += p.x
		sumY += p.y
		sumXY += p.x * p.y
		sumXX += p.x * p.x
	}
	olsSlope := (n*sumXY - sumX*sumY) / (n*sumXX - sumX*sumX)
	if diff := olsSlope - cleanSlope; diff > -3 && diff < 3 {
		t.Fatalf("test setup problem: OLS only moved %.2f — outlier isn't disruptive enough to demonstrate the contrast", diff)
	}
}

func TestDischargeRuns_SplitsOnARealRecharge(t *testing.T) {
	// A fast 10h discharge (90 -> 50), a recharge back up to 90, then a much
	// calmer 20h discharge (90 -> 84.3) that's still ongoing at window end.
	pts := mkpts(2,
		90, 82, 74, 66, 58, 50, // idx 0-5: fast discharge
		70, 90, // idx 6-7: charging (rise > chargeRise, peaks at idx7)
		87, 86, 85.7, 85.5, 85.3, 85.1, 84.9, 84.7, 84.5, 84.3, // idx 8-17: calm discharge
	)
	runs := dischargeRuns(pts)
	want := [][2]int{{0, 5}, {7, 17}}
	if len(runs) != len(want) {
		t.Fatalf("dischargeRuns returned %v, want %v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("run %d = %v, want %v", i, runs[i], want[i])
		}
	}
}

func TestRobustDrainAcrossWindow_PicksTheWorstConfirmedRun(t *testing.T) {
	// Same shape as the dischargeRuns test above: an old FAST run (96%/day)
	// followed by a recharge and a calm CURRENT run (~7%/day). A device
	// that's proven capable of draining at 96%/day once shouldn't get a
	// "days remaining" built only on its current calm stretch — the whole
	// point of this estimator for trip planning is to assume the worse,
	// already-observed case can recur.
	pts := mkpts(2,
		90, 82, 74, 66, 58, 50,
		70, 90,
		87, 86, 85.7, 85.5, 85.3, 85.1, 84.9, 84.7, 84.5, 84.3,
	)
	drain, ok := robustDrainAcrossWindow(pts)
	if !ok {
		t.Fatalf("robustDrainAcrossWindow: ok = false, want true")
	}
	if drain < 50 {
		t.Errorf("drain = %.1f, want it close to the old fast run's ~96%%/day, not the calm current run's ~7%%/day", drain)
	}
}

func TestRobustDrainAcrossWindow_IgnoresRunsShorterThanMinSpan(t *testing.T) {
	// idx 0-1 is a dramatic-looking 90->40 drop, but in just 1 hour — an
	// implied rate over 1000%/day that must NOT count as "confirmed"
	// evidence. Only the 13h run afterward (idx 3-16, a real ~12-17%/day
	// decline) clears batteryMinSpan and should be what's reported.
	pts := mkpts(1, // 1h cadence
		90, 40, // idx 0-1: 1h span, huge drop — too short to trust
		60, 90, // idx 2-3: charging back up, peaks at idx3
		87, 86.5, 86, 85.5, 85, 84.5, 84, 83.5, 83, 82.5, 82, 81.5, 81, // idx 4-16: 13h calm discharge
	)
	drain, ok := robustDrainAcrossWindow(pts)
	if !ok {
		t.Fatalf("robustDrainAcrossWindow: ok = false, want true (the 13h run should qualify)")
	}
	if drain > 40 {
		t.Errorf("drain = %.1f — looks like the too-short 1h blip (implied ~1200%%/day) leaked in despite failing batteryMinSpan", drain)
	}
}
