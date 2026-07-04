DROP TABLE IF EXISTS device_logs;
ALTER TABLE devices DROP COLUMN log_retention_value;
ALTER TABLE devices DROP COLUMN log_retention_unit;
