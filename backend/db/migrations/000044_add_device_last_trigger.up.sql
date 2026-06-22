-- Records what triggered the frame's most recent image change: a timer wake
-- (auto-rotate), a wake-button press, a cold boot, a server push, or a plain
-- pull from firmware too old to report a wake reason. Surfaced as the Home
-- Assistant "Last Trigger" sensor.
ALTER TABLE devices ADD COLUMN last_trigger TEXT DEFAULT '';
