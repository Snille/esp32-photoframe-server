# Changelog

## v1.11.1

### Fixed
- **Wide overlay bands hug their text**: the two full-width band placements (`wide-top` / `wide-bottom`) no longer draw a full-width background bar when the field is short. The band stays centered but its chip now grows from the centre to fit its content, only reaching full width (then wrapping) for long text.

## v1.11.0

### Added
- **Overlay settings moved to their own device-dialog tab** (out from under Auto Rotate), with a live preview that mirrors the renderer.
- **Per-device overlay font and weight**: pick one of five e-paper-legible families and Regular / Medium / Bold for all overlay chips. All chips now share one uniform look (font, size, weight, opacity).
- **People-names overlay** from Immich face metadata: placeable chip with six name formats (Förnamn Efternamn / Förnamn E. / Förnamn / Efternamn Förnamn / Efternamn F. / Efternamn), an optional age (computed at the photo's date) in parentheses, comma-separated names, and a max-length cap that keeps whole names then collapses the rest to "+N".
- **Location overlay** from Immich EXIF (city / state / country), with its own length cap.
- **Description overlay** from Immich EXIF description (gallery uploads fall back to their caption), with its own length cap.
- **Two full-width band placements** (`wide-top` / `wide-bottom`) suited to long fields, in addition to the six corner slots. When no corner chip is present the band collapses into the corner row's position.
- **Battery drain estimate** (no external hardware): the level the frame reports on each image fetch (`X-Battery-Percentage`, plus optional `X-Battery-Voltage`) is sampled per wake and regressed over a 14-day window into a %/day drain and an estimated runtime, shown with a sparkline in the device dialog's Power tab. New `GET /api/devices/:id/battery`.

### Fixed
- All overlay fields now render with identical typography (previously the date and weather chips could differ in size).
- The "From Google Photos" junk caption is no longer written as a description.

### Database
- Migrations `000032`–`000035`: overlay font/weight; people + location; location length; description.
- Migration `000036`: `battery_samples` table (per-device timestamped level/voltage).

## v1.10.0

### Added
- **HTTPS capability guard for no-PSRAM frames**: the device dialog now warns and blocks Save when an `https://` image URL is chosen for a board that can't do HTTPS (e.g. the FireBeetle — its TLS handshake won't fit in RAM alongside the framebuffer). Capability is read from the device's `system-info` `https_supported` flag, persisted per device and refreshed on add / sync. Existing and remote devices default to "supported" so nothing is falsely flagged.

