package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type Setting struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `json:"value"`
}

const (
	SourceGooglePhotos   = "google_photos"
	SourceSynologyPhotos = "synology_photos"
	SourceGallery        = "gallery"
	SourceURLProxy       = "url_proxy"
	SourceAIGeneration   = "ai_generation"
	SourceImmich         = "immich"
	SourcePublicArt      = "public_art"
	SourceFractal        = "fractal"
	SourceDLA            = "dla"
)

// IsOrderedSource reports whether a source has a deterministic display-order
// cursor (so a truthful "next image" preview can be rendered). The DB-backed
// library sources qualify; synthetic generators (AI/fractal/DLA), the URL proxy
// and public-art do not. Collage mode also disqualifies a frame even on these
// sources, since it shuffles random pairs — check EnableCollage separately.
func IsOrderedSource(source string) bool {
	switch source {
	case SourceGallery, SourceImmich, SourceSynologyPhotos, SourceGooglePhotos:
		return true
	default:
		return false
	}
}

type Image struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	FilePath        string     `json:"file_path"`
	Caption         string     `json:"caption"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	Orientation     string     `json:"orientation"` // "landscape", "portrait"
	UserID          int64      `json:"user_id"`
	Status          string     `json:"status"`                                                                                                                      // pending, shown
	Source          string     `gorm:"index:idx_images_source;index:idx_images_source_synology,priority:1;index:idx_images_source_immich,priority:1" json:"source"` // "local", "google_photos", "synology_photos"
	SynologyPhotoID int        `gorm:"index:idx_images_source_synology,priority:2" json:"synology_id"`
	ThumbnailKey    string     `json:"thumbnail_key"`                                                    // Cache key for Synology
	ImmichAssetID   string     `gorm:"index:idx_images_source_immich,priority:2" json:"immich_asset_id"` // UUID for Immich assets
	PhotoTakenAt    *time.Time `json:"photo_taken_at"`                                                   // Original photo creation/taken date
	// PeopleJSON is a JSON array of {"name","birthDate"} for faces recognized in
	// the photo (Immich only). Location is a formatted "City, State, Country"
	// string from EXIF (Immich only). Both empty for sources that lack metadata.
	PeopleJSON   string `json:"people_json" gorm:"column:people_json"`
	Location     string `json:"location"`
	Description  string `json:"description"`   // photo description/caption (Immich exif description; gallery caption)
	DisplayOrder int    `json:"display_order"` // Manual sort position for devices in 'custom' order mode (lower = earlier)
	// Hidden photos are globally excluded from every frame's rotation (a "remove
	// this from the slideshow" flag, toggled from HA / the web UI without deleting
	// the asset). Favorite is a user-set star, surfaced to HA and usable as a
	// per-frame "favorites only" rotation filter.
	Hidden    bool           `json:"hidden" gorm:"default:0"`
	Favorite  bool           `json:"favorite" gorm:"default:0"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type GoogleAuth struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"-"`
	Expiry       time.Time `json:"expiry"`
}

type GoogleCalendarAuth struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"-"`
	Expiry       time.Time `json:"expiry"`
}

