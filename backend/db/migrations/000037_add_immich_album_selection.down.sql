DROP INDEX IF EXISTS idx_immich_image_albums_album;
DROP TABLE IF EXISTS immich_image_albums;
ALTER TABLE devices DROP COLUMN immich_album_ids;
