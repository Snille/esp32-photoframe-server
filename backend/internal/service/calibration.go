package service

import "math"

// factoryCalScale is what a frame reports when it has no calibration of its own:
// the board driver's default multiplier.
const factoryCalScale = 1.0

// calScaleEpsilon absorbs the float round-trip through JSON and NVS (the frame
// stores the scale as a fixed-point integer, so a value comes back as e.g.
// 1.0125000476837158 rather than exactly 1.0125).
const calScaleEpsilon = 0.0005

// CalibrationIsFactoryDefault reports whether a frame's calibration is
// indistinguishable from "never calibrated".
func CalibrationIsFactoryDefault(scale float64) bool {
	return math.Abs(scale-factoryCalScale) < calScaleEpsilon
}

// CalibrationNeedsRestore decides whether to hand a stored calibration back to a
// frame.
//
// The frame is always the source of truth: a frame that reports a real
// calibration keeps it, and we never push over a value it just measured. The one
// case worth acting on is a frame that has come up on the factory default while
// we hold a real one — which is exactly what a re-flash looks like, since the
// scale lives only in NVS and a merged factory image wipes it.
//
// reported is what the frame just told us; stored is our mirror.
func CalibrationNeedsRestore(reported, stored float64) bool {
	if stored <= 0 || CalibrationIsFactoryDefault(stored) {
		return false // nothing meaningful to give back
	}
	if reported <= 0 {
		return false // frame can't calibrate, or didn't report — leave it alone
	}
	return CalibrationIsFactoryDefault(reported)
}
