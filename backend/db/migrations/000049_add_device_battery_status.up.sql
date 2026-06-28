-- Coarse charge status the frame reports on each pull via the X-Battery-Status
-- header: "charging", "full" or "on_battery". Only boards that can actually
-- sense it (USB + voltage heuristic, or a real charge line) send it; others
-- leave this empty and the Home Assistant "Battery Status" sensor is omitted.
ALTER TABLE devices ADD COLUMN battery_status TEXT DEFAULT '';
