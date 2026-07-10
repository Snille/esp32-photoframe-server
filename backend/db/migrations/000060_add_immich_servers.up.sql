-- Multi-Immich-server support: a frame can show photos from selected albums
-- across several Immich servers (e.g. the user's own + a family member's
-- separate instance). Immich album-sharing is within-instance only, so a second
-- physical server is genuinely needed. Each server is reached via its own API
-- key (typically a dedicated "photoframes" user that only sees shared albums).
CREATE TABLE immich_servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    label TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME
);

-- Seed server 1 from the existing single-server settings so the current library
-- keeps working untouched. The old immich_url / immich_api_key settings keys are
-- left in place as a read fallback (not removed) to avoid a breaking change.
INSERT INTO immich_servers (id, label, url, api_key, enabled, created_at)
SELECT 1, 'Default',
    COALESCE((SELECT value FROM settings WHERE key = 'immich_url'), ''),
    COALESCE((SELECT value FROM settings WHERE key = 'immich_api_key'), ''),
    1, CURRENT_TIMESTAMP;

-- Track which server each Immich asset / album membership / cached album name
-- came from: Immich pixels are fetched on-demand at serve time, so the serve and
-- thumbnail paths must hit the right server with the right key. Album/asset UUIDs
-- are globally unique per instance, so no namespacing of the device's selected
-- album IDs is needed — the server id only routes the pixel fetch and groups the
-- picker. Back-fill existing Immich rows to server 1.
ALTER TABLE images ADD COLUMN immich_server_id INTEGER NOT NULL DEFAULT 0;
UPDATE images SET immich_server_id = 1 WHERE source = 'immich';

ALTER TABLE immich_image_albums ADD COLUMN immich_server_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE immich_albums ADD COLUMN immich_server_id INTEGER NOT NULL DEFAULT 1;
