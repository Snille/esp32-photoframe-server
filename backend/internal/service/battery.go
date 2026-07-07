package service

import (
	"math"
	"sort"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"gorm.io/gorm"
)

// BatteryService records the battery readings devices report on each image
// fetch and derives a drain estimate (percent/day + runtime left) from the
// trailing history. No external hardware: the slope of the reported
// state-of-charge over real elapsed time is the measurement.
type BatteryService struct {
	db *gorm.DB
}

func NewBatteryService(db *gorm.DB) *BatteryService {
	return &BatteryService{db: db}
}

const (
	// Rapid re-fetches (previews, retries, a frame that double-pulls) must not
	// flood the table — one sample per device per this interval is plenty for a
	// device that normally wakes every 15-60 min.
	batterySampleMinInterval = 10 * time.Minute
	// Regression window: recent enough to reflect the current usage pattern,
	// long enough to average out the LiPo voltage/percent noise per wake.
	batteryWindow = 14 * 24 * time.Hour
	// Drop ancient samples so the table stays small. Long enough to cover a
	// 1-year history-chart range with margin; samples are throttled to at
	// most one per batterySampleMinInterval per device, so even 400 days is
	// a trivial row count (~57k/device worst case) for SQLite.
	batteryRetention = 400 * 24 * time.Hour
	// A slope smaller than this (in %/day, either sign) is treated as flat —
	// below the noise floor of the firmware's coarse percent reading.
	batteryFlatThreshold = 0.15
	// Need at least this much real time spanned before a slope means anything.
	batteryMinSpan = 6 * time.Hour
	// A rise of more than this (%SoC) above the recent trough marks a recharge —
	// well above the per-wake voltage/percent noise (loaded voltage jitters a few
	// % per wake), so it won't trip on jitter; a real top-up moves 15-100%.
	batteryChargeRise = 8.0
	// After a charge, SoC must drop this far (%SoC) below the peak before we call
	// the charge finished and begin the new discharge run.
	batteryDischargeHyst = 2.0
	// Cap the points handed back for the sparkline.
	batteryRecentLimit = 60
	// batteryOutlierSoC: a swing bigger than this (%SoC, either direction)
	// against a sample's surrounding readings is treated as a momentary
	// WiFi-TX rail sag rather than a real level change — some boards (the
	// EE02) briefly sag hundreds of mV under the radio's transmit current,
	// which lands well within PlausibleVoltage's bounds (3.3-4.3 V) yet still
	// implies a wildly wrong SoC on the steep part of the LiPo curve (e.g. a
	// healthy ~4.03 V/80% reading briefly at 3.68 V/~9%, back to normal the
	// very next wake). Left untreated this both flashes a bogus "2%"/"83%"/
	// "20%" reading (Devices list, HA sensor, on-photo badge) and gets
	// misread by the discharge-run segmentation below as a full recharge
	// cycle, silently discarding real drain history. Chosen comfortably above
	// the "per-wake voltage/percent noise (loaded voltage jitters a few %)"
	// already documented above, and well below what a real recharge looks
	// like once confirmed by a following reading.
	batteryOutlierSoC = 15.0
	// batteryMedianRadius: despikeSoC compares each reading against a
	// 2*radius+1-wide window of its neighbors (median filter), so it can
	// reject not just a single bad wake but up to `batteryMedianRadius`
	// consecutive ones — observed in the field on one board reporting two
	// sagged readings in a row before recovering.
	batteryMedianRadius = 2
	// batteryRobustMaxPoints caps how many points theilSenSlope considers
	// (subsampled evenly) before computing every pairwise slope — pairwise
	// comparisons are O(n^2), and a run can hold thousands of points at this
	// project's sampling cadence over a multi-day span. A few hundred evenly
	// spread points already characterize a run's rate; more just adds cost.
	batteryRobustMaxPoints = 300
)

