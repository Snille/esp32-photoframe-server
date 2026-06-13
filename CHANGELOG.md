# Changelog

## v1.23.0

### Added
- **"On this day" mode.** A per-frame toggle that shows only photos taken on today's date (any year), so the frame becomes a daily memories slideshow. On days with no matching photo it falls back to the full library, so the frame is never blank. Works for the ordered sources (Gallery / Immich / Synology / Google Photos).
- **Hide and favorite photos.** Star the photos you love and hide the ones you don't — from Home Assistant or the web UI:
  - **Hide Current Photo** (MQTT button) — removes the photo on the frame from every frame's rotation (a "take it out of the slideshow" flag; it isn't deleted from your library).
  - **Toggle Favorite** (MQTT button) — stars/unstars the photo currently on the frame.
  - **Favorites Only** (per-frame toggle / MQTT switch) — restrict a frame's rotation to starred photos (falls back to the full library when none are starred).
  - **Current Photo Favorite** (MQTT binary sensor) — whether the photo on the frame right now is starred.
- **Per-frame Online/Offline sensor (MQTT).** A connectivity sensor that flips to *offline* when a frame hasn't checked in for roughly two rotation cycles, so you can alert on a frame that has gone quiet (dead battery, lost Wi-Fi). The server now re-publishes state every few minutes so this stays current even without a check-in.

## v1.22.0

### Added
- **"Where am I in the rotation?" — new Home Assistant sensors (MQTT).** Each frame now reports its position in its photo rotation (for ordered sources — Gallery / Immich / Synology / Google Photos, with collage off):
  - **Rotation Size** — how many photos are in the frame's rotation (respecting its Immich album filter).
  - **Rotation Status** — a one-line summary: in shuffle mode "*N* of *T* left" (counts down to a fresh shuffle), in chronological/custom mode "Image *P* of *T*".
  - **Rotation Position** / **Rotation Remaining** — the numeric position and images-left (diagnostic).
  - **Rotation Completes** — a timestamp estimate of when the current cycle finishes (a fresh shuffle / wrap), based on the rotate interval. Only when auto-rotate is on.
- **Current Photo Date (MQTT)** — the capture date of the photo currently on the frame.
- **Immich Albums (MQTT)** — the names of the Immich albums the frame pulls from (falls back to album IDs until the album list has been loaded once).
- **Reshuffle button (MQTT)** — reshuffles the frame's photo order for the next pull. Server-side, so it works on any board.
- **Rotation-position overlay.** A new opt-in overlay chip bakes the rotation position onto the photo itself, with selectable placement like the other overlay elements. Kept compact to stay out of the way: in shuffle mode it shows images left (with a shuffle icon), in chronological/custom mode the image number (with a "#" icon); a per-frame **Show total** toggle switches between e.g. `23` and `23/183`. It hides itself for sources that have no fixed rotation (Public Art, AI, URL proxy) and in collage mode.

## v1.21.0

### Added
- **Control your frames from Home Assistant (MQTT).** The HA bridge gains writable controls so automations can change a frame, not just read it:
  - **Image Source** (select) — switch the frame's photo source (Immich, Gallery, Public Art, …). Applies to the next pull and reissues the device token automatically.
  - **Image Order** (select) — Shuffle / Newest first / Oldest first / Custom.
  - **Refresh Interval** (number) — minutes between image pulls.
  - **Deep Sleep** and **Auto Rotate** (switches) — toggle the frame's power/rotation behaviour (synced to the frame on its next pull).
  - **Rotate Now** (button) — advance the frame immediately. Shown but **Unavailable on always-sleeping boards** (e.g. the FireBeetle) where a live command can't reliably land; it activates automatically on always-reachable boards.

  These replace the former read-only Image Source / Refresh Interval / Image Order / Deep Sleep sensors (the controls show the same state and let you change it); the old read-only entities are removed automatically.

## v1.20.0

