-- The frame reports exactly when it intends to wake next (X-Next-Wake-Time, a
-- unix epoch) on each check-in. Storing it lets the Home Assistant "Next Image
-- Pull" sensor show the frame's real scheduled wake — which already accounts for
-- clock-aligned wakes and the quiet-hours sleep schedule — instead of the server
-- re-deriving (and drifting from) it. 0 = unknown / firmware too old to report.
ALTER TABLE devices ADD COLUMN next_wake_at INTEGER DEFAULT 0;