// RecordSample stores a reading, throttled per device. Best-effort; callers
// ignore the error. voltageMV may be 0 when the firmware doesn't report it.
func (s *BatteryService) RecordSample(deviceID uint, percent, voltageMV int) error {
	if percent < 0 || percent > 100 {
		return nil
	}
	var last model.BatterySample
	err := s.db.Where("device_id = ?", deviceID).
		Order("sampled_at DESC").First(&last).Error
	if err == nil && time.Since(last.SampledAt) < batterySampleMinInterval {
		return nil
	}
	sample := model.BatterySample{
		DeviceID:  deviceID,
		SampledAt: time.Now(),
		Percent:   percent,
		VoltageMV: voltageMV,
	}
	if err := s.db.Create(&sample).Error; err != nil {
		return err
	}
	// Opportunistic prune — runs at most once per throttle interval per device.
	s.db.Where("sampled_at < ?", time.Now().Add(-batteryRetention)).
		Delete(&model.BatterySample{})
	return nil
}

// BatteryEstimate is the derived drain picture for one device.
type BatteryEstimate struct {
	HasData          bool                  `json:"has_data"`
	CurrentPercent   int                   `json:"current_percent"`
	CurrentVoltageMV int                   `json:"current_voltage_mv"`
	// DrainPerDay (%/day, positive = discharging) is deliberately
	// conservative when Trend is "discharging": it's the WORST
	// robustly-confirmed rate seen across the trailing window, not just the
	// current run's own average — an unusually calm recent stretch shouldn't
	// make "days remaining" claim more runway than the device has actually
	// proven capable of burning through. See Estimate / robustDrainAcrossWindow.
	DrainPerDay float64 `json:"drain_per_day"`
	DaysRemaining    float64               `json:"days_remaining"` // -1 = unknown/charging/stable
	Trend            string                `json:"trend"`          // discharging | charging | stable | insufficient
	SampleCount      int                   `json:"sample_count"`
	WindowStart      time.Time             `json:"window_start"`
	LastSampledAt    time.Time             `json:"last_sampled_at"`
	Recent           []model.BatterySample `json:"recent"`
	// Basis reports what the drain regression ran on: "voltage" (finer, via the
	// LiPo curve) when the frame reports mV, else "percent".
	Basis string `json:"basis"`
	// Plugged is true when the latest reading is physically implausible for a
	// running frame (the EE02-on-USB signature: charging current corrupts the ADC
	// so percent/voltage read garbage). The UI then shows a "plugged in" indicator
	// instead of a bogus level — see BatteryReadingImplausible.
	Plugged bool `json:"plugged"`
	// EstimatedCurrentMA is the average discharge current implied by
	// DrainPerDay, given the device's optional BatteryCapacityMAh (0 when
	// capacity isn't set, or the device isn't discharging). This is purely a
	// diagnostic extra — DaysRemaining is already computed from the %/day
	// trend alone and doesn't need or use capacity.
	EstimatedCurrentMA float64 `json:"estimated_current_ma"`
}

// BatteryReadingImplausible reports that a (percent, voltageMV) pair from a frame
// that is actively checking in cannot be a real battery level — the signature of
// an EE02 on USB, whose charging current corrupts the ADC so both signals read
// garbage (a sub-3.1 V "pack" while the frame is plainly running, or a firmware
// percent that disagrees wildly with the voltage curve). Used to show a "plugged
// in" indicator instead of a bogus 0 %. Returns false when no voltage is given
// (nothing to sanity-check the percent against — e.g. older FireBeetle firmware).
func BatteryReadingImplausible(percent, voltageMV int) bool {
	if voltageMV <= 0 {
		return false
	}
	if voltageMV < 3100 {
		// A frame that just completed a WiFi pull can't be at a near-dead voltage;
		// this is the ADC collapsing under charge current, not a real level.
		return true
	}
	if percent >= 0 {
		d := percent - VoltageToSoC(voltageMV)
		if d < 0 {
			d = -d
		}
		if d > 35 {
			return true // percent and voltage disagree far beyond LiPo sag
		}
	}
	return false
}

// PlausibleVoltage reports whether a reported cell voltage (mV) is in a
// believable resting / light-load band for a running single-cell LiPo frame.
// Below ~3.3 V on a frame that just completed a WiFi pull is almost always a
// momentary rail collapse under TX current, not the real level; above ~4.3 V is
// out of range for a single cell.
func PlausibleVoltage(mv int) bool {
	return mv >= 3300 && mv <= 4300
}

