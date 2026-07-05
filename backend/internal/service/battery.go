package service

import (
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
	// Drop ancient samples so the table stays small.
	batteryRetention = 90 * 24 * time.Hour
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
	DrainPerDay      float64               `json:"drain_per_day"`  // %/day, positive = discharging
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
	type pt struct{ x, y float64 }
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

	// Trim to the most recent discharge run. A mid-window recharge (the user
	// tops the battery up over USB) otherwise poisons the least-squares slope:
	// averaging the pre-charge decline, the charge jump, and the post-charge
	// decline yields a near-flat or rising line, so a plainly-draining frame
	// reads "stable"/"charging". Only the rate since the last charge predicts
	// the runtime left. We find the last charge by tracking the recent trough;
	// once SoC climbs chargeRise above it we're charging, then follow the rise
	// to its peak (where charging ends) and start the run there.
	if len(pts) >= 2 {
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
			// Window ends mid-charge; start the run at the latest peak so we
			// don't regress over the still-rising tail.
			segStart = peakIdx
		}
		pts = pts[segStart:]
	}

	span := time.Duration(0)
	if len(pts) >= 2 {
		span = time.Duration((pts[len(pts)-1].x - pts[0].x) * float64(24*time.Hour))
	}
	if len(pts) < 2 || span < batteryMinSpan {
		return est // not enough to read a trend yet; current values still shown
	}

	// Least-squares slope of SoC vs. elapsed days.
	var n, sumX, sumY, sumXY, sumXX float64
	for _, p := range pts {
		n++
		sumX += p.x
		sumY += p.y
		sumXY += p.x * p.y
		sumXX += p.x * p.x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return est
	}
	slope := (n*sumXY - sumX*sumY) / denom // SoC %/day, negative when discharging

	drain := -slope
	est.DrainPerDay = drain
	switch {
	case drain > batteryFlatThreshold:
		est.Trend = "discharging"
		if drain > 0 {
			est.DaysRemaining = currentY / drain
		}
	case drain < -batteryFlatThreshold:
		est.Trend = "charging"
	default:
		est.Trend = "stable"
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
