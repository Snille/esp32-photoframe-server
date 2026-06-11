-- Track the id of the most recent served-image thumbnail per device so the
-- Devices list can show a miniature of what's currently on each frame. The id
-- is the unix-nano filename stem of thumb_<id>.jpg, served publicly via
-- /served-image-thumbnail/:id.
ALTER TABLE devices ADD COLUMN current_thumb_id TEXT DEFAULT '';