// RobustBadgePercent returns the best battery percentage to draw on the photo
// badge, smoothing over the EE02's occasional collapsed / erratic readings.
// Preference order: voltage-derived SoC when the current voltage is plausible
// AND consistent with this device's recent trend (not a rail-sag spike, see
// despikeSoC) → this device's most recent trustworthy sample → the frame's
// raw percent → -1 (no usable level, caller hides the badge). deviceID 0
// skips the DB fallback (e.g. preview / unknown device).
func (s *BatteryService) RobustBadgePercent(deviceID uint, percent, voltageMV int) int {
	if PlausibleVoltage(voltageMV) {
		soc := voltageToSoC(voltageMV)
		if deviceID == 0 || s.trustedAgainstRecent(deviceID, soc) {
			return int(soc + 0.5)
		}
	}
	if deviceID != 0 {
		if soc, ok := s.trustedRecentSoC(deviceID); ok {
			return int(soc + 0.5)
		}
	}
	if percent >= 0 && percent <= 100 {
		return percent
	}
	return -1
}

// trustedAgainstRecent reports whether soc — a just-reported LIVE reading,
// not yet stored — is consistent with this device's recent stored history,
// via the same despike window used for stored samples (see despikeSoC). Live
// readings get the "no future context yet" edge of that window, same as the
// most recent stored sample would.
func (s *BatteryService) trustedAgainstRecent(deviceID uint, soc float64) bool {
	var recent []model.BatterySample
	if err := s.db.Where("device_id = ?", deviceID).
		Order("sampled_at DESC").Limit(2 * batteryMedianRadius).Find(&recent).Error; err != nil || len(recent) == 0 {
		return true // nothing to compare against yet — don't block the first reading
	}
	n := len(recent)
	socs := make([]float64, n+1)
	for i, sm := range recent { // recent is newest-first; reverse into oldest-first
		socs[n-1-i] = socOf(sm)
	}
	socs[n] = soc
	return despikeSoC(socs)[n]
}

// trustedRecentSoC returns this device's most recent stored sample that
// survives despiking, or (0, false) if none qualify (no history yet, or every
// recent sample is itself flagged). Used to ride out a collapsed live reading
// without flashing a bogus badge value.
func (s *BatteryService) trustedRecentSoC(deviceID uint) (float64, bool) {
	var samples []model.BatterySample
	if err := s.db.Where("device_id = ?", deviceID).
		Order("sampled_at DESC").Limit(20).Find(&samples).Error; err != nil || len(samples) == 0 {
		return 0, false
	}
	n := len(samples)
	socs := make([]float64, n)
	for i, sm := range samples { // samples is newest-first; reverse into oldest-first
		socs[n-1-i] = socOf(sm)
	}
	keep := despikeSoC(socs)
	for i := n - 1; i >= 0; i-- {
		if keep[i] {
			return socs[i], true
		}
	}
	return 0, false
}

// lipoCurve maps a single-cell LiPo resting voltage (mV) to an approximate
// state-of-charge (%). The pack voltage sags under the WiFi/refresh load the
// frame reports at, so absolute SoC isn't exact — but the curve is monotonic,
// so the slope over time (the drain) is far smoother than the firmware's coarse
// integer percentage. Points are interpolated linearly; outside the range it
// clamps to 0/100.
var lipoCurve = []struct{ mv, soc float64 }{
	{4200, 100}, {4150, 95}, {4110, 90}, {4080, 85}, {4020, 80},
	{3980, 75}, {3950, 70}, {3910, 65}, {3870, 60}, {3850, 55},
	{3840, 50}, {3820, 45}, {3800, 40}, {3790, 35}, {3770, 30},
	{3750, 25}, {3730, 20}, {3710, 15}, {3690, 10}, {3610, 5}, {3270, 0},
}

// VoltageToSoC converts a LiPo cell voltage (mV) to an integer state-of-charge
// percentage (0-100) via the same curve the drain estimate uses, or -1 when no
// voltage is given. Exported so the serve path can render a voltage-derived
// battery badge — steadier than the firmware's coarse/erratic percent, which on
// some boards (XIAO EE02 on USB) can read 0% at a healthy 4.1 V.
func VoltageToSoC(mv int) int {
	if mv <= 0 {
		return -1
	}
	return int(voltageToSoC(mv) + 0.5)
}

