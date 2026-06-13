package service

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/gcalendar"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/googlephotos"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/imageops"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/photoframe"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/weather"
	"gorm.io/gorm"
)

type DeviceServiceDeps struct {
	DB             *gorm.DB
	Settings       *SettingsService
	Processor      *ProcessorService
	Renderer       *RendererService
	Weather        *weather.Client
	Calendar       *gcalendar.Client
	CalendarGoogle *googlephotos.Client
	DataDir        string
}

type DeviceService struct {
	db             *gorm.DB
	settings       *SettingsService
	processor      *ProcessorService
	renderer       *RendererService
	weather        *weather.Client
	calendar       *gcalendar.Client
	calendarGoogle *googlephotos.Client
	dataDir        string
}

func NewDeviceService(deps DeviceServiceDeps) *DeviceService {
	return &DeviceService{
		db:             deps.DB,
		settings:       deps.Settings,
		processor:      deps.Processor,
		renderer:       deps.Renderer,
		weather:        deps.Weather,
		calendar:       deps.Calendar,
		calendarGoogle: deps.CalendarGoogle,
		dataDir:        deps.DataDir,
	}
}

// --- CRUD Operations ---

func (s *DeviceService) ListDevices() ([]model.Device, error) {
	var devices []model.Device
	if err := s.db.Find(&devices).Error; err != nil {
		return nil, err
	}
	return devices, nil
}

