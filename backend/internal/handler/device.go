package handler

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"io/ioutil"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/service"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type DeviceHandler struct {
	deviceService   *service.DeviceService
	synologyService *service.SynologyService
	immichService   *service.ImmichService
	battery         *service.BatteryService
	db              *gorm.DB
}

func NewDeviceHandler(deviceService *service.DeviceService, synologyService *service.SynologyService, immichService *service.ImmichService, db *gorm.DB) *DeviceHandler {
	return &DeviceHandler{
		deviceService:   deviceService,
		synologyService: synologyService,
		immichService:   immichService,
		battery:         service.NewBatteryService(db),
		db:              db,
	}
}

// ... existing methods ... (List, Add, Update, Delete, Push)

// GET /api/devices
func (h *DeviceHandler) ListDevices(c echo.Context) error {
	devices, err := h.deviceService.ListDevices()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, devices)
}

// POST /api/devices
func (h *DeviceHandler) AddDevice(c echo.Context) error {
	var req struct {
		Host          string  `json:"host"`
		EnableCollage bool    `json:"enable_collage"`
		ShowDate      bool    `json:"show_date"`
		ShowPhotoDate bool    `json:"show_photo_date"`
		ShowWeather   bool    `json:"show_weather"`
		WeatherLat    float64 `json:"weather_lat"`
		WeatherLon    float64 `json:"weather_lon"`
		Layout        string  `json:"layout"`
		DisplayMode   string  `json:"display_mode"`
		ShowCalendar  bool    `json:"show_calendar"`
		CalendarID    string  `json:"calendar_id"`
		DateFormat    string  `json:"date_format"`
		ShowBattery   bool    `json:"show_battery"`
		DisplayOrder  string  `json:"display_order"`
		DatePosition      string `json:"date_position"`
		PhotoDatePosition string `json:"photo_date_position"`
		WeatherPosition   string `json:"weather_position"`
		BatteryPosition   string `json:"battery_position"`
		BatteryStyle      string `json:"battery_style"`
		BatteryRotation   int     `json:"battery_rotation"`
		BatteryTextSide   string  `json:"battery_text_side"`
		BatteryIconScale  float64 `json:"battery_icon_scale"`
		OverlayScale      float64 `json:"overlay_scale"`
		OverlayFont       string  `json:"overlay_font"`
		OverlayWeight     string  `json:"overlay_weight"`
		ShowNames         bool    `json:"show_names"`
		NamesPosition     string  `json:"names_position"`
		NameFormat        string  `json:"name_format"`
		NamesShowAge      bool    `json:"names_show_age"`
		NamesMaxLen       int     `json:"names_max_len"`
		ShowLocation      bool    `json:"show_location"`
		LocationPosition  string  `json:"location_position"`
		LocationMaxLen    int     `json:"location_max_len"`
		ShowDescription     bool   `json:"show_description"`
		DescriptionPosition string `json:"description_position"`
		DescriptionMaxLen   int    `json:"description_max_len"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.Host == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "host required"})
	}

	if req.Layout == "" {
		req.Layout = model.LayoutPhotoOverlay
	}

	device, err := h.deviceService.AddDevice(req.Host, req.EnableCollage, req.ShowDate, req.ShowPhotoDate, req.ShowWeather, req.WeatherLat, req.WeatherLon, req.Layout, req.DisplayMode, req.ShowCalendar, req.CalendarID, req.DateFormat, req.ShowBattery, req.DisplayOrder, model.OverlaySettings{
		DatePosition:      req.DatePosition,
		PhotoDatePosition: req.PhotoDatePosition,
		WeatherPosition:   req.WeatherPosition,
		BatteryPosition:   req.BatteryPosition,
		BatteryStyle:      req.BatteryStyle,
		BatteryRotation:   req.BatteryRotation,
		BatteryTextSide:   req.BatteryTextSide,
		BatteryIconScale:  req.BatteryIconScale,
		OverlayScale:      req.OverlayScale,
		OverlayFont:       req.OverlayFont,
		OverlayWeight:     req.OverlayWeight,
		ShowNames:         req.ShowNames,
		NamesPosition:     req.NamesPosition,
		NameFormat:        req.NameFormat,
		NamesShowAge:      req.NamesShowAge,
		NamesMaxLen:       req.NamesMaxLen,
		ShowLocation:      req.ShowLocation,
		LocationPosition:  req.LocationPosition,
		LocationMaxLen:    req.LocationMaxLen,
		ShowDescription:     req.ShowDescription,
		DescriptionPosition: req.DescriptionPosition,
		DescriptionMaxLen:   req.DescriptionMaxLen,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, device)
}

// PUT /api/devices/:id
// Updates server-owned + shared fields only. Dimensions / board name
// come from POST /api/devices/:id/refresh.
func (h *DeviceHandler) UpdateDevice(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Name          string  `json:"name"`
		Host          string  `json:"host"`
		Orientation   string  `json:"orientation"`
		EnableCollage bool    `json:"enable_collage"`
		ShowDate      bool    `json:"show_date"`
		ShowPhotoDate bool    `json:"show_photo_date"`
		ShowWeather   bool    `json:"show_weather"`
		WeatherLat    float64 `json:"weather_lat"`
		WeatherLon    float64 `json:"weather_lon"`
		AIProvider    string  `json:"ai_provider"`
		AIModel       string  `json:"ai_model"`
		AIPrompt      string  `json:"ai_prompt"`
		Layout        string  `json:"layout"`
		DisplayMode   string  `json:"display_mode"`
		ShowCalendar  bool    `json:"show_calendar"`
		CalendarID    string  `json:"calendar_id"`
		DateFormat    string  `json:"date_format"`
		ShowBattery   bool    `json:"show_battery"`
		DisplayOrder  string  `json:"display_order"`
		DatePosition      string `json:"date_position"`
		PhotoDatePosition string `json:"photo_date_position"`
		WeatherPosition   string `json:"weather_position"`
		BatteryPosition   string `json:"battery_position"`
		BatteryStyle      string `json:"battery_style"`
		BatteryRotation   int     `json:"battery_rotation"`
		BatteryTextSide   string  `json:"battery_text_side"`
		BatteryIconScale  float64 `json:"battery_icon_scale"`
		OverlayScale      float64 `json:"overlay_scale"`
		OverlayFont       string  `json:"overlay_font"`
		OverlayWeight     string  `json:"overlay_weight"`
		ShowNames         bool    `json:"show_names"`
		NamesPosition     string  `json:"names_position"`
		NameFormat        string  `json:"name_format"`
		NamesShowAge      bool    `json:"names_show_age"`
		NamesMaxLen       int     `json:"names_max_len"`
		ShowLocation      bool    `json:"show_location"`
		LocationPosition  string  `json:"location_position"`
		LocationMaxLen    int     `json:"location_max_len"`
		ShowDescription     bool   `json:"show_description"`
		DescriptionPosition string `json:"description_position"`
		DescriptionMaxLen   int    `json:"description_max_len"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.Layout == "" {
		req.Layout = model.LayoutPhotoOverlay
	}

	device, err := h.deviceService.UpdateDevice(uint(id), req.Name, req.Host, req.Orientation, req.EnableCollage, req.ShowDate, req.ShowPhotoDate, req.ShowWeather, req.WeatherLat, req.WeatherLon, req.AIProvider, req.AIModel, req.AIPrompt, req.Layout, req.DisplayMode, req.ShowCalendar, req.CalendarID, req.DateFormat, req.ShowBattery, req.DisplayOrder, model.OverlaySettings{
		DatePosition:      req.DatePosition,
		PhotoDatePosition: req.PhotoDatePosition,
		WeatherPosition:   req.WeatherPosition,
		BatteryPosition:   req.BatteryPosition,
		BatteryStyle:      req.BatteryStyle,
		BatteryRotation:   req.BatteryRotation,
		BatteryTextSide:   req.BatteryTextSide,
		BatteryIconScale:  req.BatteryIconScale,
		OverlayScale:      req.OverlayScale,
		OverlayFont:       req.OverlayFont,
		OverlayWeight:     req.OverlayWeight,
		ShowNames:         req.ShowNames,
		NamesPosition:     req.NamesPosition,
		NameFormat:        req.NameFormat,
		NamesShowAge:      req.NamesShowAge,
		NamesMaxLen:       req.NamesMaxLen,
		ShowLocation:      req.ShowLocation,
		LocationPosition:  req.LocationPosition,
		LocationMaxLen:    req.LocationMaxLen,
		ShowDescription:     req.ShowDescription,
		DescriptionPosition: req.DescriptionPosition,
		DescriptionMaxLen:   req.DescriptionMaxLen,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, device)
}

// POST /api/devices/:id/refresh
// Pulls dimensions, board name, config, processing settings, and palette
// from the device. Requires the device to be online.
func (h *DeviceHandler) RefreshDevice(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	device, err := h.deviceService.RefreshDeviceFromHardware(uint(id))
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "failed to fetch") {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": errMsg})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": errMsg})
	}
	return c.JSON(http.StatusOK, device)
}

