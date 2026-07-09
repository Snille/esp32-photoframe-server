-- Server-controlled OTA auto-update. Unlike display_rotation_deg (frame-owned,
-- mirrored here), auto_update is server-owned and pushed to the frame via the
-- config-sync payload: when on, the frame's daily OTA check self-installs a
-- found update. Default off so each frame stays a manual canary until turned on.
-- auto_update_battery_min is the on-battery charge floor the frame requires
-- before it will auto-install (the gate is enforced on-device); 30 % default,
-- clamped 10-90.
ALTER TABLE devices ADD COLUMN auto_update INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN auto_update_battery_min INTEGER NOT NULL DEFAULT 30;
