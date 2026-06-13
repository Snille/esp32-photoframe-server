-- Remember the client IP a frame last checked in from, so the Home Assistant
-- MQTT bridge can expose it as a diagnostic sensor alongside the hostname.
ALTER TABLE devices ADD COLUMN last_ip TEXT DEFAULT '';
