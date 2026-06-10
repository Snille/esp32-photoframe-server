-- Per-device Immich album selection. immich_album_ids is a comma-separated list
-- of Immich album UUIDs; empty means "all Immich photos" (back-compatible).
ALTER TABLE devices ADD COLUMN immich_album_ids TEXT NOT NULL DEFAULT '';

-- Album membership for synced Immich assets. An asset can belong to several
-- albums, so this is a join table keyed on (image_id, immich_album_id).
CREATE TABLE immich_image_albums (
    image_id        INTEGER NOT NULL,
    immich_album_id TEXT    NOT NULL,
    PRIMARY KEY (image_id, immich_album_id)
);
CREATE INDEX idx_immich_image_albums_album ON immich_image_albums (immich_album_id);
