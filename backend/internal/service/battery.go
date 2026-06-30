package service

import (
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
// Preference order: voltage-derived SoC when the current voltage is plausible →
// this device's most recent plausible sample → the frame's raw percent → -1
// (no usable level, caller hides the badge). deviceID 0 skips the DB fallback
// (e.g. preview / unknown device).
func (s *BatteryService) RobustBadgePercent(deviceID uint, percent, voltageMV int) int {
	if PlausibleVoltage(voltageMV) {
		return VoltageToSoC(voltageMV)
	}
	if deviceID != 0 {
		if p := s.lastPlausiblePercent(deviceID); p >= 0 {
			return p
		}
	}
	if percent >= 0 && percent <= 100 {
		return percent
	}
	return -1
}

// lastPlausiblePercent returns the voltage-derived SoC of this device's most
// recent battery sample that carried a plausible voltage, or -1 if none of the
// recent samples qualify. Used to ride out a single collapsed reading without
// flashing a bogus near-empty badge.
func (s *BatteryService) lastPlausiblePercent(deviceID uint) int {
	var samples []model.BatterySample
	if err := s.db.Where("device_id = ?", deviceID).
		Order("sampled_at DESC").Limit(20).Find(&samples).Error; err != nil {
		return -1
	}
	for _, sm := range samples {
		if PlausibleVoltage(sm.VoltageMV) {
			return VoltageToSoC(sm.VoltageMV)
		}
	}
	return -1
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
	est.CurrentPercent = last.Percent
	est.CurrentVoltageMV = last.VoltageMV
	// Prefer the voltage-derived level for the reported "current %" when the frame
	// sends a voltage: the firmware percent is coarse and, on some boards (XIAO
	// EE02 on USB), erratic — a healthy 4.1 V can read 0%. This feeds the Devices
	// list and the HA battery sensor, so they stay truthful like the on-photo badge.
	// Flag an implausible reading (EE02 on USB) BEFORE overwriting CurrentPercent
	// with the voltage-derived value, so the raw firmware percent can be compared
	// against the voltage.
	est.Plugged = BatteryReadingImplausible(last.Percent, last.VoltageMV)
	if last.VoltageMV > 0 {
		est.CurrentPercent = VoltageToSoC(last.VoltageMV)
	}
	est.WindowStart = samples[0].SampledAt
	est.LastSampledAt = last.SampledAt

	// Sparkline: cap to the most recent points.
	if len(samples) > batteryRecentLimit {
		est.Recent = samples[len(samples)-batteryRecentLimit:]
	} else {
		est.Recent = samples
	}

	// Prefer voltage when the latest reading has it and enough samples carry it.
	withV := 0
	for _, sm := range samples {
		if sm.VoltageMV > 0 {
			withV++
		}
	}
	useVoltage := last.VoltageMV > 0 && withV >= 2
	if useVoltage {
		est.Basis = "voltage"
	}

	// Build the (time, y) series. In voltage mode only points that carry a
	// reading are used; y is the voltage-derived SoC. Otherwise y is the
	// firmware percentage.
	type pt struct{ x, y float64 }
	var pts []pt
	var t0 time.Time
	for _, sm := range samples {
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

	currentY := float64(last.Percent)
	if useVoltage {
		currentY = voltageToSoC(last.VoltageMV)
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
	return est
}