### Added
- **Six more Home Assistant sensors per frame (MQTT).** Each frame now also exposes:
  - **Timezone** — the frame's configured POSIX timezone.
  - **Display Rotation** — how the frame is mounted (0/90/180/270°).
  - **Image Order** — the photo-ordering mode (Shuffle / Newest first / Oldest first / Custom).
  - **Server Host** — the server (host:port) the frame pulls its images from.
  - **Deep Sleep** — binary sensor for whether the frame deep-sleeps between rotations.
  - **Last Trigger** — what caused the most recent image change: Timer (auto-rotate wake), Button (wake button), Boot (cold boot), Push (server-initiated) or Pull. Distinguishing Timer/Button/Boot requires the matching firmware (it reports the wake reason via a new `X-Wake-Reason` header); older firmware shows "Pull".

### Fixed
- **"Previous Image" no longer shows a broken-image icon in Home Assistant.** When a frame had no previous image yet (fresh device, or just after a source change) the Previous Image entity published an empty payload, which Home Assistant renders as a broken icon. It now goes **Unavailable** in that case, mirroring the Next Image entity.
- **Clicking a frame's miniature now opens the image at a usable size.** The full-image lightbox rendered the image at the panel's small native resolution (~600 px) instead of scaling it up to fill the viewport, so it looked shrunk. It now scales to fit the screen while preserving aspect ratio.

### Security
- **Each install now uses a unique JWT signing secret.** Without an explicit `JWT_SECRET`, the server previously fell back to a hardcoded default, which would let anyone forge admin and device tokens. It now generates a strong random secret on first run and persists it (with restrictive permissions) in the data directory. **Note:** on first start after this upgrade, existing tokens become invalid — re-log in to the web UI, and re-save each frame's configuration once (while it is awake) to push it a fresh device token.

### Changed
- Internal cleanups: removed a duplicate settings route, fixed a flaky test helper, corrected the Google OAuth callback-port example (9607), and suppressed a false-positive lint warning on the firmware installer button.

## v1.19.2

### Fixed
- **"Next Image Pull" now matches the frame's aligned wake schedule.** The sensor estimated the next pull as *last check-in + refresh interval*, which was wrong for frames using aligned rotation: those wake on clock-grid boundaries (e.g. :00/:15/:30/:45 for a 15-minute interval), so an off-cycle **button press** does not push the next auto-pull forward. The estimate now mirrors the firmware's wake scheduler (aligned grid + its "skip if <60s away" guard), and is only published when auto-rotate is enabled. (Still not adjusted for the sleep schedule — the Sleep Schedule sensor gives that context.)

## v1.19.1

### Added
- **Collage sensor + clearer Next Image state in Home Assistant.** When a frame runs in **collage** mode there is no deterministic "next image" (collage shuffles random pairs), so the Next Image entity was simply empty with no explanation. Now:
  - A new **Collage** binary sensor shows whether collage is on for each frame.
  - The **Next Image** entity is marked **Unavailable** (visibly disabled) when collage is on or the source has no fixed next image, instead of being silently empty.
  - A new **Next Image Status** sensor explains why (e.g. "Disabled — collage mode shuffles random pairs, so there is no fixed next image" / "Active").

## v1.19.0

### Added
- **More Home Assistant sensors per frame (MQTT).** Each frame now also exposes:
  - **Refresh Interval** — how often the frame is set to pull a new image (minutes, from `rotate_interval`).
  - **Sleep Schedule** — the frame's configured quiet hours as `HH:MM–HH:MM` (or `Off`), during which it pauses all updates.
  - **Next Image Pull** — a timestamp for when the frame *should* next fetch an image (last check-in + the refresh interval). Interval-based; the Sleep Schedule sensor gives the quiet-hours context.
  - **Host** and **IP Address** — the frame's hostname (as the server addresses it) and the client IP it last checked in from.
- **Battery days-remaining in the Devices list.** The Battery column now shows the estimated days of runtime left (e.g. `~45 d left`) under the percentage, not just in the hover tooltip.

## v1.18.0

### Added
- **Home Assistant now shows Previous / Current / Next image per frame.** The MQTT bridge previously exposed a single "Current Image" entity that, because it published before the frame's new image had finished rendering, often lagged a rotation behind (it showed the *last* image, not the one on the wall). Each frame now exposes three image entities:
  - **Current Image** — what's on the frame right now. The bridge is notified *after* the new thumbnail is committed, so it's always truthful (no more off-by-one lag).
  - **Previous Image** — the image that was on the frame before the current one.
  - **Next Image** — a non-mutating preview render of what the next pull will show (overlays + the real e-paper dither), for the ordered DB-backed sources (gallery, Immich, Synology, Google Photos). Synthetic sources (AI/fractal/DLA), URL proxy, public art and collage mode have no deterministic "next", so that entity stays empty for them.