// voltageToSoC converts a battery voltage (mV) to an estimated state-of-charge
// percentage via lipoCurve (linear interpolation, clamped to [0,100]).
func voltageToSoC(mv int) float64 {
	v := float64(mv)
	if v >= lipoCurve[0].mv {
		return 100
	}
	last := lipoCurve[len(lipoCurve)-1]
	if v <= last.mv {
		return 0
	}
	for i := 0; i < len(lipoCurve)-1; i++ {
		hi, lo := lipoCurve[i], lipoCurve[i+1]
		if v <= hi.mv && v >= lo.mv {
			frac := (v - lo.mv) / (hi.mv - lo.mv)
			return lo.soc + frac*(hi.soc-lo.soc)
		}
	}
	return 0
}

// socOf returns the best available SoC estimate for one sample: voltage-
// derived (finer, steadier) when it carries a voltage, else the firmware's
// raw percent.
func socOf(sm model.BatterySample) float64 {
	if sm.VoltageMV > 0 {
		return voltageToSoC(sm.VoltageMV)
	}
	return float64(sm.Percent)
}

// median returns the middle value of vals (average of the two middle values
// for an even-length slice). Returns 0 for an empty slice.
func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// despikeSoC flags which entries in a chronologically-ordered (oldest first)
// SoC series are trustworthy, using a sliding-window median filter: entry i is
// rejected when it differs from the median of its up-to-`batteryMedianRadius`
// neighbors on EACH side by more than batteryOutlierSoC. This is the standard
// technique for rejecting impulse ("salt and pepper") noise while preserving
// real step changes — a genuine recharge is confirmed by the readings that
// follow it (so it forms a majority within the window), whereas a momentary
// rail sag is contradicted by the readings on both sides of it.
//
// The window naturally narrows at the edges of the slice (fewer neighbors
// available), which is exactly what's wanted for the most recent entry: with
// no "future" reading yet to confirm it, it's judged purely against recent
// history — a real recharge only ever RISES, so this can suppress a spurious
// low reading without ever hiding genuine (rising) charge-detection signal.
func despikeSoC(socs []float64) []bool {
	n := len(socs)
	keep := make([]bool, n)
	for i := 0; i < n; i++ {
		lo := i - batteryMedianRadius
		if lo < 0 {
			lo = 0
		}
		hi := i + batteryMedianRadius
		if hi > n-1 {
			hi = n - 1
		}
		m := median(socs[lo : hi+1])
		d := socs[i] - m
		if d < 0 {
			d = -d
		}
		keep[i] = d <= batteryOutlierSoC
	}
	return keep
}

// pt is one (elapsed-days, SoC%) point in the regression series built by
// Estimate, chronologically ordered (oldest first).
type pt struct{ x, y float64 }

// trimToCurrentSegment finds the slice of a chronologically-ordered (oldest
// first) SoC series that reflects the CURRENT trend, discarding an earlier,
// now-irrelevant run. A mid-window recharge (the user tops the battery up
// over USB) otherwise poisons the least-squares slope: averaging the
// pre-charge decline, the charge jump, and the post-charge decline yields a
// near-flat or rising line, so a plainly-draining frame reads
// "stable"/"charging". Only the rate since the last charge predicts the
// runtime left. It finds the last charge by tracking the recent trough; once
// SoC climbs batteryChargeRise above it, that's charging, and it follows the
// rise to its peak (where charging ends, confirmed by a batteryDischargeHyst
// drop) to start the next run there.
//
// Returns segStart (an index into pts) and activeCharge — true when the
// window ends still mid-charge (still rising, no discharge-after-charge seen
// yet). In that case segStart points at the trough where the current climb
// began (not its peak: while still climbing, the peak keeps re-advancing to
// the latest point on every sample, which would collapse the segment to a
// single point and leave too few to regress — Estimate exempts an
// activeCharge segment from its minimum-span gate for this reason, since
// getting here already required a confirmed, well-above-noise SoC rise).
func trimToCurrentSegment(pts []pt) (segStart int, activeCharge bool) {
	if len(pts) < 2 {
		return 0, false
	}
	minIdx := 0
	peakIdx := 0
	charging := false
	for i := 1; i < len(pts); i++ {
		if !charging {
			if pts[i].y < pts[minIdx].y {
				minIdx = i
			}
			if pts[i].y-pts[minIdx].y > batteryChargeRise {
				charging = true
				peakIdx = i
			}
		} else {
			if pts[i].y >= pts[peakIdx].y {
				peakIdx = i
			} else if pts[peakIdx].y-pts[i].y > batteryDischargeHyst {
				charging = false
				segStart = peakIdx
				minIdx = peakIdx
			}
		}
	}
	if charging {
		return minIdx, true
	}
	return segStart, false
}

