package handler

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"

	_ "image/jpeg"

	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/imagesource"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/service"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/gcalendar"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/googlephotos"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/imageops"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/photoframe"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/weather"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// ImageHandlerDeps is the dependency bundle the handler needs at construction.
// Photo-library backends (synology, immich, google, on-disk gallery) live
// inside their respective imagesource plugins now — the handler itself
// only needs DB access for device lookup / history, the renderer for
// overlays, the processor for dithering, the weather/calendar clients for
// overlay data, the auth service, and the registered source registry.
type ImageHandlerDeps struct {
	Settings       *service.SettingsService
	Renderer       *service.RendererService
	Processor      *service.ProcessorService
	CalendarGoogle *googlephotos.Client
	Sources        *imagesource.Registry
	Weather        *weather.Client
	Calendar       *gcalendar.Client
	Auth           *service.AuthService
	DB             *gorm.DB
	DataDir        string
	MQTT           *service.MQTTService
}

type ImageHandler struct {
	settings       *service.SettingsService
	renderer       *service.RendererService
	processor      *service.ProcessorService
	calendarGoogle *googlephotos.Client
	sources        *imagesource.Registry
	weather        *weather.Client
	calendar       *gcalendar.Client
	auth           *service.AuthService
	db             *gorm.DB
	battery        *service.BatteryService
	mqtt           *service.MQTTService
	dataDir        string
}

func NewImageHandler(deps ImageHandlerDeps) *ImageHandler {
	return &ImageHandler{
		settings:       deps.Settings,
		renderer:       deps.Renderer,
		processor:      deps.Processor,
		calendarGoogle: deps.CalendarGoogle,
		sources:        deps.Sources,
		weather:        deps.Weather,
		calendar:       deps.Calendar,
		auth:           deps.Auth,
		db:             deps.DB,
		battery:        service.NewBatteryService(deps.DB),
		mqtt:           deps.MQTT,
		dataDir:        deps.DataDir,
	}
}

