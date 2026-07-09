-- Two-tier on-screen low-battery "charge me" warning, rendered into the served
-- image from the battery % the frame reports. Tier 1 = a chip at a chosen
-- position once battery drops to the low threshold; tier 2 = a large centred
-- banner once it drops to the critical threshold. Thresholds, texts and
-- positions are all user-configurable per device. Disabled by default.
ALTER TABLE devices ADD COLUMN low_battery_warn_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN low_battery_warn_percent INTEGER NOT NULL DEFAULT 25;
ALTER TABLE devices ADD COLUMN low_battery_warn_text TEXT NOT NULL DEFAULT 'Time to charge me soon';
ALTER TABLE devices ADD COLUMN low_battery_warn_position TEXT NOT NULL DEFAULT 'top-center';
ALTER TABLE devices ADD COLUMN critical_battery_warn_percent INTEGER NOT NULL DEFAULT 10;
ALTER TABLE devices ADD COLUMN critical_battery_warn_text TEXT NOT NULL DEFAULT 'Charge me now!';
ALTER TABLE devices ADD COLUMN critical_battery_warn_position TEXT NOT NULL DEFAULT 'center';