### Fixed
- **Pushed images now render the chosen overlays**: a server-initiated push (Push to Device) previously skipped the date / battery / placement overlays that the pull path renders. The push path now applies the full overlay set — battery (with level fetched live from the device's `/api/battery`, since a push has no `X-Battery-Percentage` header), per-element placement, styles and scale.
- **Photo-date overlay on push**: the original capture date now shows on pushed images (threaded from the image record), not just on pulls.
- **"Sync from Device" no longer drops settings**: saving a device's config previously rebuilt it from a fixed field list, silently dropping device settings the UI doesn't manage (custom HTTP header, SD rotation mode, Wi-Fi SSID, AI prompt) and any newly-added firmware field. Config saves now merge onto the device's last-synced config, preserving every untouched field.
- **Today's-date overlay font size** now matches the other overlay elements (was rendered larger).

### Database
- Migration `000031`: `devices.https_supported` (default true).

## v1.9.0

### Added
- **Per-device display order**: choose how each frame sequences its photos from DB-backed sources (gallery, Immich, Synology, Google Photos) — `shuffle` (each photo once per cycle, then reshuffle), chronological newest-first, chronological oldest-first, or a manual `custom` order. Every mode shares one cursor mechanism derived from the device's served-image history, so no separate position needs persisting. Set in Edit Device → Auto Rotate → Display order.
- **Custom-order drag-and-drop**: a Reorder mode in the Gallery (via `vuedraggable`) to arrange photos into the exact sequence used by `custom` display order; persisted through `POST /api/gallery/reorder`.
- **Server version in the web UI**: the running version (single source of truth: `config.yaml`, stamped into the binary at build via ldflags) is shown in the app bar and returned by the public `GET /api/status`.
- **Configurable button actions (server-side editing)**: per-device dropdowns in Edit Device → Power to map the frame's wake-button gestures (short / long / hold) to actions (next image, sleep, toggle deep sleep, info screen). Pushed to the frame via the existing device-config sync.

### Changed
- Shuffle (each photo once per cycle) replaces the previous "random with last-50 exclusion" as the default rotation behaviour for DB-backed sources.

### Database
- Migration `000030`: `devices.display_order`, `devices.shuffle_seed`, and `images.display_order`.

## v1.8.0

### Added
- **Battery overlay**: opt-in per-device battery badge baked onto the photo, using the `X-Battery-Percentage` the device sends on each fetch. Display style is selectable (icon, text, or both) and the fill turns red at ≤15%. Works on the pull path in every layout.
- **Per-element overlay placement**: Date, Photo Date, Weather and Battery can each be placed in any of six slots (top/bottom × left/center/right). Date/Photo-Date/Weather apply on the full-photo (`photo_overlay`) layout; Battery floats on the photo in all layouts. Each element now renders as its own translucent chip so it stays legible in any corner.
- **Overlay text size**: an adjustable size scale (50%–200%) applied to all overlay elements.
- **Live overlay preview** in the device settings dialog that mirrors the renderer's placement, size and battery-style rules and updates instantly.
- **ComfyUI AI provider**: generate images via a local ComfyUI server (workflow editable from the web UI), in addition to OpenAI and Google Gemini.
- **Raw-EPD push/pull for storage-less boards**: boards without flash/SD (e.g. FireBeetle 2 ESP32-E) receive uncompressed `application/x-epd-raw` so they don't OOM inflating EPDGZ.
- **Dynamic image-source list** (`GET /api/sources`) and an in-dialog source switcher.
- **Named themes** (terracotta/ocean/forest × light/dark) selectable from the app bar.
- **Battery badge rotation**: rotate the battery icon 0/90/180/270° per device (e.g. for a portrait-mounted frame), with extra chip headroom so a turned icon stays inside its background.
- **Battery text side**: place the percentage text on any side of the icon — right, left, above or below.
- **Battery icon size**: a per-device size scale (50%–200%) for the battery icon, independent of the overlay text size.
- **Device-facing server URL override**: an optional base URL (Settings → Data Sources) used to build the per-source Image Endpoint URLs. Blank auto-detects (http appends the add-on port; https / reverse-proxy origins are used as-is); set it (e.g. `https://photos.example.com`, no port) when running behind a reverse proxy.

### Changed
- Capability-gated UI: upload, OTA, AI key fields and software flash-mode controls are hidden on boards that don't support them and shown automatically on those that do.
- Disconnecting Immich now also clears the synced photos (and disables auto-sync), so its albums no longer linger as an available source.

### Fixed
- Immich "Test Connection" no longer reports a false failure for **scoped API keys**: a `403` (missing `user.read`) or `404` on `/api/users/me` now falls back to verifying the key against `/api/albums` (the permission the app actually needs).
- "Sync from Device" now surfaces the frame's own AI prompt (`ai_prompt` from the device config) into the dialog instead of dropping it.

### Database
- Migrations `000024`–`000029`: `show_battery`; overlay placement columns (`date_position`, `photo_date_position`, `weather_position`, `battery_position`, `battery_style`); `overlay_scale`; `battery_rotation`; `battery_text_side`; and `battery_icon_scale`.

## v1.7.6

### Added
- Immich sync gained per-server **Sync Mode** in Settings: `album` (default, existing behavior), `all` (entire library via `/api/search/metadata`), `favorites` (Immich Favorites), and `memories` ("on this day" assets via `/api/memories`). The album picker only renders when mode is `album`. Closes #32
- Home Assistant add-on store now ships an `icon.png` (128×128 tile) and `logo.png` (250×100 banner) rendered from the firmware project's icon, so the add-on shares the brand mark with the rest of the ecosystem

### Changed
- Image source dispatch refactored into a flat plugin registry (`internal/imagesource.Source` + `Registry`). Each of the eight sources (`ai_generation`, `fractal`, `dla`, `gallery`, `immich`, `synology_photos`, `google_photos`, `url_proxy`) is now its own ~30-line plugin file owning one source name; the handler does a single `registry.Fetch` with zero per-source branching. The four library-backed sources share `RunDBPhotoFlow` (exclusion-aware pick + smart collage + photo-date lookup) parameterized by per-source pick/load callbacks. Adding a new source is now one file plus one `main.go` registration. `handler/image.go` shrinks ~365 lines. Fractal and DLA generative algorithms ported from the standalone `fractalgen` and `dla` CLIs (contributor: Christopher Rowley)

### Fixed
- Gallery uploads from iPhone (and any source that writes EXIF `Orientation` instead of rotating pixels) showed up sideways on the device and in the gallery. Uploads and Google Photos sync now run `magick -auto-orient` to bake the orientation into the pixel grid and reset the tag to 1. Telegram, Immich, and Synology paths were already covered
- Smart collage on a portrait device paired a landscape first photo with a portrait (not landscape) second photo, which `DrawCover` then cropped to a wide strip — and symmetrically on landscape devices. The second-photo query now targets the slot's shape (opposite of the device) instead of the device's own orientation

## v1.7.5

### Fixed
- Gallery delete failed with HTTP 500 ("database is locked") and 26-30s latency under concurrent image fetches. SQLite is now opened in WAL mode with a 30s busy_timeout (`_journal_mode=WAL&_busy_timeout=30000&_synchronous=NORMAL`), so the per-fetch `device_history` writer no longer starves user-facing writes. The per-fetch insert + prune is also wrapped in a single transaction so the writer holds the lock once instead of three times
- Gallery `DeletePhoto` / `DeletePhotos` now drop the DB row before removing the file, so a timed-out delete can no longer leave a row pointing at a missing file. Thumbnail handler returns 404 instead of 500 when the source file is gone
- Immich users got a Synology-flavored empty-state message; each source now has its own copy that points at the correct Data Sources tab

### Changed
- Gallery card and delete UI restyled to match the device webapp: outlined cards with hover lift/shadow, photos shown at native aspect ratio inside square cards (`contain` + grey letterbox), `mdi-delete` trash-icon affordance revealed on top-right corner hover, inline delete dialogs with thumbnail preview and a destructive Delete button
- Both per-photo and "Delete All" actions now require confirmation
- Gallery shows 6 photos per row at large viewports (down from 8)

## v1.7.4

### Added
- Standalone server-side gallery: photos uploaded from the web UI or sent to the Telegram bot are stored under `data/photos/gallery/` and tracked in the images table. Telegram is now an upload path into the gallery rather than a separate source; push-to-device on bot upload still works when enabled. Migration `000022` rewrites any existing `images.source` / `settings.image_source` rows from `telegram` to `gallery`

### Changed
- Update AI model list: drop deprecated DALL-E entries, add Gemini 3.1 Flash
- Split server-owned device fields from hardware-derived ones in the device model

### Fixed
- Renderer: emit CSS `contain` for fit display mode so cropped images render correctly

### Performance
- Rewrite `device_histories` prune as a range delete so it stays O(log n) once a device's history grows past a few hundred rows

### Build
- Bump Alpine base image to 3.21 for newer libheif

## v1.7.3

### Fixed
- Device image URL is now built against the add-on port (default `9607`, configurable via `VITE_ADDON_PORT`) instead of `window.location.origin`, so the URL works when the webapp is served through Home Assistant ingress (`:8123`) but the ESP32 reaches the server directly

### Performance
- Index the hot-path GORM queries that were doing sequential scans: `api_keys(user_id)` and `(user_id, device_id)` (token lookups on every device config save), `devices(host)` (LAN fallback identification on every image fetch), and `images(source)` / `(source, synology_photo_id)` / `(source, immich_asset_id)` (per-photo dedup during Synology and Immich sync)

## v1.7.1

### Added
- Walnut photo-frame icon, used as both the favicon and a 32×32 prepend in the app-bar so the title bar reads as a branded header

### Changed
- New warm amber color palette: primary `#ce9160`, with matched-saturation error / info / success / warning (`#982f2f`, `#2f6398`, `#2f9852`, `#987e2f`)

### Fixed
- Image fetch failures propagate as errors instead of falling back to a picsum placeholder

## v1.7.0

### Added
- Remote device configuration: manage device settings from the server. The device dialog is now a tabbed UI matching the device webapp (General, Auto Rotate, Processing, Palette, Home Assistant), fetches live config from the device, and pushes changes back on Save with an offline fallback message.
- Display orientation dropdown shows resolution (e.g., "Portrait 480×800")

### Changed
- Display mode "contain" renamed to "fit" for consistency
- Image orientation detection unified across sources; orientation is passed to the CLI for device-aware processing
- Bind flow simplified: image URL and token generated on Save (no separate Bind button)

### Performance
- Cache resolved IP on photoframe client and reuse clients across fetches
- Use dns-sd for fast mDNS resolution on macOS
- Composite index on `device_histories(device_id, served_at)`
- Prune device_histories to 50 entries, remove unnecessary COUNT query

### Fixed
- Increase HTTP client timeout to 120s for image push operations
- Reuse existing device token instead of revoking and regenerating when re-binding
- Return 404 instead of 500 when no URL proxy sources are configured
- Use `--ignore-scripts` for frontend npm install to skip canvas native build

## v1.6.1

### Added
- EPDGZ format: serve compressed 4-bit-per-pixel images by default, saving bandwidth and enabling instant display rendering on the device. Automatically falls back to PNG for firmware older than v2.6.2.
- Internet operation: bind device tokens to specific device IDs for reliable identification over the internet without hostname/IP matching
- Auto-sync scheduler for Immich and Synology photo sources with configurable intervals
- Dev Container configuration for streamlined local development

### Fixed
- Synology: automatically re-login when session expires using saved device token (bypasses 2FA on trusted devices)
- Synology auto-sync UI: fix indentation in settings panel
- Device push: return 503 error when device is unreachable instead of misleading "queued" response
- Token management: add missing PUT endpoint for updating device binding on tokens
- Token backfill: skip ambiguous matches when multiple devices share the same name

## v1.6.0

### Added
- Overlay: display photo creation date from Immich EXIF metadata and Synology timestamps
- Overlay: new per-device "Show Photo Date" toggle in device settings

### Fixed
- Docker: pin Alpine to 3.20 to fix canvas native module build failure with GCC 15
- Docker: fix Go toolchain version mismatch in builder stage
- Immich: fix photo orientation for portrait photos with EXIF rotation (orientations 5-8)
- Increase HTTP client timeout to 120s for slow e-ink display updates
- Restore authentication to image serving endpoint
- Add IPv6 link-local fallback to mDNS transport

## v1.5.6

### Added
- Loading spinner for device list and gallery source switching

### Fixed
- Immich: fix concurrent image request failures by adding preview API fallback
- Immich: fix connection failures with .local mDNS hostnames resolving to link-local IPv6
- Immich: fix data race on shared client during concurrent requests
- Immich: fix source binding for device configuration
- Immich: include response body in error messages for better debugging
- Synology: fix .local mDNS IPv6 link-local connection issues
- Parallelize initialization fetches for faster startup

## v1.5.4

### Added
- Immich: gallery tabs now default to Immich, reordered as Immich → Google Photos → Synology

### Fixed
- Synology: personal album thumbnails no longer return 404

## v1.5.3

### Added
- Google Calendar integration: display today's events as an overlay on the frame
- Calendar: show at least 1 event entry on small screens

## v1.5.2

### Fixed
- Synology: empty orientation field no longer causes layout issues
- Collage: fix potential duplicate photo in collage

## v1.5.0

### Added
- AI Generation: support for Gen AI image rotation
- Overlay: scale fonts and UI elements based on image size

## v1.4.9

### Fixed
- Fix port binding and configuration propagation when running as HA add-on
- Fix auto-binding URL port detection for add-on environment

## v1.4.8

### Changed
- Build Docker images for both x86 and amd64 in CI
- Switch from prebuilt Docker image to local builds
- Fix ingress API base URL for HA add-on
- Fix data location migration to `/data`
- Migrate persistent data to `/data` directory for HA add-on compatibility

## v1.4.6 / v1.4.5

### Added
- Login session management
- Allow HA add-on to appear in the HA side panel
- Admin username and password can now be changed
- Auto-binding: frames are automatically bound to a data source on first connection
- Device binding: manually bind devices to specific data sources
- URL proxy data source support

### Fixed
- Prevent the same image from being served repeatedly

## v1.4.1

### Added
- Multi-device support with per-device resolution settings
- Push image directly from server to a specific frame
- Smart collage: automatically create side-by-side collages when photo orientation mismatches screen

## v1.3.3

### Added
- Push image from server to frame

### Fixed
- Remove stale device last-seen records
- Show an error when the target device is not reachable during push

## v1.3.1

### Fixed
- Fix npm package installation in Docker build

## v1.3.0

### Changed
- Updated UI style to match new firmware web app
- Switched image processing to the `epaper-image-convert` package

## v1.2.1

### Fixed
- Fix clipboard copy for image URL

## v1.2.0

### Added
- Authentication: login with username and password required to access the UI and API

### Fixed
- Set correct `Content-Length` header on image serving endpoint

## v1.1.2

### Added
- Display the image serving endpoint URL in the UI

### Fixed
- Various bug fixes

## v1.1.1

### Fixed
- Fix OAuth redirect URL for Google Photos
- Telegram: push received photo to frame when device is reachable

## v1.1.0

### Added
- Synology DSM Photos integration
- Google Photos and Synology integrations can now be used side by side

## v1.0.2

### Changed
- Improved overlay rendering styles

## v1.0.1

### Fixed
- Fix OAuth redirect URL for Google Photos authentication
