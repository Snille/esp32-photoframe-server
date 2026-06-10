CREATE TABLE battery_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id INTEGER NOT NULL,
    sampled_at DATETIME NOT NULL,
    percent INTEGER NOT NULL,
    voltage_mv INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_battery_samples_device_time ON battery_samples (device_id, sampled_at);