func (s *DeviceService) AddDevice(host string, enableCollage, showDate, showPhotoDate, showWeather bool, weatherLat, weatherLon float64, layout string, displayMode string, showCalendar bool, calendarID string, dateFormat string, showBattery bool, displayOrder string, immichAlbumIDs string, overlay model.OverlaySettings) (*model.Device, error) {
	// Try to fetch device info (works on LAN, fails for remote devices)
	var name string
	var width, height int
	var orientation, boardName string
	rotationDeg := 0
	httpsSupported := true // assume capable unless the device reports otherwise

	var deviceConfig, deviceProc, devicePalette string

	pfClient := photoframe.NewClient(host)
	sysInfo, err := pfClient.FetchSystemInfo()
	if err != nil {
		log.Printf("Could not reach device at %s (may be remote): %v", host, err)
		// Use defaults for unreachable devices; dimensions will be updated on first image request
		name = host
		width = 800
		height = 480
		orientation = "landscape"
	} else {
		name = sysInfo.DeviceName
		width = sysInfo.Width
		height = sysInfo.Height
		boardName = sysInfo.BoardName
		if sysInfo.HTTPSSupported != nil {
			httpsSupported = *sysInfo.HTTPSSupported
		}

		configRaw, cfgErr := pfClient.FetchConfig()
		if cfgErr == nil {
			deviceConfig = configRaw
			var parsed struct {
				DisplayOrientation string `json:"display_orientation"`
				DisplayRotationDeg int    `json:"display_rotation_deg"`
			}
			if json.Unmarshal([]byte(configRaw), &parsed) == nil {
				if parsed.DisplayOrientation != "" {
					orientation = parsed.DisplayOrientation
				}
				rotationDeg = model.NormalizeRotationDeg(parsed.DisplayRotationDeg)
			}
		}

		if procRaw, err := pfClient.FetchProcessingSettings(); err == nil {
			deviceProc = procRaw
		}
		if paletteRaw, err := pfClient.FetchPalette(); err == nil {
			devicePalette = paletteRaw
		}
	}

	if name == "" {
		name = host
	}
	if width == 0 || height == 0 {
		width = 800
		height = 480
	}
	if orientation == "" {
		orientation = "landscape"
	}

	if displayMode == "" {
		displayMode = "cover"
	}

	device := &model.Device{
		Name:                     name,
		Host:                     host,
		Width:                    width,
		Height:                   height,
		Orientation:              orientation,
		DisplayRotationDeg:       rotationDeg,
		BoardName:                boardName,
		HTTPSSupported:           httpsSupported,
		EnableCollage:            enableCollage,
		ShowDate:                 showDate,
		ShowPhotoDate:            showPhotoDate,
		ShowWeather:              showWeather,
		WeatherLat:               weatherLat,
		WeatherLon:               weatherLon,
		Layout:                   layout,
		DisplayMode:              displayMode,
		ShowCalendar:             showCalendar,
		CalendarID:               calendarID,
		DateFormat:               dateFormat,
		ShowBattery:              showBattery,
		DisplayOrder:             model.NormalizeDisplayOrder(displayOrder),
		ImmichAlbumIDs:           model.NormalizeImmichAlbumIDs(immichAlbumIDs),
		DatePosition:             model.NormalizeOverlayPosition(overlay.DatePosition, "bottom-left"),
		PhotoDatePosition:        model.NormalizeOverlayPosition(overlay.PhotoDatePosition, "bottom-left"),
		WeatherPosition:          model.NormalizeOverlayPosition(overlay.WeatherPosition, "bottom-right"),
		BatteryPosition:          model.NormalizeOverlayPosition(overlay.BatteryPosition, "top-right"),
		BatteryStyle:             model.NormalizeBatteryStyle(overlay.BatteryStyle),
		BatteryRotation:          model.NormalizeBatteryRotation(overlay.BatteryRotation),
		BatteryTextSide:          model.NormalizeBatteryTextSide(overlay.BatteryTextSide),
		BatteryIconScale:         model.NormalizeBatteryIconScale(overlay.BatteryIconScale),
		OverlayScale:             model.NormalizeOverlayScale(overlay.OverlayScale),
		OverlayFont:              model.NormalizeOverlayFont(overlay.OverlayFont),
		OverlayWeight:            model.NormalizeOverlayWeight(overlay.OverlayWeight),
		ShowNames:                overlay.ShowNames,
		NamesPosition:            model.NormalizeOverlayPosition(overlay.NamesPosition, "top-left"),
		NameFormat:               model.NormalizeNameFormat(overlay.NameFormat),
		NamesShowAge:             overlay.NamesShowAge,
		NamesMaxLen:              model.NormalizeNamesMaxLen(overlay.NamesMaxLen),
		ShowLocation:             overlay.ShowLocation,
		LocationPosition:         model.NormalizeOverlayPosition(overlay.LocationPosition, "bottom-center"),
		LocationMaxLen:           model.NormalizeLocationMaxLen(overlay.LocationMaxLen),
		ShowDescription:          overlay.ShowDescription,
		DescriptionPosition:      model.NormalizeOverlayPosition(overlay.DescriptionPosition, "wide-bottom"),
		DescriptionMaxLen:        model.NormalizeDescriptionMaxLen(overlay.DescriptionMaxLen),
		OverlayHiddenIcons:       model.NormalizeOverlayHiddenIcons(overlay.OverlayHiddenIcons),
		DeviceConfig:             deviceConfig,
		DeviceProcessingSettings: deviceProc,
		DeviceColorPalette:       devicePalette,
	}
	if err := s.db.Create(device).Error; err != nil {
		return nil, err
	}
	return device, nil
}