func (h *ImageHandler) ServeImage(c echo.Context) error {
	// Get source from route parameter
	source := c.Param("source")

	// preview=1 is a non-mutating render for the companion app: no device
	// history write, no battery sample, and the source pick must not persist
	// state. The app may still send X-Battery-Percentage so the battery badge
	// renders in the preview.
	preview := c.QueryParam("preview") == "1"

	// 1. Identify Device and Determine Settings
	// Three-tier identification: token DeviceID → X-Hostname → client IP
	var device model.Device
	deviceFound := false

	// Tier 1: Token-based identification (works over internet)
	if devID, ok := c.Get("device_id").(uint); ok && devID > 0 {
		if err := h.db.First(&device, devID).Error; err == nil {
			deviceFound = true
		}
	}

	// Tier 2: X-Hostname header (backward compat, LAN setups)
	if !deviceFound {
		if hostname := c.Request().Header.Get("X-Hostname"); hostname != "" {
			if err := h.db.Where("host = ?", hostname).First(&device).Error; err == nil {
				deviceFound = true
			}
		}
	}

	// Tier 3: Client IP (backward compat, LAN setups)
	if !deviceFound {
		clientIP := c.RealIP()
		if err := h.db.Where("host = ?", clientIP).First(&device).Error; err == nil {
			deviceFound = true
		}
	}

	// Native resolution of the device panel
	nativeW, nativeH := 800, 480
	// Logical resolution for image generation (respects orientation)
	logicalW, logicalH := 800, 480

	showDate := false
	showPhotoDate := false
	showWeather := false
	showBattery := false
	showNames := false
	showLocation := false
	showDescription := false
	var lat, lon float64

	if deviceFound {
		nativeW = device.Width
		nativeH = device.Height
		logicalW, logicalH = nativeW, nativeH

		showDate = device.ShowDate
		showPhotoDate = device.ShowPhotoDate
		showWeather = device.ShowWeather
		showBattery = device.ShowBattery
		showNames = device.ShowNames
		showLocation = device.ShowLocation
		showDescription = device.ShowDescription
		lat = device.WeatherLat
		lon = device.WeatherLon
	}

	// Battery level reported by the device on every image fetch
	// (X-Battery-Percentage). -1 means no battery / not readable, in which
	// case the badge is suppressed even if showBattery is enabled.
	batteryPercent := -1
	if bStr := c.Request().Header.Get("X-Battery-Percentage"); bStr != "" {
		if b, err := strconv.Atoi(bStr); err == nil {
			batteryPercent = b
		}
	}
	showBattery = showBattery && batteryPercent >= 0 && batteryPercent <= 100

	// Log a battery sample for the drain estimate whenever a real device fetch
	// carries a reading (independent of the show-battery overlay toggle). The
	// optional X-Battery-Voltage header (millivolts) gives a finer signal than
	// the coarse percentage when present. Throttled + async so it never delays
	// or fails the image response.
	if deviceFound && !preview && batteryPercent >= 0 && batteryPercent <= 100 {
		voltageMV := 0
		if vStr := c.Request().Header.Get("X-Battery-Voltage"); vStr != "" {
			if v, err := strconv.Atoi(vStr); err == nil {
				voltageMV = v
			}
		}
		go h.battery.RecordSample(device.ID, batteryPercent, voltageMV)
	}

	// Remember the IP the frame checked in from (for the HA IP-address sensor).
	// RealIP honours X-Forwarded-For, so this is the frame's LAN IP even behind a
	// reverse proxy that forwards it. Only write on change to avoid churn.
	if deviceFound && !preview {
		if ip := c.RealIP(); ip != "" && ip != device.LastIP {
			device.LastIP = ip
			go h.db.Model(&model.Device{}).Where("id = ?", device.ID).Update("last_ip", ip)
		}
		// Record what triggered this pull (for the HA "Last Trigger" sensor). The
		// firmware reports its deep-sleep wake cause via X-Wake-Reason; firmware too
		// old to send it is recorded as a generic "pull".
		trigger := "pull"
		switch strings.ToLower(c.Request().Header.Get("X-Wake-Reason")) {
		case "timer":
			trigger = "timer"
		case "button":
			trigger = "button"
		case "boot":
			trigger = "boot"
		}
		if trigger != device.LastTrigger {
			device.LastTrigger = trigger
			go h.db.Model(&model.Device{}).Where("id = ?", device.ID).Update("last_trigger", trigger)
		}
	}

	// (The MQTT bridge is notified at the end of the serve, once current_thumb_id
	// is committed and the next-image preview is rendered — see post-serve hook
	// below. Notifying here would race the thumbnail write and publish the
	// previous rotation's image as "Current".)

	// ALWAYS overrides logical resolution/orientation from Headers if present
	if wStr := c.Request().Header.Get("X-Display-Width"); wStr != "" {
		if w, err := strconv.Atoi(wStr); err == nil && w > 0 {
			logicalW = w
			nativeW = w
			if deviceFound && device.Width != w {
				device.Width = w
				h.db.Model(&device).Update("width", w)
			}
		}
	}
	if hStr := c.Request().Header.Get("X-Display-Height"); hStr != "" {
		if he, err := strconv.Atoi(hStr); err == nil && he > 0 {
			logicalH = he
			nativeH = he
			if deviceFound && device.Height != he {
				device.Height = he
				h.db.Model(&device).Update("height", he)
			}
		}
	}
	// Display Rotation (deg, one of 0/90/180/270) is the single source of truth
	// for how the frame is mounted relative to the panel's native orientation.
	// Native dims (nativeW/nativeH) stay the baseline; the viewing (logical) dims
	// and every rotation in the pipeline derive from deg. Legacy frames that
	// haven't synced a rotation fall back to the X-Display-Orientation header
	// (landscape => 90).
	deg := 0
	if deviceFound {
		deg = model.NormalizeRotationDeg(device.DisplayRotationDeg)
	}
	if deg == 0 && c.Request().Header.Get("X-Display-Orientation") == "landscape" {
		deg = 90
	}
	logicalW, logicalH = imageops.LogicalDims(nativeW, nativeH, deg)

	// Keep the legacy orientation mirror in sync (derived from the viewing dims)
	// for any consumer still reading it.
	orientation := "portrait"
	if logicalW >= logicalH {
		orientation = "landscape"
	}
	if deviceFound && device.Orientation != orientation {
		device.Orientation = orientation
		h.db.Model(&device).Update("orientation", orientation)
	}

	layout := model.LayoutPhotoOverlay
	displayMode := "cover"
	showCalendar := false

	if deviceFound {
		if device.Layout != "" {
			layout = device.Layout
		}
		if device.DisplayMode != "" {
			displayMode = device.DisplayMode
		}
		showCalendar = device.ShowCalendar
	}

	var img image.Image
	var err error
	var photoTakenAt *time.Time

	// 1.5. Get Device History for Exclusion
	var excludeIDs []uint
	if deviceFound {
		// History retention: ensure we don't repeat recent 50 images
		// Get last 50 served images for this device
		var history []model.DeviceHistory
		if err := h.db.Where("device_id = ?", device.ID).
			Order("served_at desc").
			Limit(50).
			Find(&history).Error; err == nil {
			for _, h := range history {
				excludeIDs = append(excludeIDs, h.ImageID)
			}
		}
	}

	// All image sources — synthetic (AI, fractal, DLA) and library-backed
	// (gallery, immich, synology, google_photos, url_proxy) — flow through
	// the unified imagesource.Registry.
	if !h.sources.Has(source) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "invalid source"})
	}
	var devicePtr *model.Device
	if deviceFound {
		devicePtr = &device
		// The device can override its AI prompt locally (set on the frame's
		// WebUI and sent as X-AI-Prompt). Used by the ai_generation source.
		if source == model.SourceAIGeneration {
			if p := strings.TrimSpace(c.Request().Header.Get("X-AI-Prompt")); p != "" {
				device.AIPrompt = p
			}
		}
	}
	sourceResp, err := h.sources.Fetch(source, &imagesource.Request{
		Device:       devicePtr,
		Source:       source,
		Width:        logicalW,
		Height:       logicalH,
		NativeWidth:  nativeW,
		NativeHeight: nativeH,
		Orientation:  orientation,
		ExcludeIDs:   excludeIDs,
		Preview:      preview,
	})
	if err != nil {
		if strings.Contains(err.Error(), "invalid source filter") {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "invalid source"})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "record not found") {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "no photos found for this device"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch photo: " + err.Error()})
	}
	img = sourceResp.Image
	servedImageIDs := sourceResp.ImageIDs
	if sourceResp.PhotoTakenAt != nil {
		photoTakenAt = sourceResp.PhotoTakenAt
	}

	// If the source asked to bypass post-processing, encode straight to PNG
	// and ship it. The renderer overlay and epaper-image-convert pipeline
	// are skipped — the source already produced a panel-ready image, and
	// CDR / preprocessing would shift its flat color regions.
	if sourceResp.SkipPostProcessing {
		// The source produced a panel-ready image in viewing orientation; rotate
		// it into native panel layout (inverse of deg). No-op at deg=0.
		out := imageops.RotateDeg(img, 360-deg)
		var buf bytes.Buffer
		if err := png.Encode(&buf, out); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "png encode: " + err.Error()})
		}
		body := buf.Bytes()

		applyConfigSyncHeader(c, &device, deviceFound)

		// These sources ship a panel-ready image with no stored thumbnail, so
		// there's no current/next image to publish — just refresh the bridge
		// state (battery / last-seen) for the frame.
		if deviceFound && !preview && h.mqtt != nil {
			h.mqtt.NotifyDeviceUpdated(device.ID)
		}

		c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		return c.Blob(http.StatusOK, "image/png", body)
	}

	// 1.6. Record History (skipped for non-mutating preview renders)
	if deviceFound && !preview && len(servedImageIDs) > 0 {
		go func(devID uint, imgIDs []uint) {
			rows := make([]model.DeviceHistory, 0, len(imgIDs))
			now := time.Now()
			for _, imgID := range imgIDs {
				if imgID == 0 {
					continue
				}
				rows = append(rows, model.DeviceHistory{
					DeviceID: devID,
					ImageID:  imgID,
					ServedAt: now,
				})
			}
			if len(rows) == 0 {
				return
			}
			// Insert + prune in a single transaction so we acquire the
			// SQLite write lock once instead of three times. The prune
			// finds the served_at of the 51st-newest row and range-deletes
			// anything older; both halves hit the
			// idx_device_histories_device_served composite index, so this
			// stays O(log n) instead of the O(n) scan the previous
			// "NOT IN (subquery)" form degraded into.
			h.db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Create(&rows).Error; err != nil {
					return err
				}
				var cutoffs []time.Time
				if err := tx.Model(&model.DeviceHistory{}).
					Where("device_id = ?", devID).
					Order("served_at desc").
					Offset(50).
					Limit(1).
					Pluck("served_at", &cutoffs).Error; err != nil || len(cutoffs) == 0 {
					return nil
				}
				return tx.Where("device_id = ? AND served_at < ?", devID, cutoffs[0]).
					Delete(&model.DeviceHistory{}).Error
			})
		}(device.ID, servedImageIDs)
	}

	// Rotation-position chip: where the just-served photo sits in this frame's
	// rotation (computed from the in-flight served id so it's truthful before the
	// async DeviceHistory write). Empty for non-ordered sources / collage.
	rotationText, rotationIcon := "", ""
	if device.ShowRotation {
		curID := uint(0)
		if len(servedImageIDs) > 0 {
			curID = servedImageIDs[0]
		}
		rs := service.ComputeRotationStatus(h.db, &device, source, curID)
		rotationText, rotationIcon = service.FormatRotationOverlay(rs, device.RotationShowTotal)
	}
	showRotation := device.ShowRotation && rotationText != ""

	// 2. Render layout (photo + overlay + calendar)
	needsOverlay := showDate || showPhotoDate || showWeather || showCalendar || showBattery || showNames || showLocation || showDescription || showRotation
	var imgWithOverlay image.Image

	// People-names + location + description strings, formatted per device
	// settings from the served photo's metadata (Immich/gallery; empty else).
	namesStr := ""
	if showNames {
		namesStr = service.FormatPeople(sourceResp.PeopleJSON, photoTakenAt, device.NameFormat, device.NamesShowAge, device.NamesMaxLen)
	}
	locationStr := ""
	if showLocation {
		locationStr = service.FormatLocation(sourceResp.Location, device.LocationMaxLen)
	}
	descriptionStr := ""
	if showDescription {
		descriptionStr = service.FormatDescription(sourceResp.Description, device.DescriptionMaxLen)
	}

	if needsOverlay {
		var weatherData *weather.CurrentWeather
		var deviceTimezone string
		if showWeather && lat != 0 && lon != 0 {
			latStr := fmt.Sprintf("%f", lat)
			lonStr := fmt.Sprintf("%f", lon)
			var weatherErr error
			weatherData, weatherErr = h.weather.GetWeather(latStr, lonStr)
			if weatherErr != nil {
				log.Printf("Failed to fetch weather data: %v", weatherErr)
			}
			if weatherData != nil {
				deviceTimezone = weatherData.Timezone
			}
		}

		var events []gcalendar.Event
		if showCalendar && h.calendar != nil && h.calendarGoogle != nil {
			httpClient, err := h.calendarGoogle.GetClient()
			if err == nil {
				calendarID := device.CalendarID
				if calendarID == "" {
					calendarID = "primary"
				}
				var calErr error
				events, calErr = h.calendar.GetTodayEvents(httpClient, calendarID, deviceTimezone)
				if calErr != nil {
					log.Printf("Failed to fetch calendar events: %v", calErr)
				}
			}
		}

		var renderErr error
		imgWithOverlay, renderErr = h.renderer.Render(service.RenderOptions{
			Layout:              layout,
			DisplayMode:         displayMode,
			Width:               logicalW,
			Height:              logicalH,
			NativeWidth:         nativeW,
			NativeHeight:        nativeH,
			Photo:               img,
			ShowDate:            showDate,
			ShowPhotoDate:       showPhotoDate,
			PhotoDate:           photoTakenAt,
			ShowWeather:         showWeather,
			Weather:             weatherData,
			ShowCalendar:        showCalendar,
			Events:              events,
			Timezone:            deviceTimezone,
			DateFormat:          device.DateFormat,
			ShowBattery:         showBattery,
			BatteryPercent:      batteryPercent,
			DatePosition:        device.DatePosition,
			PhotoDatePosition:   device.PhotoDatePosition,
			WeatherPosition:     device.WeatherPosition,
			BatteryPosition:     device.BatteryPosition,
			BatteryStyle:        device.BatteryStyle,
			BatteryRotation:     device.BatteryRotation,
			BatteryTextSide:     device.BatteryTextSide,
			BatteryIconScale:    device.BatteryIconScale,
			OverlayScale:        device.OverlayScale,
			OverlayFont:         device.OverlayFont,
			OverlayWeight:       device.OverlayWeight,
			ShowNames:           showNames,
			Names:               namesStr,
			NamesPosition:       device.NamesPosition,
			ShowLocation:        showLocation,
			Location:            locationStr,
			LocationPosition:    device.LocationPosition,
			ShowDescription:     showDescription,
			Description:         descriptionStr,
			DescriptionPosition: device.DescriptionPosition,
			ShowRotation:        showRotation,
			RotationText:        rotationText,
			RotationIcon:        rotationIcon,
			RotationPosition:    device.RotationPosition,
			OverlayHiddenIcons:  device.OverlayHiddenIcons,
		})
		if renderErr != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "render failed: " + renderErr.Error()})
		}
	} else {
		imgWithOverlay = img
	}

	// 3. Tone Mapping + Thumbnail (CLI)
	// The overlay-composited image is in viewing orientation; pre-rotate it into
	// native panel layout (inverse of deg) so the CLI dithers straight to native
	// dimensions. This makes rotation a single deg-driven Go step instead of
	// relying on the CLI's opaque --orientation rotation. No-op at deg=0.
	imgWithOverlay = imageops.RotateDeg(imgWithOverlay, 360-deg)
	procOptions := map[string]string{
		"dimension": fmt.Sprintf("%dx%d", nativeW, nativeH),
	}

	// Determine output format based on firmware version (epdgz requires >= 2.6.1)
	firmwareVersion := c.Request().Header.Get("X-Firmware-Version")
	rawEPD := c.Request().Header.Get("X-EPD-Raw") == "1"
	if !rawEPD && (firmwareVersion == "" || !photoframe.SupportsEPDGZ(firmwareVersion)) {
		procOptions["format"] = "png"
	}

	// 3.5. Resolve processing settings.
	// The server-stored config is authoritative when present: it's what the
	// device dialog edits, and it matches what the push path renders with.
	// Relying on the device-sent X-Processing-Settings header instead would
	// render with stale values whenever config-sync hasn't reached the device
	// yet (timestamp gate) — so pull/button refreshes ignored the configured
	// preset. Fall back to the header for standalone/unmanaged devices that
	// have no server-stored config.
	var settings *photoframe.ProcessingSettings
	if deviceFound && device.DeviceProcessingSettings != "" && device.DeviceProcessingSettings != "{}" {
		settings = &photoframe.ProcessingSettings{}
		if err := json.Unmarshal([]byte(device.DeviceProcessingSettings), settings); err != nil {
			fmt.Printf("Failed to parse stored processing settings for device %d: %v\n", device.ID, err)
			settings = nil
		}
	}
	if settings == nil {
		if settingsStr := c.Request().Header.Get("X-Processing-Settings"); settingsStr != "" {
			settings = &photoframe.ProcessingSettings{}
			if err := json.Unmarshal([]byte(settingsStr), settings); err != nil {
				fmt.Printf("Failed to parse X-Processing-Settings header: %v\n", err)
				settings = nil
			}
		}
	}

	// 3.6. Resolve color palette (same server-authoritative rule as above).
	var palette *photoframe.Palette
	if deviceFound && device.DeviceColorPalette != "" && device.DeviceColorPalette != "{}" {
		palette = &photoframe.Palette{}
		if err := json.Unmarshal([]byte(device.DeviceColorPalette), palette); err != nil {
			fmt.Printf("Failed to parse stored color palette for device %d: %v\n", device.ID, err)
			palette = nil
		}
	}
	if palette == nil {
		if paletteStr := c.Request().Header.Get("X-Color-Palette"); paletteStr != "" {
			palette = &photoframe.Palette{}
			if err := json.Unmarshal([]byte(paletteStr), palette); err != nil {
				fmt.Printf("Failed to parse X-Color-Palette header: %v\n", err)
				palette = nil
			}
		}
	}

	headerOpts := h.processor.MapProcessingSettings(settings, palette)
	for k, v := range headerOpts {
		procOptions[k] = v
	}

	log.Println("Processing image with options: ", procOptions)
	processedBytes, _, err := h.processor.ProcessImage(imgWithOverlay, procOptions)
	if err != nil {
		fmt.Printf("Processor failed: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "processor service failed: " + err.Error()})
	}
	if rawEPD {
		gz, err := gzip.NewReader(bytes.NewReader(processedBytes))
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "raw epd gzip reader failed: " + err.Error()})
		}
		rawBytes, err := io.ReadAll(gz)
		gz.Close()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "raw epd decompress failed: " + err.Error()})
		}
		processedBytes = rawBytes
	}

	// 4. Cache Thumbnail & Set Headers.
	// Build the preview from the *processed* (dithered) output so it truthfully
	// shows grayscale / palette / tone. The converter's own -t thumbnail is
	// snapshotted pre-dither and ignores those filters.
	thumbFormat := "epdgz"
	if rawEPD {
		thumbFormat = "raw"
	} else if firmwareVersion == "" || !photoframe.SupportsEPDGZ(firmwareVersion) {
		thumbFormat = "png"
	}
	if previewJPEG, terr := imageops.ProcessedToThumbnailJPEG(processedBytes, thumbFormat, nativeW, nativeH, 400, deg); terr != nil {
		fmt.Printf("Failed to build preview thumbnail: %v\n", terr)
	} else {
		thumbID := fmt.Sprintf("%d", time.Now().UnixNano())
		thumbPath := filepath.Join(h.dataDir, fmt.Sprintf("thumb_%s.jpg", thumbID))
		if err := os.WriteFile(thumbPath, previewJPEG, 0644); err == nil {
			thumbnailUrl := fmt.Sprintf("http://%s/served-image-thumbnail/%s", c.Request().Host, thumbID)
			c.Response().Header().Set("X-Thumbnail-URL", thumbnailUrl)

			// Remember this as the frame's current image so the Devices list can
			// show a live miniature. Skip preview renders (they don't change what
			// the frame displays), and delete the previous per-device thumbnail so
			// these files don't pile up.
			if deviceFound && !preview {
				// Also write a full-resolution (un-downscaled) JPEG so the UI can
				// open the current image at native panel size — what the frame
				// actually shows. Served via /served-image-full/:id.
				if fullJPEG, ferr := imageops.ProcessedToThumbnailJPEG(processedBytes, thumbFormat, nativeW, nativeH, 0, deg); ferr != nil {
					fmt.Printf("Failed to build full preview: %v\n", ferr)
				} else if werr := os.WriteFile(filepath.Join(h.dataDir, fmt.Sprintf("full_%s.jpg", thumbID)), fullJPEG, 0644); werr != nil {
					fmt.Printf("Failed to save full preview: %v\n", werr)
				}
				// Rotate Previous ← Current ← new (keeps the demoted image as
				// "previous", deletes the one leaving the retained set).
				service.SetCurrentThumb(h.db, h.dataDir, &device, thumbID)

				// Post-serve hook: render the next image (non-mutating preview) and
				// notify the Home Assistant MQTT bridge once current_thumb_id is
				// committed. Async so it never delays the frame's response.
				if h.mqtt != nil {
					h.afterServe(device, source, servedImageIDs)
				}
			}
		} else {
			fmt.Printf("Failed to save served thumbnail: %v\n", err)
		}
	}

	// 5. Config Sync: push config payload if server has newer config
	applyConfigSyncHeader(c, &device, deviceFound)

	// Set Content-Length header
	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(processedBytes)))

	contentType := "application/octet-stream"
	if rawEPD {
		contentType = "application/x-epd-raw"
	} else if firmwareVersion == "" || !photoframe.SupportsEPDGZ(firmwareVersion) {
		contentType = "image/png"
	}
	return c.Blob(http.StatusOK, contentType, processedBytes)
}