type Device struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `json:"name"`
	Host        string `gorm:"index" json:"host"` // IP or Hostname
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Orientation string `json:"orientation"`
	// DisplayRotationDeg (0/90/180/270) is how the frame is mounted relative to
	// the panel's native orientation, and the single source of truth the render
	// pipeline + all previews derive viewing orientation from. Width/Height stay
	// native (as reported by the firmware); Orientation is now a derived/legacy
	// mirror. A landscape-mounted portrait-native panel is 90°.
	DisplayRotationDeg int    `json:"display_rotation_deg" gorm:"default:0"`
	BoardName          string `json:"board_name"`
	// FirmwareVersion is the frame's reported firmware version. Refreshed on every
	// pull (X-Firmware-Version header) and on a hardware refresh (SystemInfo.Version).
	// Shown in the Devices list; empty until the frame first checks in.
	FirmwareVersion string `json:"firmware_version" gorm:"default:''"`
	// HTTPSSupported mirrors the device's system-info https_supported flag:
	// false on no-PSRAM boards (e.g. FireBeetle) that can't fit a TLS handshake
	// alongside the framebuffer, so the web UI warns against https:// image URLs.
	// Defaults true so existing devices and firmware that don't report it aren't
	// falsely flagged. Refreshed from hardware on add / sync.
	HTTPSSupported bool    `json:"https_supported" gorm:"column:https_supported;default:1"`
	EnableCollage  bool    `json:"enable_collage"` // Per-device collage setting
	ShowDate       bool    `json:"show_date"`
	ShowPhotoDate  bool    `json:"show_photo_date"`
	ShowWeather    bool    `json:"show_weather"`
	WeatherLat     float64 `json:"weather_lat"`
	WeatherLon     float64 `json:"weather_lon"`
	AIProvider     string  `gorm:"column:ai_provider" json:"ai_provider"`
	AIModel        string  `gorm:"column:ai_model" json:"ai_model"`
	AIPrompt       string  `gorm:"column:ai_prompt" json:"ai_prompt"`
	// DisplayOrder controls the sequence photos are shown in for DB-backed
	// sources: shuffle | chrono_newest | chrono_oldest | custom. ShuffleSeed is
	// server-managed (bumped each completed shuffle cycle) and not user-editable.
	DisplayOrder string `json:"display_order" gorm:"default:'shuffle'"`
	ShuffleSeed  int64  `json:"-" gorm:"default:0"`
	Layout       string `json:"layout"`       // "photo_info", "photo_overlay", "side_panel"
	DisplayMode  string `json:"display_mode"` // "cover" or "fit"
	ShowCalendar bool   `json:"show_calendar"`
	CalendarID   string `json:"calendar_id"`  // Google Calendar ID (per-device)
	DateFormat   string `json:"date_format"`  // Go time format string, empty = default "Mon, Jan 02"
	ShowBattery  bool   `json:"show_battery"` // Overlay a battery badge (uses X-Battery-Percentage from device)
	// Per-element overlay placement. One of: top-left, top-center, top-right,
	// bottom-left, bottom-center, bottom-right. Date/photo-date/weather only
	// apply on the full-photo (photo_overlay) layout; battery applies on the
	// photo in every layout.
	DatePosition      string  `json:"date_position" gorm:"default:'bottom-left'"`
	PhotoDatePosition string  `json:"photo_date_position" gorm:"default:'bottom-left'"`
	WeatherPosition   string  `json:"weather_position" gorm:"default:'bottom-right'"`
	BatteryPosition   string  `json:"battery_position" gorm:"default:'top-right'"`
	BatteryStyle      string  `json:"battery_style" gorm:"default:'both'"`      // both | icon | text
	BatteryRotation   int     `json:"battery_rotation" gorm:"default:0"`        // rotate the battery badge: 0/90/180/270 degrees
	BatteryTextSide   string  `json:"battery_text_side" gorm:"default:'right'"` // which side of the icon the % text sits: left | right
	BatteryIconScale  float64 `json:"battery_icon_scale" gorm:"default:1"`      // size multiplier for the battery icon only (0.5–2.0), independent of text size
	OverlayScale      float64 `json:"overlay_scale" gorm:"default:1"`           // size multiplier for overlay elements (0.5–2.0)
	// Typeface for the floating overlay chips. OverlayFont is one of the keys in
	// validOverlayFonts (mapped to a real installed family by the renderer);
	// OverlayWeight is regular | medium | bold.
	OverlayFont   string `json:"overlay_font" gorm:"default:'noto_sans'"`
	OverlayWeight string `json:"overlay_weight" gorm:"default:'medium'"`
	// People names overlay (from Immich face metadata). NameFormat is one of the
	// keys in validNameFormats; NamesMaxLen caps the rendered string length (whole
	// names that fit are kept, the rest collapse to "+N"); NamesShowAge appends the
	// person's age at the photo date in parentheses. ShowLocation draws the photo's
	// EXIF place. Both only apply on the photo_overlay layout.
	ShowNames        bool   `json:"show_names" gorm:"default:0"`
	NamesPosition    string `json:"names_position" gorm:"default:'top-left'"`
	NameFormat       string `json:"name_format" gorm:"default:'first_last'"`
	NamesShowAge     bool   `json:"names_show_age" gorm:"default:0"`
	NamesMaxLen      int    `json:"names_max_len" gorm:"default:30"`
	ShowLocation     bool   `json:"show_location" gorm:"default:0"`
	LocationPosition string `json:"location_position" gorm:"default:'bottom-center'"`
	LocationMaxLen   int    `json:"location_max_len" gorm:"default:40"`
	// Photo description overlay (Immich exif description / gallery caption).
	ShowDescription     bool   `json:"show_description" gorm:"default:0"`
	DescriptionPosition string `json:"description_position" gorm:"default:'wide-bottom'"`
	DescriptionMaxLen   int    `json:"description_max_len" gorm:"default:80"`
	// Rotation-position overlay: a compact "where am I in the rotation" chip.
	// For shuffle it shows images left in the cycle, for chronological/custom the
	// current image number. RotationShowTotal appends "/<pool size>". Only ordered
	// DB-backed sources have a meaningful rotation, so the chip self-suppresses
	// otherwise (collage / synthetic sources).
	ShowRotation      bool   `json:"show_rotation" gorm:"default:0"`
	RotationPosition  string `json:"rotation_position" gorm:"default:'bottom-right'"`
	RotationShowTotal bool   `json:"rotation_show_total" gorm:"default:1"`
	// Rotation-pool filters (ordered DB sources). OnThisDay restricts the frame to
	// photos taken on today's month/day (any year); FavoritesOnly to starred
	// photos. Both fall back to the full pool when the filtered set is empty.
	OnThisDay     bool `json:"on_this_day" gorm:"default:0"`
	FavoritesOnly bool `json:"favorites_only" gorm:"default:0"`
	// Per-device Immich album filter: comma-separated Immich album UUIDs. When
	// set, this frame only shows photos that belong to one of these albums
	// (using the same global Immich connection). Empty = all Immich photos.
	ImmichAlbumIDs string `json:"immich_album_ids" gorm:"default:''"`
	// Comma-separated overlay element keys whose leading icon is hidden
	// (photo_date, weather, names, location, description). Empty = all icons
	// shown. Lets a frame run a clean text-only chip (e.g. a Description slogan).
	OverlayHiddenIcons string `json:"overlay_hidden_icons" gorm:"default:''"`
	// Remote config sync fields (JSON blobs synced from/to device)
	DeviceConfig             string `json:"device_config" gorm:"default:'{}'"`
	DeviceProcessingSettings string `json:"device_processing_settings" gorm:"default:'{}'"`
	DeviceColorPalette       string `json:"device_color_palette" gorm:"default:'{}'"`
	ConfigLastUpdated        int64  `json:"config_last_updated" gorm:"default:0"`
	// CurrentThumbID is the id (unix-nano filename stem) of the most recent
	// served-image thumbnail for this frame, served via /served-image-thumbnail/:id.
	// Lets the Devices list show a miniature of what's currently on the frame.
	CurrentThumbID string `json:"current_thumb_id" gorm:"default:''"`
	// PrevThumbID is the thumbnail id of the image that was on the frame before
	// the current one (the demoted CurrentThumbID); NextThumbID is a non-mutating
	// preview render of what the next pull will show (ordered DB-backed sources
	// only). Both feed the Home Assistant MQTT Previous / Next image entities.
	PrevThumbID string `json:"prev_thumb_id" gorm:"default:''"`
	NextThumbID string `json:"next_thumb_id" gorm:"default:''"`
	// LastIP is the client IP the frame last checked in from (best-effort, via
	// X-Forwarded-For when proxied), surfaced as an HA diagnostic sensor.
	LastIP string `json:"last_ip" gorm:"default:''"`
	// LastSeenAt is when the frame last checked in (pulled an image). Updated on
	// every real pull; surfaced as "Last check-in" in the Devices list. Nullable:
	// null means the frame has never been seen since this column was added.
	LastSeenAt *time.Time `json:"last_seen_at"`
	// LastResetReason is the frame's most recent reset cause (X-Reset-Reason on
	// pull): poweron / deepsleep / sw / task_wdt / int_wdt / wdt / panic /
	// brownout / efuse / pwr_glitch / cpu_lockup / usb / jtag / sdio / ext.
	// Lets the Devices list flag a crash-looping frame after recovery.
	LastResetReason string `json:"last_reset_reason" gorm:"default:''"`
	// BatteryStatus is the coarse charge state the frame reports each pull
	// (X-Battery-Status): "charging", "full" or "on_battery". Empty when the
	// board can't sense it; surfaced as the HA "Battery Status" sensor.
	BatteryStatus string `json:"battery_status" gorm:"default:''"`
	// LastTrigger is what caused the frame's most recent image change, surfaced as
	// the HA "Last Trigger" sensor: "timer" (auto-rotate wake), "button" (wake
	// button), "boot" (cold boot/reset), "push" (server-initiated) or "pull"
	// (firmware too old to report a wake reason). Set on every real serve.
	LastTrigger string `json:"last_trigger" gorm:"default:''"`
	// PendingNextImageID pins the exact image the next ordered pull should serve,
	// set by the HA "Skip Queue" command to jump N steps in the rotation. The pull
	// serves it and clears the pin (back to 0); rotation then continues from there.
	PendingNextImageID uint `json:"-" gorm:"default:0"`
	// LogRetentionValue + LogRetentionUnit control how long this device's
	// activity log (DeviceLog rows) is kept before the oldest entries are
	// pruned. Unit is one of "days" | "months" | "years". Default 6 months.
	LogRetentionValue int    `json:"log_retention_value" gorm:"default:6"`
	LogRetentionUnit  string `json:"log_retention_unit" gorm:"default:'months'"`
	// BatteryCapacityMAh is optional (0 = not set). The %/day drain estimate
	// doesn't need it — it's already capacity-independent — but when set it
	// lets the server also report an estimated average discharge current in
	// mA, a number that can't be derived without knowing the pack size.
	// Explicit column name: GORM's naming strategy would otherwise derive
	// "battery_capacity_m_ah" from the Go field name (splitting "MAh"),
	// which doesn't match the migration's "battery_capacity_mah" column.
	BatteryCapacityMAh int `json:"battery_capacity_mah" gorm:"column:battery_capacity_mah;default:0"`
	// BatteryADCGPIO mirrors which GPIO (if any) the frame is using for an
	// external battery voltage divider, on boards with no built-in battery
	// ADC (e.g. the FireBeetle 2 ESP32-S3). Read-only from the server's
	// perspective -- the pin is selected on the frame's own local WebGUI and
	// reported here via X-Battery-ADC-Pin on each check-in. -1 = not
	// configured / not applicable. Explicit column name for the same reason
	// as BatteryCapacityMAh above.
	BatteryADCGPIO int       `json:"battery_adc_gpio" gorm:"column:battery_adc_gpio;default:-1"`
	CreatedAt      time.Time `json:"created_at"`
}

