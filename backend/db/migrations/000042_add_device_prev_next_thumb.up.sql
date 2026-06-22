-- Track the previous and next image thumbnails alongside the current one so the
-- Home Assistant MQTT bridge can expose Previous / Current / Next image entities
-- per frame. prev_thumb_id is the image that was on the frame before the current
-- one; next_thumb_id is a non-mutating preview render of what the next pull will
-- show (ordered DB-backed sources only).
ALTER TABLE devices ADD COLUMN prev_thumb_id TEXT DEFAULT '';
ALTER TABLE devices ADD COLUMN next_thumb_id TEXT DEFAULT '';