// SyncDeviceConfig handles device config sync.
// The device POSTs its current config; the server stores it and returns its own
// config if it's newer.
// POST /api/device-config/sync
func (h *ImageHandler) SyncDeviceConfig(c echo.Context) error {
	// Identify device using same logic as ServeImage
	var device model.Device
	deviceFound := false

	if devID, ok := c.Get("device_id").(uint); ok && devID > 0 {
		if err := h.db.First(&device, devID).Error; err == nil {
			deviceFound = true
		}
	}
	if !deviceFound {
		if hostname := c.Request().Header.Get("X-Hostname"); hostname != "" {
			if err := h.db.Where("host = ?", hostname).First(&device).Error; err == nil {
				deviceFound = true
			}
		}
	}
	if !deviceFound {
		clientIP := c.RealIP()
		if err := h.db.Where("host = ?", clientIP).First(&device).Error; err == nil {
			deviceFound = true
		}
	}

	if !deviceFound {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	// Parse request body: { "config": {...}, "processing_settings": {...}, "color_palette": {...}, "config_last_updated": 123 }
	var req struct {
		Config             json.RawMessage `json:"config"`
		ProcessingSettings json.RawMessage `json:"processing_settings"`
		ColorPalette       json.RawMessage `json:"color_palette"`
		ConfigLastUpdated  int64           `json:"config_last_updated"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	// Store device's config in database
	updates := map[string]interface{}{}
	if len(req.Config) > 0 {
		updates["device_config"] = string(req.Config)
	}
	if len(req.ProcessingSettings) > 0 {
		updates["device_processing_settings"] = string(req.ProcessingSettings)
	}
	if len(req.ColorPalette) > 0 {
		updates["device_color_palette"] = string(req.ColorPalette)
	}
	if req.ConfigLastUpdated > 0 {
		updates["config_last_updated"] = req.ConfigLastUpdated
	}

	if len(updates) > 0 {
		h.db.Model(&device).Updates(updates)
	}

	// Return server's config if it's newer
	resp := map[string]interface{}{
		"status":              "synced",
		"config_last_updated": device.ConfigLastUpdated,
	}

	return c.JSON(http.StatusOK, resp)
}

// UpdateDeviceConfig updates the server-side device config (called from web UI).
// PUT /api/devices/:id/config
func (h *ImageHandler) UpdateDeviceConfig(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))

	var device model.Device
	if err := h.db.First(&device, uint(id)).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	var req struct {
		Config             json.RawMessage `json:"config"`
		ProcessingSettings json.RawMessage `json:"processing_settings"`
		ColorPalette       json.RawMessage `json:"color_palette"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	updates := map[string]interface{}{
		"config_last_updated": time.Now().Unix(),
	}

	// Merge the incoming config onto the last-synced device config rather than
	// replacing it. The web UI only sends the fields it knows how to edit, so a
	// plain replace silently drops device settings it doesn't manage
	// (http_header_*, sd_rotation_mode, ai_prompt, wifi_ssid, …) and — worse —
	// any firmware setting added after this save code was last touched. Merging
	// preserves every untouched key, so new fields survive a save even before
	// the UI learns about them. Sync-from-device re-pulls the full config, so
	// the merge base stays fresh.
	var configMap map[string]interface{}
	if device.DeviceConfig != "" && device.DeviceConfig != "{}" {
		json.Unmarshal([]byte(device.DeviceConfig), &configMap)
	}
	if configMap == nil {
		configMap = map[string]interface{}{}
	}
	if len(req.Config) > 0 {
		var incoming map[string]interface{}
		if err := json.Unmarshal(req.Config, &incoming); err == nil {
			for k, v := range incoming {
				configMap[k] = v
			}
		}
	}

	// If image_url points to this server, ensure a device token is included.
	if imageURL, ok := configMap["image_url"].(string); ok && strings.Contains(imageURL, "/image/") {
		if userID, ok := c.Get("user_id").(uint); ok {
			username, _ := c.Get("username").(string)
			token, err := h.auth.GetOrGenerateDeviceToken(userID, username, device.Name, &device.ID)
			if err == nil {
				configMap["access_token"] = token
			}
		}
	}

	if len(req.Config) > 0 || len(configMap) > 0 {
		if merged, err := json.Marshal(configMap); err == nil {
			updates["device_config"] = string(merged)
		}
	}

	// Mirror Display Rotation into its dedicated column so the render pipeline and
	// previews (image.go / device.go) can read it without parsing device_config.
	// JSON numbers unmarshal as float64. Keep the legacy orientation column in
	// sync (derived) for any consumer still reading it.
	if v, ok := configMap["display_rotation_deg"]; ok {
		if f, ok := v.(float64); ok {
			deg := model.NormalizeRotationDeg(int(f))
			updates["display_rotation_deg"] = deg
			nw, nh := device.Width, device.Height
			if lw, lh := imageops.LogicalDims(nw, nh, deg); lw >= lh {
				updates["orientation"] = "landscape"
			} else {
				updates["orientation"] = "portrait"
			}
		}
	}
	if len(req.ProcessingSettings) > 0 {
		updates["device_processing_settings"] = string(req.ProcessingSettings)
	}
	if len(req.ColorPalette) > 0 {
		updates["device_color_palette"] = string(req.ColorPalette)
	}

	h.db.Model(&device).Updates(updates)

	// Push the saved settings to the device directly so an awake frame updates
	// its NVS immediately — keeping the device's own webapp and the dialog's
	// "Sync from device" truthful. Each kind goes to its matching endpoint:
	// config is pushed as the full merged map (so the device receives every
	// field, not just the edited ones); processing settings and palette are
	// pushed as the raw saved JSON. Anything that doesn't reach an asleep frame
	// still rides the config-sync header on its next image fetch, and rendering
	// is already server-authoritative regardless. A single failure (device
	// asleep/unreachable) marks the whole save "offline".
	pushResult := "synced"
	if device.Host != "" {
		client := photoframe.NewClient(device.Host)
		if len(req.Config) > 0 && len(configMap) > 0 {
			if err := client.PushConfig(configMap); err != nil {
				log.Printf("Could not push config to device %s: %v (will sync on next image fetch)", device.Host, err)
				pushResult = "offline"
			}
		}
		if len(req.ProcessingSettings) > 0 {
			if err := client.PushProcessingSettings(req.ProcessingSettings); err != nil {
				log.Printf("Could not push processing settings to device %s: %v (will sync on next image fetch)", device.Host, err)
				pushResult = "offline"
			}
		}
		if len(req.ColorPalette) > 0 {
			if err := client.PushPalette(req.ColorPalette); err != nil {
				log.Printf("Could not push palette to device %s: %v (will sync on next image fetch)", device.Host, err)
				pushResult = "offline"
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":              "updated",
		"push_result":         pushResult,
		"config_last_updated": updates["config_last_updated"],
	})
}

// GetDeviceConfig returns the server-side device config.
// GET /api/devices/:id/config
func (h *ImageHandler) GetDeviceConfig(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))

	var device model.Device
	if err := h.db.First(&device, uint(id)).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	resp := map[string]interface{}{
		"config_last_updated": device.ConfigLastUpdated,
	}

	if device.DeviceConfig != "" && device.DeviceConfig != "{}" {
		resp["config"] = json.RawMessage(device.DeviceConfig)
	}
	if device.DeviceProcessingSettings != "" && device.DeviceProcessingSettings != "{}" {
		resp["processing_settings"] = json.RawMessage(device.DeviceProcessingSettings)
	}
	if device.DeviceColorPalette != "" && device.DeviceColorPalette != "{}" {
		resp["color_palette"] = json.RawMessage(device.DeviceColorPalette)
	}

	return c.JSON(http.StatusOK, resp)
}

// ListSources returns the names of all registered image sources, so the web UI
// can offer them when switching a device's source. GET /api/sources
func (h *ImageHandler) ListSources(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{"sources": h.sources.Names()})
}

