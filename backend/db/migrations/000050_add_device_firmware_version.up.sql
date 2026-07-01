-- The frame's reported firmware version, refreshed on every pull from the
-- X-Firmware-Version header (and on a hardware refresh from SystemInfo.Version).
-- Surfaced in the Devices list so you can see what each frame is running.
ALTER TABLE devices ADD COLUMN firmware_version TEXT DEFAULT '';
