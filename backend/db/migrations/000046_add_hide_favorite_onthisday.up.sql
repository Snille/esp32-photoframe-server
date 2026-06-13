-- Hide / favorite a photo (global flags on the image) + per-device rotation-pool
-- toggles: "on this day" (only photos taken on today's month/day, any year) and
-- "favorites only".
ALTER TABLE images ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE images ADD COLUMN favorite BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN on_this_day BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN favorites_only BOOLEAN NOT NULL DEFAULT 0;