// buildConfigPayload builds the X-Config-Payload JSON from device's stored config.
// Returns empty string if there's nothing to send.
func buildConfigPayload(device *model.Device) string {
	payload := map[string]json.RawMessage{}

	if device.DeviceConfig != "" && device.DeviceConfig != "{}" {
		payload["config"] = json.RawMessage(device.DeviceConfig)
	}
	if device.DeviceProcessingSettings != "" && device.DeviceProcessingSettings != "{}" {
		payload["processing_settings"] = json.RawMessage(device.DeviceProcessingSettings)
	}
	if device.DeviceColorPalette != "" && device.DeviceColorPalette != "{}" {
		payload["color_palette"] = json.RawMessage(device.DeviceColorPalette)
	}

	if len(payload) == 0 {
		return ""
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func (h *ImageHandler) GetServedImageThumbnail(c echo.Context) error {
	id := c.Param("id")
	// Prevent directory traversal
	if id == "" || id == "." || id == ".." {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	thumbPath := filepath.Join(h.dataDir, fmt.Sprintf("thumb_%s.jpg", id))
	data, err := os.ReadFile(thumbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "thumbnail not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read thumbnail"})
	}

	// Delete after 5 minutes instead of immediately
	go func() {
		time.Sleep(5 * time.Minute)
		os.Remove(thumbPath)
	}()

	// Set Content-Length header
	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	return c.Blob(http.StatusOK, "image/jpeg", data)
}

// GetServedImageFull serves the full-resolution (native panel size) JPEG of the
// frame's current image — what the UI shows when the Devices-list miniature is
// clicked. Unlike the thumbnail it is not auto-deleted on access, so the
// lightbox keeps working; it is cleaned up when the device's current image
// changes (see ServeImage / PushToHost).
func (h *ImageHandler) GetServedImageFull(c echo.Context) error {
	id := c.Param("id")
	// Prevent directory traversal
	if id == "" || id == "." || id == ".." {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	fullPath := filepath.Join(h.dataDir, fmt.Sprintf("full_%s.jpg", id))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "image not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read image"})
	}

	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	return c.Blob(http.StatusOK, "image/jpeg", data)
}

// applyConfigSyncHeader sets the X-Config-Payload response header when the
// server's stored device config is newer than what the device most recently
// reported. Pulled out so both the bypass branch and the main flow share it.
func applyConfigSyncHeader(c echo.Context, device *model.Device, deviceFound bool) {
	if !deviceFound || device.ConfigLastUpdated <= 0 {
		return
	}
	deviceConfigTS := int64(0)
	if tsStr := c.Request().Header.Get("X-Config-Last-Updated"); tsStr != "" {
		if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			deviceConfigTS = ts
		}
	}
	if device.ConfigLastUpdated <= deviceConfigTS {
		return
	}
	payload := buildConfigPayload(device)
	if payload == "" {
		return
	}
	c.Response().Header().Set("X-Config-Payload", payload)
	log.Printf("Config sync: pushing config to device (server=%d, device=%d)",
		device.ConfigLastUpdated, deviceConfigTS)
}