// UpdateDevice writes only fields the server owns or shares with the device
// (Name, Host, Orientation, and the render/overlay settings). It never
// contacts the device, so offline edits succeed — shared fields (Name,
// Orientation) propagate to the device via the separate updateDeviceConfig
// path (push-if-online, else X-Config-Payload on next fetch).
//
// Hardware-derived fields (Width, Height, BoardName, DeviceConfig,
// DeviceProcessingSettings, DeviceColorPalette) are only written by
// AddDevice and RefreshDeviceFromHardware.
func (s *DeviceService) UpdateDevice(id uint, name, host, orientation string, enableCollage, showDate, showPhotoDate, showWeather bool, weatherLat, weatherLon float64, aiProvider, aiModel, aiPrompt string, layout string, displayMode string, showCalendar bool, calendarID string, dateFormat string, showBattery bool, displayOrder string, immichAlbumIDs string, overlay model.OverlaySettings) (*model.Device, error) {
	var device model.Device
	if err := s.db.First(&device, id).Error; err != nil {
		return nil, errors.New("device not found")
	}

	if name == "" {
		name = device.Name // Keep existing if blank
	}
	if name == "" {
		name = host // Final fallback
	}
	if orientation == "" {
		orientation = device.Orientation
	}
	if displayMode == "" {
		displayMode = "cover"
	}

	device.Name = name
	device.Host = host
	device.Orientation = orientation
	device.EnableCollage = enableCollage
	device.ShowDate = showDate
	device.ShowPhotoDate = showPhotoDate
	device.ShowWeather = showWeather
	device.WeatherLat = weatherLat
	device.WeatherLon = weatherLon
	device.AIProvider = aiProvider
	device.AIModel = aiModel
	device.AIPrompt = aiPrompt
	device.Layout = layout
	device.DisplayMode = displayMode
	device.ShowCalendar = showCalendar
	device.CalendarID = calendarID
	device.DateFormat = dateFormat
	device.ShowBattery = showBattery
	device.DisplayOrder = model.NormalizeDisplayOrder(displayOrder)
	device.ImmichAlbumIDs = model.NormalizeImmichAlbumIDs(immichAlbumIDs)
	device.DatePosition = model.NormalizeOverlayPosition(overlay.DatePosition, "bottom-left")
	device.PhotoDatePosition = model.NormalizeOverlayPosition(overlay.PhotoDatePosition, "bottom-left")
	device.WeatherPosition = model.NormalizeOverlayPosition(overlay.WeatherPosition, "bottom-right")
	device.BatteryPosition = model.NormalizeOverlayPosition(overlay.BatteryPosition, "top-right")
	device.BatteryStyle = model.NormalizeBatteryStyle(overlay.BatteryStyle)
	device.BatteryRotation = model.NormalizeBatteryRotation(overlay.BatteryRotation)
	device.BatteryTextSide = model.NormalizeBatteryTextSide(overlay.BatteryTextSide)
	device.BatteryIconScale = model.NormalizeBatteryIconScale(overlay.BatteryIconScale)
	device.OverlayScale = model.NormalizeOverlayScale(overlay.OverlayScale)
	device.OverlayFont = model.NormalizeOverlayFont(overlay.OverlayFont)
	device.OverlayWeight = model.NormalizeOverlayWeight(overlay.OverlayWeight)
	device.ShowNames = overlay.ShowNames
	device.NamesPosition = model.NormalizeOverlayPosition(overlay.NamesPosition, "top-left")
	device.NameFormat = model.NormalizeNameFormat(overlay.NameFormat)
	device.NamesShowAge = overlay.NamesShowAge
	device.NamesMaxLen = model.NormalizeNamesMaxLen(overlay.NamesMaxLen)
	device.ShowLocation = overlay.ShowLocation
	device.LocationPosition = model.NormalizeOverlayPosition(overlay.LocationPosition, "bottom-center")
	device.LocationMaxLen = model.NormalizeLocationMaxLen(overlay.LocationMaxLen)
	device.ShowDescription = overlay.ShowDescription
	device.DescriptionPosition = model.NormalizeOverlayPosition(overlay.DescriptionPosition, "wide-bottom")
	device.DescriptionMaxLen = model.NormalizeDescriptionMaxLen(overlay.DescriptionMaxLen)
	device.OverlayHiddenIcons = model.NormalizeOverlayHiddenIcons(overlay.OverlayHiddenIcons)

	if err := s.db.Save(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// RefreshDeviceFromHardware pulls live state from the device (dimensions,
// board name, config, processing settings, palette) and writes it onto the
// stored row. Unlike UpdateDevice this requires the device to be reachable
// and returns an error if any of the critical fetches fail.
func (s *DeviceService) RefreshDeviceFromHardware(id uint) (*model.Device, error) {
	var device model.Device
	if err := s.db.First(&device, id).Error; err != nil {
		return nil, errors.New("device not found")
	}

	pfClient := photoframe.NewClient(device.Host)

	sysInfo, err := pfClient.FetchSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch system info: %w", err)
	}
	if sysInfo.DeviceName != "" {
		device.Name = sysInfo.DeviceName
	}
	device.Width = sysInfo.Width
	device.Height = sysInfo.Height
	if sysInfo.BoardName != "" {
		device.BoardName = sysInfo.BoardName
	}
	if sysInfo.HTTPSSupported != nil {
		device.HTTPSSupported = *sysInfo.HTTPSSupported
	}

	configRaw, err := pfClient.FetchConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch device config: %w", err)
	}
	device.DeviceConfig = configRaw
	var parsedConfig struct {
		DisplayOrientation string `json:"display_orientation"`
		DisplayRotationDeg int    `json:"display_rotation_deg"`
	}
	if json.Unmarshal([]byte(configRaw), &parsedConfig) == nil {
		if parsedConfig.DisplayOrientation != "" {
			device.Orientation = parsedConfig.DisplayOrientation
		}
		device.DisplayRotationDeg = model.NormalizeRotationDeg(parsedConfig.DisplayRotationDeg)
	}

	if procRaw, err := pfClient.FetchProcessingSettings(); err != nil {
		log.Printf("Failed to fetch processing settings from %s: %v", device.Host, err)
	} else {
		device.DeviceProcessingSettings = procRaw
	}

	if paletteRaw, err := pfClient.FetchPalette(); err != nil {
		log.Printf("Failed to fetch palette from %s: %v", device.Host, err)
	} else {
		device.DeviceColorPalette = paletteRaw
	}

	if err := s.db.Save(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

func (s *DeviceService) DeleteDevice(id uint) error {
	result := s.db.Delete(&model.Device{}, id)
	return result.Error
}

// --- Push Logic ---

// PushToDevice resolves a device ID to a host and pushes the image.
// photoTakenAt (may be nil) feeds the photo-date overlay; peopleJSON/location
// (may be empty) feed the names/location overlays.
func (s *DeviceService) PushToDevice(deviceID uint, imagePath string, photoTakenAt *time.Time, peopleJSON, location, description string) error {
	var device model.Device
	if err := s.db.First(&device, deviceID).Error; err != nil {
		return errors.New("device not found")
	}

	if err := s.PushToHost(&device, imagePath, nil, photoTakenAt, peopleJSON, location, description); err != nil {
		return err
	}

	return nil
}

// PushToHost processes an image file and pushes it to a target host
// This encapsulates the logic previously in Telegram bot
// Now includes fetching device parameters if configured
func (s *DeviceService) PushToHost(device *model.Device, imagePath string, extraOpts map[string]string, photoTakenAt *time.Time, peopleJSON, location, description string) error {
	// 0. Fetch system info to determine firmware version and optionally device parameters
	processingOpts := make(map[string]string)
	for k, v := range extraOpts {
		processingOpts[k] = v
	}

	// Always fetch system info for firmware version check
	pfClient := photoframe.NewClient(device.Host)
	sysInfo, sysInfoErr := pfClient.FetchSystemInfo()
	if sysInfoErr != nil {
		log.Printf("Failed to fetch system info for %s: %v", device.Name, sysInfoErr)
	}

	// Decide output/transport based on what the device can handle.
	// SRAM-only boards (no persistent storage) cannot inflate EPDGZ or buffer a
	// multipart upload, so we push raw 4bpp EPD bytes. They require EPDGZ output
	// from the CLI (which we then decompress), so do NOT force PNG for them.
	rawEPD := sysInfoErr == nil && sysInfo.IsRawEPDOnly()
	if rawEPD {
		delete(processingOpts, "format")
	} else if sysInfoErr != nil || !photoframe.SupportsEPDGZ(sysInfo.Version) {
		// Use PNG for older firmware that doesn't support epdgz
		processingOpts["format"] = "png"
	}

	// 1. Validate dimensions. Native dims are the baseline; the viewing
	// (logical) dims derive from the frame's Display Rotation.
	nativeW, nativeH := device.Width, device.Height
	if nativeW == 0 || nativeH == 0 {
		nativeW, nativeH = 800, 480
	}
	deg := model.NormalizeRotationDeg(device.DisplayRotationDeg)
	logicalW, logicalH := imageops.LogicalDims(nativeW, nativeH, deg)

	// 2. Open file
	f, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer f.Close()

	// 3. Decode
	srcImg, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}

	// 5. Render layout (photo + overlay + calendar)
	// The pull path reads the battery level from the device's
	// X-Battery-Percentage header; a server-initiated push has no such header,
	// so query the device directly (it's online — we're pushing to it).
	batteryPercent := -1
	if device.ShowBattery {
		if bat, batErr := pfClient.FetchBattery(); batErr != nil {
			log.Printf("Failed to fetch battery for device %d: %v", device.ID, batErr)
		} else {
			batteryPercent = bat.BatteryLevel
		}
	}
	showBattery := device.ShowBattery && batteryPercent >= 0 && batteryPercent <= 100

	showNames := device.ShowNames
	showLocation := device.ShowLocation
	namesStr := ""
	if showNames {
		namesStr = FormatPeople(peopleJSON, photoTakenAt, device.NameFormat, device.NamesShowAge, device.NamesMaxLen)
	}
	locationStr := ""
	if showLocation {
		locationStr = FormatLocation(location, device.LocationMaxLen)
	}
	showDescription := device.ShowDescription
	descriptionStr := ""
	if showDescription {
		descriptionStr = FormatDescription(description, device.DescriptionMaxLen)
	}

	needsOverlay := device.ShowDate || device.ShowPhotoDate || device.ShowWeather || device.ShowCalendar || showBattery || showNames || showLocation || showDescription
	var finalImg image.Image

	if needsOverlay {
		var weatherData *weather.CurrentWeather
		var deviceTimezone string
		if device.ShowWeather && device.WeatherLat != 0 && device.WeatherLon != 0 {
			latStr := fmt.Sprintf("%f", device.WeatherLat)
			lonStr := fmt.Sprintf("%f", device.WeatherLon)
			var weatherErr error
			weatherData, weatherErr = s.weather.GetWeather(latStr, lonStr)
			if weatherErr != nil {
				log.Printf("Failed to fetch weather data for device %d: %v", device.ID, weatherErr)
			}
			if weatherData != nil {
				deviceTimezone = weatherData.Timezone
			}
		}

		var events []gcalendar.Event
		if device.ShowCalendar && s.calendar != nil && s.calendarGoogle != nil {
			httpClient, err := s.calendarGoogle.GetClient()
			if err == nil {
				calendarID := device.CalendarID
				if calendarID == "" {
					calendarID = "primary"
				}
				var calErr error
				events, calErr = s.calendar.GetTodayEvents(httpClient, calendarID, deviceTimezone)
				if calErr != nil {
					log.Printf("Failed to fetch calendar events for device %d: %v", device.ID, calErr)
				}
			}
		}

		layout := device.Layout
		if layout == "" {
			layout = model.LayoutPhotoOverlay
		}
		displayMode := device.DisplayMode
		if displayMode == "" {
			displayMode = "cover"
		}

		var renderErr error
		finalImg, renderErr = s.renderer.Render(RenderOptions{
			Layout:              layout,
			DisplayMode:         displayMode,
			Width:               logicalW,
			Height:              logicalH,
			NativeWidth:         nativeW,
			NativeHeight:        nativeH,
			Photo:               srcImg,
			ShowDate:            device.ShowDate,
			ShowPhotoDate:       device.ShowPhotoDate,
			PhotoDate:           photoTakenAt,
			ShowWeather:         device.ShowWeather,
			Weather:             weatherData,
			ShowCalendar:        device.ShowCalendar,
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
			OverlayHiddenIcons:  device.OverlayHiddenIcons,
		})
		if renderErr != nil {
			return fmt.Errorf("render failed: %w", renderErr)
		}
	} else {
		finalImg = srcImg
	}

	// 6. Process for E-Paper
	// finalImg is in viewing orientation; pre-rotate it into native panel layout
	// (inverse of deg) so the CLI dithers straight to native dimensions. Rotation
	// is a single deg-driven Go step (mirrors the pull path in image.go). No-op
	// at deg=0.
	finalImg = imageops.RotateDeg(finalImg, 360-deg)
	opts := map[string]string{
		"dimension": fmt.Sprintf("%dx%d", nativeW, nativeH),
	}

	// Merge extra options (device params)
	for k, v := range processingOpts {
		opts[k] = v
	}

	// Apply the device's saved processing settings and color palette so a
	// server-initiated push renders identically to the device's own pull path
	// (image.go reads these from the X-Processing-Settings / X-Color-Palette
	// request headers). Without this, pushes always used library defaults and
	// silently ignored the configured preset and palette.
	if device.DeviceProcessingSettings != "" && device.DeviceProcessingSettings != "{}" {
		var settings photoframe.ProcessingSettings
		if err := json.Unmarshal([]byte(device.DeviceProcessingSettings), &settings); err != nil {
			log.Printf("Failed to parse processing settings for device %d: %v", device.ID, err)
		} else {
			var palette *photoframe.Palette
			if device.DeviceColorPalette != "" && device.DeviceColorPalette != "{}" {
				palette = &photoframe.Palette{}
				if err := json.Unmarshal([]byte(device.DeviceColorPalette), palette); err != nil {
					log.Printf("Failed to parse color palette for device %d: %v", device.ID, err)
					palette = nil
				}
			}
			for k, v := range s.processor.MapProcessingSettings(&settings, palette) {
				opts[k] = v
			}
		}
	}

	processedData, thumbData, err := s.processor.ProcessImage(finalImg, opts)
	if err != nil {
		return fmt.Errorf("processing failed: %w", err)
	}

	// Record the rendered image as the frame's current thumbnail so the Devices
	// list shows what a server-initiated push put on the frame (the pull path
	// does the same in image.go). Built from the *processed* (dithered) output
	// so it truthfully reflects grayscale / palette / tone. Served publicly via
	// /served-image-thumbnail/:id.
	if s.dataDir != "" {
		thumbFormat := "epdgz"
		if opts["format"] == "png" {
			thumbFormat = "png"
		}
		if previewJPEG, terr := imageops.ProcessedToThumbnailJPEG(processedData, thumbFormat, nativeW, nativeH, 400, deg); terr != nil {
			log.Printf("Failed to build current thumbnail for device %d: %v", device.ID, terr)
		} else {
			thumbID := fmt.Sprintf("%d", time.Now().UnixNano())
			thumbPath := filepath.Join(s.dataDir, fmt.Sprintf("thumb_%s.jpg", thumbID))
			if writeErr := os.WriteFile(thumbPath, previewJPEG, 0644); writeErr == nil {
				// Also write a full-resolution JPEG (native panel size) so the UI
				// can open what the frame actually shows. Served via
				// /served-image-full/:id.
				if fullJPEG, ferr := imageops.ProcessedToThumbnailJPEG(processedData, thumbFormat, nativeW, nativeH, 0, deg); ferr != nil {
					log.Printf("Failed to build full preview for device %d: %v", device.ID, ferr)
				} else if werr := os.WriteFile(filepath.Join(s.dataDir, fmt.Sprintf("full_%s.jpg", thumbID)), fullJPEG, 0644); werr != nil {
					log.Printf("Failed to save full preview for device %d: %v", device.ID, werr)
				}
				// Rotate Previous ← Current ← new (and clean up the demoted file).
				SetCurrentThumb(s.db, s.dataDir, device, thumbID)
			} else {
				log.Printf("Failed to save current thumbnail for device %d: %v", device.ID, writeErr)
			}
		}
	}

	if rawEPD {
		// CLI produced EPDGZ; decompress to the raw 4bpp panel bytes the
		// SRAM-only device streams directly into its framebuffer.
		rawData, err := gunzipBytes(processedData)
		if err != nil {
			return fmt.Errorf("failed to decompress EPD for raw push: %w", err)
		}
		if err := pfClient.PushRawEPD(rawData); err != nil {
			return fmt.Errorf("failed to push to device: %w", err)
		}
		return nil
	}

	if err := pfClient.PushImage(processedData, thumbData); err != nil {
		return fmt.Errorf("failed to push to device: %w", err)
	}

	return nil
}

// gunzipBytes decompresses a gzip (EPDGZ) byte slice in full.
func gunzipBytes(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}
