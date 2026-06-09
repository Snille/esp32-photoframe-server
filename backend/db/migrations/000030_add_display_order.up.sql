-- Per-device image display order. Existing devices default to 'shuffle'
-- (random, each photo once per cycle), which supersedes the old random+history
-- behaviour. shuffle_seed is the server-managed seed for the current shuffle
-- cycle; it is bumped each time a device finishes a full pass.
ALTER TABLE devices ADD COLUMN display_order TEXT NOT NULL DEFAULT 'shuffle';
ALTER TABLE devices ADD COLUMN shuffle_seed INTEGER NOT NULL DEFAULT 0;

-- Per-image manual sort position, used only by devices in 'custom' order mode.
-- Lower = shown earlier; ties broken by id.
ALTER TABLE images ADD COLUMN display_order INTEGER NOT NULL DEFAULT 0;
