package handler

import (
	"encoding/json"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/imagesource"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/service"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/gcalendar"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/imageops"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/photoframe"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/weather"
)

func (h *ImageHandler) supportsNextPreview(device *model.Device, source string) bool {
	return device != nil && !device.EnableCollage && model.IsOrderedSource(source)
}

// afterServe runs the post-serve hook (async): render the next image as a
// non-mutating preview, store it as the device's next thumbnail, then notify the
// Home Assistant MQTT bridge. Current_thumb_id is already committed by the
// caller, so "Current Image" published here is exactly what the frame shows.
// device is a value copy taken after the Previous←Current rotation.
func (h *ImageHandler) afterServe(device model.Device, source string, servedImageIDs []uint) {
	go func() {
		if h.supportsNextPreview(&device, source) && len(servedImageIDs) > 0 {
			justServed := servedImageIDs[len(servedImageIDs)-1]
			if jpeg, err := h.renderNextThumbnail(device.ID, source, justServed); err != nil {
				log.Printf("Failed to render next-image preview for device %d: %v", device.ID, err)
			} else {
				nextID := fmt.Sprintf("%d", time.Now().UnixNano())
				if werr := os.WriteFile(filepath.Join(h.dataDir, fmt.Sprintf("thumb_%s.jpg", nextID)), jpeg, 0644); werr != nil {
					log.Printf("Failed to save next-image thumbnail for device %d: %v", device.ID, werr)
				} else {
					// device still carries the previously-stored next id (loaded at
					// request start), so the cleanup guard removes the stale render.
					service.SetNextThumb(h.db, h.dataDir, &device, nextID)
				}
			}
		}
		// Reloads the device + publishes fresh state/images to HA.
		h.mqtt.NotifyDeviceUpdated(device.ID)
	}()
}

// renderNextThumbnail renders a 400px JPEG thumbnail of the next image a device
// will be served, without advancing its display-order cursor. It mirrors the
// serve pipeline (fetch → overlay → rotate → dither → thumbnail) but is driven
// entirely by server-stored device config (no request headers): it runs async,
// long after the HTTP request is gone. The battery badge is omitted — the
// battery level at the next display is unknown — and the dither is forced to the
// PNG path so the thumbnail is built the same way regardless of frame firmware.
func (h *ImageHandler) renderNextThumbnail(deviceID uint, source string, justServedID uint) ([]byte, error) {
	var device model.Device
	if err := h.db.First(&device, deviceID).Error; err != nil {
		return nil, err
	}

	nativeW, nativeH := device.Width, device.Height
	if nativeW == 0 || nativeH == 0 {
		nativeW, nativeH = 800, 480
	}
	deg := model.NormalizeRotationDeg(device.DisplayRotationDeg)
	logicalW, logicalH := imageops.LogicalDims(nativeW, nativeH, deg)
	orientation := "portrait"
	if logicalW >= logicalH {
		orientation = "landscape"
	}

	if !h.sources.Has(source) {
		return nil, fmt.Errorf("invalid source %q", source)
	}
	sourceResp, err := h.sources.Fetch(source, &imagesource.Request{
		Device:             &device,
		Source:             source,
		Width:              logicalW,
		Height:             logicalH,
		NativeWidth:        nativeW,
		NativeHeight:       nativeH,
		Orientation:        orientation,
		Preview:            true,
		LastServedOverride: justServedID,
	})
	if err != nil {
		return nil, err
	}
	img := sourceResp.Image

	// Overlay (same elements as the serve path, minus the battery badge).
	imgWithOverlay := h.composeNextOverlay(&device, source, img, sourceResp, logicalW, logicalH, nativeW, nativeH, deg)

	// Rotate into native panel layout, then dither via the PNG path.
	imgWithOverlay = imageops.RotateDeg(imgWithOverlay, 360-deg)
	procOptions := map[string]string{
		"dimension": fmt.Sprintf("%dx%d", nativeW, nativeH),
		"format":    "png",
	}
	var settings *photoframe.ProcessingSettings
	if device.DeviceProcessingSettings != "" && device.DeviceProcessingSettings != "{}" {
		settings = &photoframe.ProcessingSettings{}
		if err := json.Unmarshal([]byte(device.DeviceProcessingSettings), settings); err != nil {
			settings = nil
		}
	}
	var palette *photoframe.Palette
	if device.DeviceColorPalette != "" && device.DeviceColorPalette != "{}" {
		palette = &photoframe.Palette{}
		if err := json.Unmarshal([]byte(device.DeviceColorPalette), palette); err != nil {
			palette = nil
		}
	}
	for k, v := range h.processor.MapProcessingSettings(settings, palette) {
		procOptions[k] = v
	}

	processedBytes, _, err := h.processor.ProcessImage(imgWithOverlay, procOptions)
	if err != nil {
		return nil, fmt.Errorf("processor failed: %w", err)
	}
	return imageops.ProcessedToThumbnailJPEG(processedBytes, "png", nativeW, nativeH, 400, deg)
}