### Fixed
- **"Current Image" no longer shows the previous rotation's photo.** The MQTT publish was triggered at the start of the image request with a fixed 1.5s delay, which lost the race against image download + dithering on slower pulls. It now fires once `current_thumb_id` is committed.

## v1.17.2

### Fixed
- **MQTT bridge no longer fights itself when two servers share one broker.** The MQTT client used a fixed client ID (`esp32-photoframe-server`), so running two instances against the same broker (e.g. a Portainer prod container plus a dev container, or any second copy) made them repeatedly kick each other off in an endless `connected → connection lost: EOF` loop. The client ID is now made unique per instance by appending the hostname (the container ID under Docker), so multiple servers can connect to the same Home Assistant broker without disconnecting one another.

## v1.17.1

### Fixed
- **MiniMax is now selectable in the UI.** v1.17.0 added the MiniMax server-side image provider to the backend but never exposed it in the web app. Settings → AI Generation now has **MiniMax Global / MiniMax China API Key** fields, and the per-device AI provider dropdown offers **MiniMax (Global)** and **MiniMax (China)** with the Image-01 model.

## v1.17.0

### Added
- **Public Art image source** (`public_art`): auto-rotate or lock open-access museum artwork from the **Cleveland Museum of Art** on a frame — no API key. A new "Public Art" tab in Settings → Data Sources lets you search the collection, preview & crop (drag to pan, scroll to zoom, cover/fit), pick an artwork to lock, or push one to a frame. Point a frame's source at `public_art` to rotate by a saved query with de-duplication. Migration **000041** adds the serving-history table. (A source selector is present for future collections.) Also adds a **MiniMax** server-side AI image provider.
- **Home Assistant MQTT bridge**: the server publishes each frame to your Home Assistant MQTT broker using HA's MQTT discovery, so every frame shows up as a device with **Battery %, Battery Voltage, Battery Days Remaining, Battery Trend, Image Source, Last Seen** sensors and a **Current Image** entity — ready for automations (low-battery alerts, dashboards, etc.). The server is a plain MQTT client (its own broker user/pass) and does **not** need to run as an HA add-on. Configure it in the new Settings → **Home Assistant** tab (enable, broker host/port, credentials, advanced topics) with a live connection-status indicator.

## v1.16.0

### Added
- **Click a Devices-list miniature to see the full image on the frame.** The current-image thumbnail is now clickable and opens a lightbox showing the **full-resolution** dithered image at native panel size — exactly what the frame displays. Served via the new public `/served-image-full/:id` route (a full-size JPEG written alongside the miniature on every real serve, and cleaned up when the frame's image changes).

### Changed
- **Orientation rework: native panel orientation is now the baseline and "Display Rotation" drives everything.** The panel's native orientation (portrait for the 4" Spectra 6) is the anchor, and a single **Device → General → "Display Rotation"** choice (0/90/180/270°) drives both the rendered frame image **and** every preview (Devices-list miniature, full-image lightbox, companion-app preview) — no more per-feature un-rotation hacks. Rotation is a single `display_rotation_deg`-driven step: the composed image is pre-rotated into native panel layout before dithering, and previews rotate back to the viewing orientation by the same amount. The landscape/portrait label is now derived (shown read-only). Migration **000040** adds `devices.display_rotation_deg`, backfilled from the existing orientation (`landscape → 90°`) so frames look identical after upgrade.

## v1.15.0

### Added
- **Live status in the Devices list**: each row now shows a **miniature of the image currently on the frame** and its **battery status** (percent + icon, red at ≤15%, with days-remaining on hover) — the same battery data as the device's Power tab, at a glance. The list auto-refreshes every 30 s while the Devices tab is open. Migration 000039 adds `devices.current_thumb_id`.

### Fixed
- **The Devices-list miniature truthfully reflects the applied processing** (grayscale, palette, tone). It is built from the frame's actual dithered output (decoding the Spectra-6 EPD buffer, or the dithered PNG) rather than the converter's own thumbnail, which is snapshotted pre-dither and ignored those filters. The same fix makes the companion app's `X-Thumbnail-URL` truthful. The miniature is un-rotated from native panel layout back to the viewing orientation.

## v1.14.0

### Fixed
- **Saved processing preset and colour palette now apply on every path.** A server-initiated push rendered with library defaults — it never read the device's saved processing settings or palette — so the configured preset (e.g. Grayscale) and any palette calibration were silently ignored on pushes. `PushToHost` now applies the stored settings, exactly like the pull path.
- **Pull/button refreshes now render from the server's stored settings, not the frame's stale headers.** The frame echoes its own NVS settings back in `X-Processing-Settings` / `X-Color-Palette`; when config-sync hadn't reached the frame yet, those were stale and a button refresh ignored the configured preset. The server now treats its stored config as authoritative when present (falling back to the frame's headers only for standalone/unmanaged frames), so push and pull render identically.

