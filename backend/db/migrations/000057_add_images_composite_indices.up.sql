-- Two composite indexes matching our actual hot-path rotation queries
-- (photosource_helpers.go):
--   PickRandomDBPhoto:      WHERE source = ? AND hidden = 0 [AND orientation IN (...)]
--   orderedCandidateIDs:    WHERE source = ? AND hidden = 0 ORDER BY COALESCE(photo_taken_at, created_at)
-- The existing idx_images_source (migration 000021) only covers the leading
-- source filter; these extend it to also serve the orientation filter and
-- the chronological display-order modes without a secondary scan. Purely
-- additive -- no existing query changes shape.
CREATE INDEX IF NOT EXISTS idx_images_source_orientation ON images(source, orientation);
CREATE INDEX IF NOT EXISTS idx_images_source_created_at ON images(source, created_at);