const (
	LayoutPhotoInfo    = "photo_info"
	LayoutPhotoOverlay = "photo_overlay"
	LayoutSidePanel    = "side_panel"
)

// Per-device image display order modes (Device.DisplayOrder).
const (
	DisplayOrderShuffle      = "shuffle"       // random, each photo once per cycle, then reshuffle
	DisplayOrderChronoNewest = "chrono_newest" // by capture date, newest first
	DisplayOrderChronoOldest = "chrono_oldest" // by capture date, oldest first
	DisplayOrderCustom       = "custom"        // by manual Image.DisplayOrder
)

// NormalizeDisplayOrder clamps the display order to a known mode, defaulting to
// shuffle for empty / unknown input.
func NormalizeDisplayOrder(s string) string {
	switch s {
	case DisplayOrderShuffle, DisplayOrderChronoNewest, DisplayOrderChronoOldest, DisplayOrderCustom:
		return s
	default:
		return DisplayOrderShuffle
	}
}

// OverlaySettings groups the per-element placement/style fields so they can be
// threaded through AddDevice/UpdateDevice as a single argument instead of five
// more positional parameters.
type OverlaySettings struct {
	DatePosition        string
	PhotoDatePosition   string
	WeatherPosition     string
	BatteryPosition     string
	BatteryStyle        string
	BatteryRotation     int
	BatteryTextSide     string
	BatteryIconScale    float64
	OverlayScale        float64
	OverlayFont         string
	OverlayWeight       string
	ShowNames           bool
	NamesPosition       string
	NameFormat          string
	NamesShowAge        bool
	NamesMaxLen         int
	ShowLocation        bool
	LocationPosition    string
	LocationMaxLen      int
	ShowDescription     bool
	DescriptionPosition string
	DescriptionMaxLen   int
	ShowRotation        bool
	RotationPosition    string
	RotationShowTotal   bool
	OverlayHiddenIcons  string
}

