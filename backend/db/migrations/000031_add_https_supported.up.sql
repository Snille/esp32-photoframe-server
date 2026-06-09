-- Per-device HTTPS capability, mirrored from the device's system-info
-- https_supported flag. false on no-PSRAM boards (e.g. FireBeetle) that can't
-- fit a TLS handshake alongside the 120KB framebuffer. Existing devices default
-- to 1 (supported) so nothing is falsely flagged until the next refresh/sync.
ALTER TABLE devices ADD COLUMN https_supported BOOLEAN NOT NULL DEFAULT 1;
