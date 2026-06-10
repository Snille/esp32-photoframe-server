-- Per-device, per-chip icon visibility. Comma-separated list of overlay element
-- keys whose leading icon should be HIDDEN (photo_date, weather, names,
-- location, description). Empty = all icons shown (previous behaviour).
ALTER TABLE devices ADD COLUMN overlay_hidden_icons TEXT NOT NULL DEFAULT '';
