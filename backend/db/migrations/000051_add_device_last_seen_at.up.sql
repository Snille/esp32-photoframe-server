-- When the frame last checked in (pulled an image). Updated on every real pull
-- so the Devices list can show a "Last check-in" time.
ALTER TABLE devices ADD COLUMN last_seen_at DATETIME;
