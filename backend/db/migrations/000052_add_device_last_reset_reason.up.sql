-- The frame's last reset cause (X-Reset-Reason on pull): poweron / deepsleep /
-- sw / task_wdt / int_wdt / wdt / panic / brownout. Lets the Devices list flag a
-- frame that's been crash-looping (watchdog/panic/brownout) after it recovers.
ALTER TABLE devices ADD COLUMN last_reset_reason TEXT DEFAULT '';
