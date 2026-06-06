ALTER TABLE devices ADD COLUMN date_position TEXT NOT NULL DEFAULT 'bottom-left';
ALTER TABLE devices ADD COLUMN photo_date_position TEXT NOT NULL DEFAULT 'bottom-left';
ALTER TABLE devices ADD COLUMN weather_position TEXT NOT NULL DEFAULT 'bottom-right';
ALTER TABLE devices ADD COLUMN battery_position TEXT NOT NULL DEFAULT 'top-right';
ALTER TABLE devices ADD COLUMN battery_style TEXT NOT NULL DEFAULT 'both';