// composeNextOverlay renders the device's overlay elements onto the next image.
// Returns the raw photo unchanged when no overlay is enabled.
func (h *ImageHandler) composeNextOverlay(device *model.Device, source string, img image.Image, sourceResp *imagesource.Response, logicalW, logicalH, nativeW, nativeH, deg int) image.Image {
	showDate := device.ShowDate
	showPhotoDate := device.ShowPhotoDate
	showWeather := device.ShowWeather
	showNames := device.ShowNames
	showLocation := device.ShowLocation
	showDescription := device.ShowDescription
	showCalendar := device.ShowCalendar
	if !(showDate || showPhotoDate || showWeather || showCalendar || showNames || showLocation || showDescription) {
		return img
	}

	namesStr := ""
	if showNames {
		namesStr = service.FormatPeople(sourceResp.PeopleJSON, sourceResp.PhotoTakenAt, device.NameFormat, device.NamesShowAge, device.NamesMaxLen)
	}
	locationStr := ""
	if showLocation {
		locationStr = service.FormatLocation(sourceResp.Location, device.LocationMaxLen)
	}
	descriptionStr := ""
	if showDescription {
		descriptionStr = service.FormatDescription(sourceResp.Description, device.DescriptionMaxLen)
	}

	var weatherData *weather.CurrentWeather
	var deviceTimezone string
	if showWeather && device.WeatherLat != 0 && device.WeatherLon != 0 {
		var werr error
		weatherData, werr = h.weather.GetWeather(fmt.Sprintf("%f", device.WeatherLat), fmt.Sprintf("%f", device.WeatherLon))
		if werr != nil {
			log.Printf("next-preview: weather fetch failed: %v", werr)
		}
		if weatherData != nil {
			deviceTimezone = weatherData.Timezone
		}
	}

	var events []gcalendar.Event
	if showCalendar && h.calendar != nil && h.calendarGoogle != nil {
		if httpClient, cerr := h.calendarGoogle.GetClient(); cerr == nil {
			calendarID := device.CalendarID
			if calendarID == "" {
				calendarID = "primary"
			}
			if evs, eerr := h.calendar.GetTodayEvents(httpClient, calendarID, deviceTimezone); eerr == nil {
				events = evs
			}
		}
	}

	layout := model.LayoutPhotoOverlay
	if device.Layout != "" {
		layout = device.Layout
	}
	displayMode := "cover"
	if device.DisplayMode != "" {
		displayMode = device.DisplayMode
	}

	out, rerr := h.renderer.Render(service.RenderOptions{
		Layout:              layout,
		DisplayMode:         displayMode,
		Width:               logicalW,
		Height:              logicalH,
		NativeWidth:         nativeW,
		NativeHeight:        nativeH,
		Photo:               img,
		ShowDate:            showDate,
		ShowPhotoDate:       showPhotoDate,
		PhotoDate:           sourceResp.PhotoTakenAt,
		ShowWeather:         showWeather,
		Weather:             weatherData,
		ShowCalendar:        showCalendar,
		Events:              events,
		Timezone:            deviceTimezone,
		DateFormat:          device.DateFormat,
		ShowBattery:         false, // unknown at next display
		DatePosition:        device.DatePosition,
		PhotoDatePosition:   device.PhotoDatePosition,
		WeatherPosition:     device.WeatherPosition,
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
	if rerr != nil {
		log.Printf("next-preview: render failed for device %d: %v", device.ID, rerr)
		return img
	}
	return out
}
