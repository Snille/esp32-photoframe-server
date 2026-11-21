-- Name (SSID) of the Wi-Fi network the frame was on, from X-Wifi-SSID
-- (firmware >= 2.21.0). Empty = not reported (older firmware).
--
-- The devices column holds the latest value (Devices list, HA sensor); the
-- device_logs column keeps it per pull, so the Activity Log shows which network
-- a frame was last seen on even after it goes silent.
ALTER TABLE devices ADD COLUMN wifi_ssid TEXT NOT NULL DEFAULT '';
ALTER TABLE device_logs ADD COLUMN wifi_ssid TEXT NOT NULL DEFAULT '';
