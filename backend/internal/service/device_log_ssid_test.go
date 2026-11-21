package service

import (
	"bytes"
	"encoding/csv"
	"path/filepath"
	"testing"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// migratedTestDB opens a fresh SQLite file and applies the real SQL migrations,
// so a test sees the production schema (not a gorm AutoMigrate guess).
func migratedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	driver, err := sqlite3.WithInstance(sqlDB, &sqlite3.Config{})
	require.NoError(t, err)
	dir, err := filepath.Abs("../../db/migrations")
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://"+filepath.ToSlash(dir), "sqlite3", driver)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	return db
}

// The SSID a frame reports must land in its Activity Log and the CSV export,
// and a pull from older firmware (no X-Wifi-SSID) must still be logged, with
// an empty network name.
func TestDeviceLogRecordsWifiSSID(t *testing.T) {
	db := migratedTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO devices (id, name, host) VALUES (1, 'Mini', 'mini.local')").Error)

	RecordDeviceLog(db, DeviceLogParams{DeviceID: 1, StatusCode: 200, WifiRSSI: -60, WifiSSID: "Snille IOT"})
	RecordDeviceLog(db, DeviceLogParams{DeviceID: 1, StatusCode: 200, WifiRSSI: -61}) // old firmware

	logs, total, err := ListDeviceLogs(db, 1, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	got := map[string]bool{}
	for _, l := range logs {
		got[l.WifiSSID] = true
	}
	require.True(t, got["Snille IOT"], "SSID not stored: %+v", logs)
	require.True(t, got[""], "old-firmware pull not stored: %+v", logs)

	var buf bytes.Buffer
	require.NoError(t, WriteDeviceLogsCSV(db, 1, &buf))
	rows, err := csv.NewReader(&buf).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	col := -1
	for i, h := range rows[0] {
		if h == "wifi_ssid" {
			col = i
		}
	}
	require.NotEqual(t, -1, col, "CSV header lacks wifi_ssid: %v", rows[0])
	require.Equal(t, "Snille IOT", rows[1][col])
	require.Equal(t, "", rows[2][col])

	// The latest SSID also lives on the device row (Devices list, HA sensor).
	require.NoError(t, db.Model(&model.Device{}).Where("id = 1").Update("wifi_ssid", "Snille IOT").Error)
	var d model.Device
	require.NoError(t, db.First(&d, 1).Error)
	require.Equal(t, "Snille IOT", d.WifiSSID)
}
