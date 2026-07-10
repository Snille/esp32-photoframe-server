ALTER TABLE immich_albums DROP COLUMN immich_server_id;
ALTER TABLE immich_image_albums DROP COLUMN immich_server_id;
ALTER TABLE images DROP COLUMN immich_server_id;
DROP TABLE immich_servers;