// GET /api/devices/:id/battery
// Returns the derived drain estimate (%/day, days remaining, trend) plus the
// recent samples for a sparkline. Built from the X-Battery-Percentage readings
// the device reports on each image fetch — no external measurement hardware.
func (h *DeviceHandler) BatteryEstimate(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	return c.JSON(http.StatusOK, h.battery.Estimate(uint(id)))
}

// DELETE /api/devices/:id
func (h *DeviceHandler) DeleteDevice(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.deviceService.DeleteDevice(uint(id)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/devices/:id/push
func (h *DeviceHandler) PushToDevice(c echo.Context) error {
	deviceID, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		ImageID uint   `json:"image_id"`
		URL     string `json:"url"` // Optional direct URL/Path
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	imagePath := req.URL
	var tempFile string         // If we create a temp file, we must clean it up
	var photoTakenAt *time.Time // Original capture date, for the photo-date overlay
	var peopleJSON, location, description string // Metadata for the names/location/description overlays

	if req.ImageID != 0 {
		var img model.Image
		if err := h.db.First(&img, req.ImageID).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "image not found"})
		}
		photoTakenAt = img.PhotoTakenAt
		peopleJSON = img.PeopleJSON
		location = img.Location
		description = img.Description
		if description == "" {
			description = img.Caption
		}

		if img.Source == model.SourceSynologyPhotos {
			// Download to temporary file
			data, err := h.synologyService.DownloadPhoto(int(img.SynologyPhotoID))
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to download synology photo: %v", err)})
			}

			// Save to temp file
			tmp, err := ioutil.TempFile("", "syno_push_*.jpg")
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
			}
			defer os.Remove(tmp.Name()) // Clean up
			tempFile = tmp.Name()

			if _, err := tmp.Write(data); err != nil {
				tmp.Close()
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write temp file"})
			}
			tmp.Close()
			imagePath = tempFile
		} else if img.Source == model.SourceImmich {
			// Download from Immich to temporary file
			data, err := h.immichService.DownloadPhoto(img.ImmichAssetID)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to download immich photo: %v", err)})
			}

			tmp, err := ioutil.TempFile("", "immich_push_*.jpg")
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
			}
			defer os.Remove(tmp.Name())
			tempFile = tmp.Name()

			if _, err := tmp.Write(data); err != nil {
				tmp.Close()
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write temp file"})
			}
			tmp.Close()
			imagePath = tempFile
		} else {
			imagePath = img.FilePath
		}
	}

	if imagePath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "image path or id required"})
	}

	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "image file not found on server"})
	}

	// Push
	if err := h.deviceService.PushToDevice(uint(deviceID), imagePath, photoTakenAt, peopleJSON, location, description); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not reachable") || strings.Contains(errMsg, "failed to resolve") {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": "Device is not reachable. Please ensure the device is online and accessible.",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("push failed: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "pushed"})
}