### Changed
- **Saving now pushes processing settings and palette straight to an awake frame**, not just the device config. New `/api/settings/processing` and `/api/settings/palette` client calls mean a Save updates the frame's NVS immediately — keeping the frame's own WebUI and the dialog's "Sync from device" truthful — instead of waiting for the lazy config-sync header on the next image fetch.
- **Processing-preset detection tolerates float drift**: values synced back from a frame come through its 32-bit-float NVS (e.g. `1.4` → `1.4000000953…`), which an exact comparison mislabelled as "Custom". Detection now compares numbers with a small epsilon, so a synced-from-device preset is recognised correctly.

## v1.13.0

### Added
- **Download the Android companion app from the server**: a new `/app` page (and `/app/photoframe.apk`) serves the app's APK so a phone can install it by browsing to the server — no sideload transfer needed. A link next to the theme switcher in the app bar opens it. Drop the built APK at `<DATA_DIR>/app.apk` to enable it (the page shows a hint until then).
- **Non-mutating frame preview** (`/image/<source>?preview=1`): renders exactly what a frame shows next — overlays and all — **without** advancing the frame's display-order sequence, writing history, or recording a battery sample. This powers the companion app's live "what's on the wall" preview, which works even while the frame is asleep.

### Changed
- **Theme-following app-bar logo**: the photoframe logo now follows the active theme (its shell, sun and horizon take the theme's primary colour) instead of being a fixed terracotta mark; the sky inside the frame is a constant light blue.

### Fixed
- The battery badge now renders in the companion app's preview: the preview accepts an `X-Battery-Percentage` reading (which the server needs to draw the badge) while still recording no battery sample, so it stays non-mutating.

## v1.12.1

### Changed
- **Battery drain estimate now uses voltage when available**: if the frame reports its battery voltage (`X-Battery-Voltage`, firmware ≥ 2.9.2), the estimate regresses a voltage-derived state-of-charge through a LiPo discharge curve instead of the firmware's coarse integer percentage — a smoother, finer signal, so the %/day and days-remaining settle faster and jitter less. Falls back to the percentage when no voltage is reported. The Power tab notes which basis was used ("from voltage" / "from %").

## v1.12.0

### Added
- **Per-device Immich albums**: each frame can be restricted to one or more Immich albums from the same global connection (Devices → edit → Auto Rotate → "Immich albums"). A frame with none selected shows all synced Immich photos. Albums are imported and their membership tracked so the filter is exact; selecting albums on a frame triggers a background import of just those albums.
- **New global Sync Mode "Per-device albums"**: syncs only the albums frames have selected — nothing is pulled globally. Ideal for several frames each showing a different album, and avoids dragging in an entire (e.g. 60 000-photo) library. Each Sync Mode now shows an explanatory note, and "Entire library" carries a size warning.
- **Per-chip overlay icon toggle**: the leading icon on the Photo Date, Weather, Names, Location and Description chips can each be hidden independently (Overlay tab → "Show icon"). Lets a frame run a clean, text-only chip — e.g. a Description "slogan" in the Ole font with no icon.

### Changed
- The Immich album list is now sorted alphabetically (case-insensitive) in both the global and per-frame pickers (was creation order).

### Database
- Migration `000037`: `devices.immich_album_ids` + `immich_image_albums` membership table.
- Migration `000038`: `devices.overlay_hidden_icons`.

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