// subsamplePts returns pts unchanged if it already has maxN or fewer points,
// else maxN evenly-spaced points (including the first and last) — used to
// bound the O(n^2) cost of theilSenSlope on a long run without meaningfully
// changing the rate it measures.
func subsamplePts(pts []pt, maxN int) []pt {
	if len(pts) <= maxN || maxN < 2 {
		return pts
	}
	out := make([]pt, maxN)
	for i := 0; i < maxN; i++ {
		out[i] = pts[i*(len(pts)-1)/(maxN-1)]
	}
	return out
}

// theilSenSlope returns the median of the pairwise slopes (y_j-y_i)/(x_j-x_i)
// over every pair i<j in pts — a robust regression slope. Unlike the
// ordinary-least-squares slope this replaces, a single noisy point (a
// WiFi-TX rail sag that slips past despiking, say) can only ever contribute
// to a minority of the pairwise slopes, so it can't swing the median the way
// it can swing an OLS fit — especially on the short runs this estimate often
// has to work with.
func theilSenSlope(pts []pt) float64 {
	pts = subsamplePts(pts, batteryRobustMaxPoints)
	var slopes []float64
	for i := 0; i < len(pts); i++ {
		for j := i + 1; j < len(pts); j++ {
			dx := pts[j].x - pts[i].x
			if dx <= 0 {
				continue
			}
			slopes = append(slopes, (pts[j].y-pts[i].y)/dx)
		}
	}
	return median(slopes)
}

// dischargeRuns splits a chronologically-ordered SoC series into every
// discharge run it contains, using the same trough/rise/peak/hysteresis
// state machine as trimToCurrentSegment — generalized to record each
// completed run instead of stopping at the latest one. Used to look across
// the WHOLE trailing window for the worst historically-confirmed drain rate,
// not just whatever the most recent run happens to look like.
func dischargeRuns(pts []pt) [][2]int {
	if len(pts) < 2 {
		return nil
	}
	var runs [][2]int
	segStart := 0
	minIdx := 0
	peakIdx := 0
	charging := false
	for i := 1; i < len(pts); i++ {
		if !charging {
			if pts[i].y < pts[minIdx].y {
				minIdx = i
			}
			if pts[i].y-pts[minIdx].y > batteryChargeRise {
				if minIdx > segStart {
					runs = append(runs, [2]int{segStart, minIdx})
				}
				charging = true
				peakIdx = i
			}
		} else {
			if pts[i].y >= pts[peakIdx].y {
				peakIdx = i
			} else if pts[peakIdx].y-pts[i].y > batteryDischargeHyst {
				charging = false
				segStart = peakIdx
				minIdx = peakIdx
			}
		}
	}
	if !charging && len(pts)-1 > segStart {
		runs = append(runs, [2]int{segStart, len(pts) - 1})
	}
	return runs
}

// robustDrainAcrossWindow scans every discharge run in the trailing window
// (via dischargeRuns) and returns the FASTEST robustly-confirmed rate among
// them — a deliberately conservative "worst case" for planning around
// (e.g. "will this survive a 2-week trip unattended?"), rather than an
// optimistic average of whatever the latest run happens to show. Only runs
// spanning at least batteryMinSpan qualify as confirmed evidence — the same
// bar Estimate already requires before trusting the current run's slope.
func robustDrainAcrossWindow(pts []pt) (drain float64, ok bool) {
	for _, r := range dischargeRuns(pts) {
		run := pts[r[0] : r[1]+1]
		span := time.Duration((run[len(run)-1].x - run[0].x) * float64(24*time.Hour))
		if span < batteryMinSpan {
			continue
		}
		d := -theilSenSlope(run)
		if d > batteryFlatThreshold && (!ok || d > drain) {
			drain, ok = d, true
		}
	}
	return drain, ok
}

