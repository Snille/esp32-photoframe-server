-- Mirrors which GPIO (if any) the frame is using for an external battery
-- voltage divider, on boards with no built-in battery ADC (e.g. FireBeetle 2
-- ESP32-S3). Selected on the frame's own local WebGUI, reported here via
-- X-Battery-ADC-Pin on each check-in -- read-only from the server's side.
-- -1 = not configured / not applicable.
ALTER TABLE devices ADD COLUMN battery_adc_gpio INTEGER NOT NULL DEFAULT -1;
