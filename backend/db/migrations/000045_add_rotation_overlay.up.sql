-- Rotation-position overlay element (per-device placement + show-total toggle).
ALTER TABLE devices ADD COLUMN show_rotation BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN rotation_position TEXT NOT NULL DEFAULT 'bottom-right';
ALTER TABLE devices ADD COLUMN rotation_show_total BOOLEAN NOT NULL DEFAULT 1;

-- Cache of Immich album names keyed by album UUID, so the Home Assistant
-- "Immich Albums" sensor can resolve a frame's selected album IDs to names
-- without hitting the Immich API on every publish. Refreshed whenever the
-- album list is fetched (album picker / import).
CREATE TABLE IF NOT EXISTS immich_albums (
    immich_album_id TEXT PRIMARY KEY,
    album_name      TEXT NOT NULL DEFAULT ''
);
