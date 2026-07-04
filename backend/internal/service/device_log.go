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

// RecordDeviceLog persists one frame check-in attempt (success or failure)
// and prunes entries older than the device's configured retention window.
// Meant to run off the request path (see safego.Go in ServeImage) so it never
// delays or fails the image response.
func RecordDeviceLog(db *gorm.DB, deviceID uint, statusCode int, triggerReason, source string, imageIDs []uint, batteryPercent int) {
	entry := model.DeviceLog{
		DeviceID:       deviceID,
		Timestamp:      time.Now(),
		Success:        statusCode >= 200 && statusCode < 300,
		StatusCode:     statusCode,
		TriggerReason:  triggerReason,
		Source:         source,
		BatteryPercent: batteryPercent,
	}
	if len(imageIDs) > 0 {
		entry.ImageID = imageIDs[len(imageIDs)-1]
	}
	if err := db.Create(&entry).Error; err != nil {
		log.Printf("Failed to write device log for device %d: %v", deviceID, err)
		return
	}

	var device model.Device
	if err := db.Select("log_retention_value", "log_retention_unit").First(&device, deviceID).Error; err != nil {
		return
	}
	cutoff := RetentionCutoff(device.LogRetentionValue, device.LogRetentionUnit)
	if err := db.Where("device_id = ? AND timestamp < ?", deviceID, cutoff).Delete(&model.DeviceLog{}).Error; err != nil {
		log.Printf("Failed to prune device log for device %d: %v", deviceID, err)
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
	if err := cw.Write([]string{"timestamp", "success", "status_code", "trigger", "source", "image_id", "battery_percent"}); err != nil {
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
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
