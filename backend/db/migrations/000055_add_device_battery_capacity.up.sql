-- Optional per-device battery capacity (mAh). Not required for the existing
-- %/day drain estimate (which is already capacity-independent), but when set
-- it lets the server also report an estimated average discharge current in
-- mA -- a diagnostic number that can't be derived without knowing pack size.
-- 0 = not set.
ALTER TABLE devices ADD COLUMN battery_capacity_mah INTEGER NOT NULL DEFAULT 0;
