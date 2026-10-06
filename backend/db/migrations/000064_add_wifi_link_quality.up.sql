-- Wi-Fi link quality the frame reports on each pull: X-Wifi-RSSI (signal of the
-- connected AP in dBm, always negative) and X-Wifi-Kbps (body throughput of the
-- previous image download). 0 means "not reported" for both, since a real RSSI
-- is never 0 and a real download is never 0 kbit/s.
--
-- The devices columns hold the latest value (Devices list, HA sensors, the
-- on-photo chip); the device_logs columns keep the history, so moving a frame
-- to a weak spot shows up in its Activity Log.
ALTER TABLE devices ADD COLUMN wifi_rssi INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN wifi_kbps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN show_wifi BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN wifi_position TEXT NOT NULL DEFAULT 'top-left';
ALTER TABLE device_logs ADD COLUMN wifi_rssi INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device_logs ADD COLUMN wifi_kbps INTEGER NOT NULL DEFAULT 0;