// Estimate regresses state-of-charge over time across the trailing window. When
// the samples carry battery voltage (newer firmware), it regresses a voltage-
// derived SoC (finer, smoother) instead of the coarse firmware percentage.
func (s *BatteryService) Estimate(deviceID uint) BatteryEstimate {
	est := BatteryEstimate{Trend: "insufficient", DaysRemaining: -1, Recent: []model.BatterySample{}, Basis: "percent"}

	var samples []model.BatterySample
	if err := s.db.Where("device_id = ? AND sampled_at >= ?", deviceID, time.Now().Add(-batteryWindow)).
		Order("sampled_at ASC").Find(&samples).Error; err != nil {
		return est
	}
	if len(samples) == 0 {
		return est
	}

	est.HasData = true
	est.SampleCount = len(samples)
	last := samples[len(samples)-1]

	// Despike: reject the (rare) momentary WiFi-TX rail sag that would
	// otherwise flash a wildly wrong "current %" and get misread as a full
	// recharge cycle by the discharge-run segmentation below — see
	// despikeSoC / batteryOutlierSoC. effIdx is the most recent sample that
	// survives despiking; "last" itself is kept for LastSampledAt (the true
	// last check-in time is still worth showing even if its reading is
	// momentarily suspect).
	socs := make([]float64, len(samples))
	for i, sm := range samples {
		socs[i] = socOf(sm)
	}
	keep := despikeSoC(socs)
	effIdx := len(samples) - 1
	for effIdx > 0 && !keep[effIdx] {
		effIdx--
	}
	effLast := samples[effIdx]

	est.CurrentPercent = effLast.Percent
	est.CurrentVoltageMV = effLast.VoltageMV
	// Prefer the voltage-derived level for the reported "current %" when the frame
	// sends a voltage: the firmware percent is coarse and, on some boards (XIAO
	// EE02 on USB), erratic — a healthy 4.1 V can read 0%. This feeds the Devices
	// list and the HA battery sensor, so they stay truthful like the on-photo badge.
	// Flag an implausible reading (EE02 on USB) BEFORE overwriting CurrentPercent
	// with the voltage-derived value, so the raw firmware percent can be compared
	// against the voltage.
	est.Plugged = BatteryReadingImplausible(effLast.Percent, effLast.VoltageMV)
	if effLast.VoltageMV > 0 {
		est.CurrentPercent = VoltageToSoC(effLast.VoltageMV)
	}
	est.WindowStart = samples[0].SampledAt
	est.LastSampledAt = last.SampledAt

	// Sparkline: cap to the most recent points. Left as raw (non-despiked)
	// samples — it's a background/context visual, not the headline number,
	// and showing the true reported wobble is useful context in its own right.
	if len(samples) > batteryRecentLimit {
		est.Recent = samples[len(samples)-batteryRecentLimit:]
	} else {
		est.Recent = samples
	}

	// Prefer voltage when the latest trustworthy reading has it and enough
	// samples carry it.
	withV := 0
	for _, sm := range samples {
		if sm.VoltageMV > 0 {
			withV++
		}
	}
	useVoltage := effLast.VoltageMV > 0 && withV >= 2
	if useVoltage {
		est.Basis = "voltage"
	}

	// Build the (time, y) series. In voltage mode only points that carry a
	// reading are used; y is the voltage-derived SoC. Otherwise y is the
	// firmware percentage. Despiked samples are excluded so a rail-sag
	// doesn't corrupt the slope or get misread as a recharge below.
	var pts []pt
	var t0 time.Time
	for i, sm := range samples {
		if !keep[i] {
			continue
		}
		if useVoltage && sm.VoltageMV <= 0 {
			continue // skip pre-voltage samples so the slope isn't mixed
		}
		if t0.IsZero() {
			t0 = sm.SampledAt
		}
		y := float64(sm.Percent)
		if useVoltage {
			y = voltageToSoC(sm.VoltageMV)
		}
		pts = append(pts, pt{x: sm.SampledAt.Sub(t0).Hours() / 24.0, y: y})
	}

	currentY := float64(effLast.Percent)
	if useVoltage {
		currentY = voltageToSoC(effLast.VoltageMV)
	}

	// Keep the full-window series before trimming: the current-run slope below
	// decides Trend (what's happening right now), but a confirmed "discharging"
	// verdict re-scans this full series for the worst historical run — see
	// robustDrainAcrossWindow.
	fullPts := pts

	// Trim to the most recent discharge run — see trimToCurrentSegment.
	segStart, activeCharge := trimToCurrentSegment(pts)
	pts = pts[segStart:]

	span := time.Duration(0)
	if len(pts) >= 2 {
		span = time.Duration((pts[len(pts)-1].x - pts[0].x) * float64(24*time.Hour))
	}
	// The minSpan gate exists to keep noisy discharging/stable readings from
	// flip-flopping near the flat threshold — it doesn't apply to an active
	// charge: getting here at all already required a confirmed chargeRise-or-
	// more SoC climb (see above), a far stronger signal than the day-to-day
	// percent/voltage jitter this gate guards against, and these charges often
	// finish in well under batteryMinSpan. Without this, a device charging for
	// (say) 2 hours would report "insufficient" for its entire charge instead
	// of "charging".
	if len(pts) < 2 || (!activeCharge && span < batteryMinSpan) {
		return est // not enough to read a trend yet; current values still shown
	}

	// Robust (Theil-Sen, not least-squares) slope of the current run — decides
	// Trend, i.e. what the device is doing right now.
	drain := -theilSenSlope(pts)
	switch {
	case drain > batteryFlatThreshold:
		est.Trend = "discharging"
		// Conservative by design: don't report the current run's own (possibly
		// unusually calm) rate — re-scan the whole trailing window for the
		// worst robustly-confirmed run and use that instead if it's worse. See
		// robustDrainAcrossWindow's doc comment for why.
		if worst, ok := robustDrainAcrossWindow(fullPts); ok && worst > drain {
			drain = worst
		}
		est.DrainPerDay = drain
		est.DaysRemaining = currentY / drain
	case drain < -batteryFlatThreshold:
		est.Trend = "charging"
		est.DrainPerDay = drain
	default:
		est.Trend = "stable"
		est.DrainPerDay = drain
	}

	// Optional: average discharge current, only computable when the device's
	// pack capacity is known. Purely a diagnostic extra on top of the
	// capacity-independent %/day trend above.
	if est.Trend == "discharging" && est.DrainPerDay > 0 {
		var device model.Device
		if err := s.db.Select("battery_capacity_mah").First(&device, deviceID).Error; err == nil && device.BatteryCapacityMAh > 0 {
			est.EstimatedCurrentMA = float64(device.BatteryCapacityMAh) * (est.DrainPerDay / 100.0) / 24.0
		}
	}
	return est
}

