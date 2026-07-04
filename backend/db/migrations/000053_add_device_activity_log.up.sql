-- Per-device activity log: unlike device_histories (which only records a
-- successful image serve), this captures every pull attempt the frame makes,
-- success or failure, so a stalled frame's actual behavior is visible instead
-- of just silence. Pruned to each device's configured retention window.
CREATE TABLE device_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id INTEGER NOT NULL,
    timestamp DATETIME NOT NULL,
    success BOOLEAN NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL DEFAULT 0,
    trigger_reason TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    image_id INTEGER NOT NULL DEFAULT 0,
    battery_percent INTEGER NOT NULL DEFAULT -1
);
CREATE INDEX idx_device_logs_device_time ON device_logs (device_id, timestamp);

-- How long this device's activity log is kept before the oldest entries are
-- pruned. unit is one of "days" | "months" | "years". Default 6 months.
ALTER TABLE devices ADD COLUMN log_retention_value INTEGER DEFAULT 6;
ALTER TABLE devices ADD COLUMN log_retention_unit TEXT DEFAULT 'months';
