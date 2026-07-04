package service

import (
	"encoding/csv"
	"io"
	"log"
	"strconv"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"gorm.io/gorm"
)

// DeviceLogParams is everything ServeImage can observe about one pull attempt.
// A struct rather than a long positional argument list since this has grown
// to capture every signal a frame can report (see ServeImage) — both for
// troubleshooting (cross-check a bad reading against voltage/status/reset
// cause) and so there's enough history to chart over time later.
type DeviceLogParams struct {
	DeviceID        uint
	StatusCode      int
	TriggerReason   string
	Source          string
	ImageIDs        []uint
	BatteryPercent  int
	VoltageMV       int
	BatteryStatus   string
	FirmwareVersion string
	ResetReason     string
	IP              string
	DisplayWidth    int
	DisplayHeight   int
}

// RecordDeviceLog persists one frame check-in attempt (success or failure)
// and prunes entries older than the device's configured retention window.
// Meant to run off the request path (see safego.Go in ServeImage) so it never
// delays or fails the image response.
func RecordDeviceLog(db *gorm.DB, p DeviceLogParams) {
	entry := model.DeviceLog{
		DeviceID:        p.DeviceID,
		Timestamp:       time.Now(),
		Success:         p.StatusCode >= 200 && p.StatusCode < 300,
		StatusCode:      p.StatusCode,
		TriggerReason:   p.TriggerReason,
		Source:          p.Source,
		BatteryPercent:  p.BatteryPercent,
		VoltageMV:       p.VoltageMV,
		BatteryStatus:   p.BatteryStatus,
		FirmwareVersion: p.FirmwareVersion,
		ResetReason:     p.ResetReason,
		IP:              p.IP,
		DisplayWidth:    p.DisplayWidth,
		DisplayHeight:   p.DisplayHeight,
	}
	if len(p.ImageIDs) > 0 {
		entry.ImageID = p.ImageIDs[len(p.ImageIDs)-1]
	}
	if err := db.Create(&entry).Error; err != nil {
		log.Printf("Failed to write device log for device %d: %v", p.DeviceID, err)
		return
	}

	var device model.Device
	if err := db.Select("log_retention_value", "log_retention_unit").First(&device, p.DeviceID).Error; err != nil {
		return
	}
	cutoff := RetentionCutoff(device.LogRetentionValue, device.LogRetentionUnit)
	if err := db.Where("device_id = ? AND timestamp < ?", p.DeviceID, cutoff).Delete(&model.DeviceLog{}).Error; err != nil {
		log.Printf("Failed to prune device log for device %d: %v", p.DeviceID, err)
	}
}

// RetentionCutoff computes the oldest timestamp to keep, given a retention
// value + unit (e.g. 6 "months"). Falls back to the 6-month default for an
// unset/invalid value or unit.
func RetentionCutoff(value int, unit string) time.Time {
	if value <= 0 {
		value = 6
		unit = "months"
	}
	now := time.Now()
	switch unit {
	case "days":
		return now.AddDate(0, 0, -value)
	case "years":
		return now.AddDate(-value, 0, 0)
	default: // "months" and any unrecognized unit
		return now.AddDate(0, -value, 0)
	}
}

// ListDeviceLogs returns one device's activity log, newest first.
func ListDeviceLogs(db *gorm.DB, deviceID uint, limit, offset int) ([]model.DeviceLog, int64, error) {
	var total int64
	if err := db.Model(&model.DeviceLog{}).Where("device_id = ?", deviceID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []model.DeviceLog
	err := db.Where("device_id = ?", deviceID).
		Order("timestamp desc").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error
	return logs, total, err
}

// WriteDeviceLogsCSV streams one device's full retained activity log as CSV
// (oldest first, natural reading order for a downloaded log file).
func WriteDeviceLogsCSV(db *gorm.DB, deviceID uint, w io.Writer) error {
	var logs []model.DeviceLog
	if err := db.Where("device_id = ?", deviceID).Order("timestamp asc").Find(&logs).Error; err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"timestamp", "success", "status_code", "trigger", "source", "image_id",
		"battery_percent", "voltage_mv", "battery_status", "firmware_version",
		"reset_reason", "ip", "display_width", "display_height",
	}); err != nil {
		return err
	}
	for _, l := range logs {
		if err := cw.Write([]string{
			l.Timestamp.Format(time.RFC3339),
			strconv.FormatBool(l.Success),
			strconv.Itoa(l.StatusCode),
			l.TriggerReason,
			l.Source,
			strconv.FormatUint(uint64(l.ImageID), 10),
			strconv.Itoa(l.BatteryPercent),
			strconv.Itoa(l.VoltageMV),
			l.BatteryStatus,
			l.FirmwareVersion,
			l.ResetReason,
			l.IP,
			strconv.Itoa(l.DisplayWidth),
			strconv.Itoa(l.DisplayHeight),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
