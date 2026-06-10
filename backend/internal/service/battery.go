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
}

// Estimate regresses percent over time across the trailing window.
func (s *BatteryService) Estimate(deviceID uint) BatteryEstimate {
	est := BatteryEstimate{Trend: "insufficient", DaysRemaining: -1, Recent: []model.BatterySample{}}

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
	est.WindowStart = samples[0].SampledAt
	est.LastSampledAt = last.SampledAt

	// Sparkline: cap to the most recent points.
	if len(samples) > batteryRecentLimit {
		est.Recent = samples[len(samples)-batteryRecentLimit:]
	} else {
		est.Recent = samples
	}

	span := last.SampledAt.Sub(samples[0].SampledAt)
	if len(samples) < 2 || span < batteryMinSpan {
		return est // not enough to read a trend yet; current % still shown
	}

	// Least-squares slope of percent vs. elapsed days, ref = first sample.
	t0 := samples[0].SampledAt
	var n, sumX, sumY, sumXY, sumXX float64
	for _, sm := range samples {
		x := sm.SampledAt.Sub(t0).Hours() / 24.0
		y := float64(sm.Percent)
		n++
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return est
	}
	slope := (n*sumXY - sumX*sumY) / denom // %/day, negative when discharging

	drain := -slope
	est.DrainPerDay = drain
	switch {
	case drain > batteryFlatThreshold:
		est.Trend = "discharging"
		if drain > 0 {
			est.DaysRemaining = float64(last.Percent) / drain
		}
	case drain < -batteryFlatThreshold:
		est.Trend = "charging"
	default:
		est.Trend = "stable"
	}
	return est
}
