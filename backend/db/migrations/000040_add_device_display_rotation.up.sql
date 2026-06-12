-- Display Rotation is now the single source of truth for how a frame is
-- mounted relative to the panel's native orientation. The render pipeline and
-- all previews derive orientation from display_rotation_deg (0/90/180/270),
-- with native panel dimensions as the baseline. Backfill from the legacy
-- landscape/portrait orientation so existing frames keep their current look:
-- a landscape-mounted portrait-native panel is a 90° rotation.
ALTER TABLE devices ADD COLUMN display_rotation_deg INTEGER DEFAULT 0;
UPDATE devices SET display_rotation_deg = CASE WHEN orientation = 'landscape' THEN 90 ELSE 0 END;