// validOverlayPositions is the set of placements the renderer understands.
// wide-top / wide-bottom are full-width bands suited to long content (location,
// names, description); the six corners hold compact chips.
var validOverlayPositions = map[string]bool{
	"top-left": true, "top-center": true, "top-right": true,
	"bottom-left": true, "bottom-center": true, "bottom-right": true,
	"wide-top": true, "wide-bottom": true,
}

// NormalizeOverlayPosition returns pos if it is a known placement, otherwise
// the supplied fallback. Keeps bad/empty input from reaching the template.
func NormalizeOverlayPosition(pos, fallback string) string {
	if validOverlayPositions[pos] {
		return pos
	}
	return fallback
}

// NormalizeBatteryStyle clamps the battery display style to a known value.
func NormalizeBatteryStyle(style string) string {
	switch style {
	case "icon", "text", "both":
		return style
	default:
		return "both"
	}
}

// NormalizeBatteryRotation clamps the battery badge rotation to one of the
// four right angles, defaulting to 0 for any other value.
func NormalizeBatteryRotation(deg int) int {
	switch deg {
	case 0, 90, 180, 270:
		return deg
	default:
		return 0
	}
}

// NormalizeRotationDeg clamps a display rotation to one of 0/90/180/270,
// defaulting to 0 (native panel orientation).
func NormalizeRotationDeg(deg int) int {
	switch deg {
	case 0, 90, 180, 270:
		return deg
	default:
		return 0
	}
}

