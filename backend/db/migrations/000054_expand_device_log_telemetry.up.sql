-- Expand the per-pull activity log to every signal a frame can report on a
-- fetch, not just battery percent — useful for diagnosing hardware issues
-- (e.g. correlating a bad battery reading with voltage/status/reset-cause)
-- and for building stats graphs over time later.
ALTER TABLE device_logs ADD COLUMN voltage_mv INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device_logs ADD COLUMN battery_status TEXT NOT NULL DEFAULT '';
ALTER TABLE device_logs ADD COLUMN firmware_version TEXT NOT NULL DEFAULT '';
ALTER TABLE device_logs ADD COLUMN reset_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE device_logs ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE device_logs ADD COLUMN display_width INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device_logs ADD COLUMN display_height INTEGER NOT NULL DEFAULT 0;