// batteryHistoryAggregateThreshold: below this range, return raw samples
// (already throttled to at most one per batterySampleMinInterval, so a few
// hundred points at most). Above it, History() aggregates to one point per
// calendar day so a multi-month/year request doesn't ship tens of thousands
// of raw points to the browser for a history chart.
const batteryHistoryAggregateThreshold = 3 * 24 * time.Hour

// History returns the battery level since the given time, for the
// Devices-list history-chart feature (day/month/year range picker). Unlike
// Estimate() (a fixed 14-day drain regression), the range here is caller-
// chosen and can span the full retention window.
func (s *BatteryService) History(deviceID uint, since time.Time) []model.BatterySample {
	if time.Since(since) <= batteryHistoryAggregateThreshold {
		var samples []model.BatterySample
		s.db.Where("device_id = ? AND sampled_at >= ?", deviceID, since).
			Order("sampled_at ASC").Find(&samples)
		return samples
	}

	type dayBucket struct {
		Day       string
		Percent   float64
		VoltageMV float64
	}
	var buckets []dayBucket
	s.db.Model(&model.BatterySample{}).
		Select("date(sampled_at) as day, AVG(percent) as percent, AVG(voltage_mv) as voltage_mv").
		Where("device_id = ? AND sampled_at >= ?", deviceID, since).
		Group("date(sampled_at)").
		Order("day ASC").
		Scan(&buckets)

	samples := make([]model.BatterySample, 0, len(buckets))
	for _, b := range buckets {
		day, err := time.Parse("2006-01-02", b.Day)
		if err != nil {
			continue
		}
		samples = append(samples, model.BatterySample{
			DeviceID:  deviceID,
			SampledAt: day,
			Percent:   int(math.Round(b.Percent)),
			VoltageMV: int(math.Round(b.VoltageMV)),
		})
	}
	return samples
}