// NormalizeBatteryTextSide clamps which side of the battery icon the percentage
// text sits on to a known value, defaulting to right.
func NormalizeBatteryTextSide(side string) string {
	switch side {
	case "right", "left", "top", "bottom":
		return side
	default:
		return "right"
	}
}

// NormalizeOverlayScale clamps the overlay size multiplier to [0.5, 2.0],
// defaulting to 1.0 for zero/unset/out-of-range input.
func NormalizeOverlayScale(scale float64) float64 {
	if scale <= 0 {
		return 1.0
	}
	if scale < 0.5 {
		return 0.5
	}
	if scale > 2.0 {
		return 2.0
	}
	return scale
}

// NormalizeBatteryIconScale clamps the battery icon size multiplier to
// [0.5, 2.0], defaulting to 1.0 for zero/unset/out-of-range input.
func NormalizeBatteryIconScale(scale float64) float64 {
	return NormalizeOverlayScale(scale)
}

// validOverlayFonts is the set of overlay typeface keys the renderer maps to a
// real installed font family. The five families were picked for legibility on
// low-contrast e-paper panels.
var validOverlayFonts = map[string]bool{
	"noto_sans":       true,
	"inter":           true,
	"dejavu_sans":     true,
	"liberation_sans": true,
	"dejavu_serif":    true,
	"ole":             true,
}

// NormalizeOverlayFont returns the font key if known, otherwise the default
// noto_sans. Keeps unknown/empty input from reaching the template.
func NormalizeOverlayFont(font string) string {
	if validOverlayFonts[font] {
		return font
	}
	return "noto_sans"
}

// NormalizeOverlayWeight clamps the overlay font weight to a known value,
// defaulting to medium.
func NormalizeOverlayWeight(weight string) string {
	switch weight {
	case "regular", "medium", "bold":
		return weight
	default:
		return "medium"
	}
}

