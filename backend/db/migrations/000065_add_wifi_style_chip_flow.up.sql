-- How the Wi-Fi chip is drawn: "both" (icon + dBm), "icon" (signal-bar icon
-- only) or "text" (dBm only) — mirrors battery_style.
ALTER TABLE devices ADD COLUMN wifi_style TEXT NOT NULL DEFAULT 'both';
-- How chips that share one overlay position are arranged: "stack" (one under
-- the other, the original behaviour) or "row" (side by side), e.g. a battery
-- icon and a Wi-Fi icon next to each other in the same corner.
ALTER TABLE devices ADD COLUMN overlay_chip_flow TEXT NOT NULL DEFAULT 'stack';