// validNameFormats lists the people-name rendering formats. Keys mirror the
// WebUI dropdown.
var validNameFormats = map[string]bool{
	"first_last":    true, // "Anna Andersson"
	"first_initial": true, // "Anna A."
	"first":         true, // "Anna"
	"last_first":    true, // "Andersson Anna"
	"last_initial":  true, // "Andersson A."
	"last":          true, // "Andersson"
}

// NormalizeNameFormat returns the format key if known, else the default
// first_last.
func NormalizeNameFormat(format string) string {
	if validNameFormats[format] {
		return format
	}
	return "first_last"
}

// NormalizeNamesMaxLen clamps the people-name string length budget to a sane
// range, defaulting to 30 for zero/unset input.
func NormalizeNamesMaxLen(n int) int {
	if n <= 0 {
		return 30
	}
	if n < 8 {
		return 8
	}
	if n > 120 {
		return 120
	}
	return n
}

// NormalizeLocationMaxLen clamps the location string length budget, defaulting
// to 40 for zero/unset input.
func NormalizeLocationMaxLen(n int) int {
	if n <= 0 {
		return 40
	}
	if n < 8 {
		return 8
	}
	if n > 120 {
		return 120
	}
	return n
}

// NormalizeDescriptionMaxLen clamps the description string length budget,
// defaulting to 80. Descriptions can be long, so the ceiling is higher.
func NormalizeDescriptionMaxLen(n int) int {
	if n <= 0 {
		return 80
	}
	if n < 8 {
		return 8
	}
	if n > 240 {
		return 240
	}
	return n
}

// NormalizeImmichAlbumIDs trims and de-duplicates a comma-separated list of
// Immich album IDs into a canonical "id,id,id" string (order preserved, blanks
// dropped). Empty means "all Immich photos".
func NormalizeImmichAlbumIDs(s string) string {
	return strings.Join(ParseImmichAlbumIDs(s), ",")
}

// ParseImmichAlbumIDs splits the stored comma-separated album list into a clean
// slice of non-empty, de-duplicated IDs.
func ParseImmichAlbumIDs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// validOverlayIconKeys are the overlay elements that draw a leading icon and can
// therefore have it hidden. Date and calendar have no icon; battery has its own
// style control.
var validOverlayIconKeys = map[string]bool{
	"photo_date": true, "weather": true, "names": true,
	"location": true, "description": true,
}

// NormalizeOverlayHiddenIcons keeps only valid, de-duplicated element keys in a
// canonical comma-separated string.
func NormalizeOverlayHiddenIcons(s string) string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		k := strings.TrimSpace(part)
		if k == "" || seen[k] || !validOverlayIconKeys[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return strings.Join(out, ",")
}

// OverlayIconHidden reports whether the given element's icon is hidden per the
// stored comma-separated list.
func OverlayIconHidden(hidden, key string) bool {
	for _, part := range strings.Split(hidden, ",") {
		if strings.TrimSpace(part) == key {
			return true
		}
	}
	return false
}

type DeviceHistory struct {
	ID       uint      `gorm:"primaryKey" json:"id"`
	DeviceID uint      `gorm:"index" json:"device_id"` // Foreign key to Device
	ImageID  uint      `json:"image_id"`
	ServedAt time.Time `json:"served_at"`
}

// DeviceLog is one frame check-in attempt (success or failure), backing the
// per-device "Activity Log". Unlike DeviceHistory (which only records a
// successful image serve), this captures every pull the frame makes —
// including ones that failed — so a stalled frame's actual behavior is
// visible instead of just silence. Pruned to the owning device's configured
// retention window (Device.LogRetentionValue/Unit) after every write.
type DeviceLog struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	DeviceID       uint      `gorm:"index:idx_device_logs_device_time" json:"device_id"`
	Timestamp      time.Time `gorm:"index:idx_device_logs_device_time" json:"timestamp"`
	Success        bool      `json:"success"`
	StatusCode     int       `json:"status_code"`
	TriggerReason  string    `gorm:"column:trigger_reason" json:"trigger_reason"`
	Source         string    `json:"source"`
	ImageID        uint      `json:"image_id"`
	BatteryPercent int       `json:"battery_percent"`
	// Everything else a frame can report on a pull (see ServeImage), captured
	// alongside BatteryPercent so a bad reading can be cross-checked against
	// the raw voltage/status/reset-cause instead of just the coarse percent —
	// and so there's enough history here to chart over time later.
	VoltageMV       int    `json:"voltage_mv"`
	BatteryStatus   string `json:"battery_status"`
	FirmwareVersion string `json:"firmware_version"`
	ResetReason     string `json:"reset_reason"`
	IP              string `json:"ip"`
	DisplayWidth    int    `json:"display_width"`
	DisplayHeight   int    `json:"display_height"`
}

// BatterySample is one timestamped battery reading reported by a device on an
// image fetch (X-Battery-Percentage / optional X-Battery-Voltage). The drain
// estimator regresses these over a trailing window to derive %/day and the
// estimated runtime left. VoltageMV is 0 when the firmware doesn't send it.
type BatterySample struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DeviceID  uint      `gorm:"index:idx_battery_samples_device_time" json:"device_id"`
	SampledAt time.Time `gorm:"index:idx_battery_samples_device_time" json:"sampled_at"`
	Percent   int       `json:"percent"`
	VoltageMV int       `json:"voltage_mv"`
}

// ImmichImageAlbum records that an Immich-sourced image belongs to a given
// Immich album. An asset can be in several albums, so this is a join table.
// Used to filter a device's photo pool to its selected albums.
type ImmichImageAlbum struct {
	ImageID       uint   `gorm:"primaryKey" json:"image_id"`
	ImmichAlbumID string `gorm:"primaryKey;index:idx_immich_image_albums_album" json:"immich_album_id"`
}

func (ImmichImageAlbum) TableName() string { return "immich_image_albums" }

// ImmichAlbum caches an Immich album's display name keyed by its UUID, so the
// Home Assistant "Immich Albums" sensor can resolve a frame's selected album IDs
// to names without calling the Immich API on each publish. Refreshed whenever the
// album list is fetched (album picker / import).
type ImmichAlbum struct {
	ImmichAlbumID string `gorm:"primaryKey" json:"immich_album_id"`
	AlbumName     string `json:"album_name"`
}

func (ImmichAlbum) TableName() string { return "immich_albums" }

type UserSession struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	TokenID   string    `gorm:"index" json:"-"`
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type DeviceImageMapping struct {
	DeviceID uint `gorm:"primaryKey" json:"device_id"`
	ImageID  uint `gorm:"primaryKey" json:"image_id"`
}

type URLSource struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type DeviceURLMapping struct {
	DeviceID    uint `gorm:"primaryKey" json:"device_id"`
	URLSourceID uint `gorm:"primaryKey" json:"url_source_id"`
}

// GenerativeState persists the rolling state for a procedural image source
// (fractal zoom counter, DLA occupancy grids, etc.). Keyed on (DeviceID, Source)
// so a device can switch sources without losing its progress in either.
type GenerativeState struct {
	DeviceID  uint      `gorm:"primaryKey" json:"device_id"`
	Source    string    `gorm:"primaryKey" json:"source"`
	State     []byte    `json:"-"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PublicArtServingHistory tracks which artworks have been served to a device
// within a deduplication window, so auto-rotate cycles don't repeat the same
// artwork too soon.
type PublicArtServingHistory struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	DeviceID  uint      `gorm:"index:idx_pah_device_served,priority:1" json:"device_id"`
	Source    string    `gorm:"column:source;not null" json:"source"`         // e.g. "aic", "rijksmuseum"
	ArtworkID string    `gorm:"column:artwork_id;not null" json:"artwork_id"` // e.g. "aic:12345"
	ServedAt  time.Time `gorm:"index:idx_pah_device_served,priority:2" json:"served_at"`
}

func (PublicArtServingHistory) TableName() string {
	return "public_art_serving_history"
}
