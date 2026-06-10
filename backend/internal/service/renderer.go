package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/gcalendar"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/weather"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// DisplayProfile contains physical display info for DPI-aware text sizing.
type DisplayProfile struct {
	WidthMM  float64
	HeightMM float64
}

// Known display profiles indexed by native resolution "WxH"
var displayProfiles = map[string]DisplayProfile{
	"800x480":   {WidthMM: 160.0, HeightMM: 96.0},
	"480x800":   {WidthMM: 96.0, HeightMM: 160.0},
	"1200x1600": {WidthMM: 202.8, HeightMM: 270.4},
	"1600x1200": {WidthMM: 270.4, HeightMM: 202.8},
}

// RenderOptions contains all data needed to render a layout.
type RenderOptions struct {
	Layout        string // "photo_info", "photo_overlay", "side_panel"
	DisplayMode   string // "cover" or "fit"
	Width         int    // Logical pixel width
	Height        int    // Logical pixel height
	NativeWidth   int    // Physical panel width (for DPI calc)
	NativeHeight  int    // Physical panel height (for DPI calc)
	Photo         image.Image
	ShowDate      bool
	ShowPhotoDate bool
	PhotoDate     *time.Time // Original photo creation date
	ShowWeather   bool
	Weather       *weather.CurrentWeather
	ShowCalendar  bool
	Events        []gcalendar.Event
	Timezone      string // IANA timezone e.g. "Asia/Taipei" for date formatting
	DateFormat    string // Go time format string, empty = default "Mon, Jan 02"
	ShowBattery    bool  // Draw a battery badge on the photo
	BatteryPercent int   // 0-100 battery level reported by the device
	// Per-element placement (top/bottom × left/center/right). Date/photo-date/
	// weather only take effect on the photo_overlay layout; battery applies on
	// the photo in every layout.
	DatePosition      string
	PhotoDatePosition string
	WeatherPosition   string
	BatteryPosition   string
	BatteryStyle      string  // both | icon | text
	BatteryRotation   int     // rotate the battery badge: 0/90/180/270 degrees
	BatteryTextSide   string  // side of the icon the % text sits: right | left | top | bottom
	BatteryIconScale  float64 // size multiplier for the battery icon only (0.5–2.0)
	OverlayScale      float64 // size multiplier for overlay elements (0.5–2.0)
	OverlayFont       string  // typeface key for overlay chips (see overlayFontFamily)
	OverlayWeight     string  // regular | medium | bold
	// People names + photo location overlays (Immich metadata). The caller
	// formats Names (per device name-format/age/length) and Location into final
	// strings; the renderer just places them. Both only show on photo_overlay.
	ShowNames        bool
	Names            string
	NamesPosition    string
	ShowLocation     bool
	Location         string
	LocationPosition string
	ShowDescription     bool
	Description         string
	DescriptionPosition string
}

// overlayFontFamily maps an overlay font key to a CSS font-family stack. Every
// family is installed in the container image (see Dockerfile). The fallbacks
// keep text legible if a face ever fails to load.
func overlayFontFamily(key string) string {
	switch key {
	case "inter":
		return "'Inter', 'Noto Sans', sans-serif"
	case "dejavu_sans":
		return "'DejaVu Sans', 'Noto Sans', sans-serif"
	case "liberation_sans":
		return "'Liberation Sans', 'Arial', 'Noto Sans', sans-serif"
	case "dejavu_serif":
		return "'DejaVu Serif', 'Noto Serif', serif"
	case "ole":
		return "'Ole', cursive"
	default: // noto_sans
		return "'Noto Sans', 'Arial', sans-serif"
	}
}

// overlayFontWeight maps an overlay weight key to a numeric CSS font-weight.
func overlayFontWeight(key string) int {
	switch key {
	case "regular":
		return 400
	case "bold":
		return 700
	default: // medium
		return 500
	}
}

const browserIdleTimeout = 1 * time.Minute

// RendererService renders HTML layout templates to images using headless Chrome.
// Chrome is launched lazily on first render and shut down after 1 minute of
// inactivity to save memory.
type RendererService struct {
	browser    *rod.Browser
	tmpl       *template.Template
	fontBase64 string
	mu         sync.Mutex
	idleTimer  *time.Timer
}

func NewRendererService() (*RendererService, error) {
	funcMap := template.FuncMap{
		"formatEventTime": gcalendar.FormatEventTime,
		"nextEvent":       gcalendar.GetNextEvent,
		"limitEvents":     limitEvents,
		"mul":             mul,
		"isPortrait": func(w, h int) bool {
			return h > w
		},
		"isSmallScreen": func(w, h int) bool {
			total := w * h
			return total < 500000
		},
		// slot bundles the root template data with a position string so the
		// shared "ov_slot" template can render any of the six placement slots.
		"slot": func(d interface{}, pos string) map[string]interface{} {
			return map[string]interface{}{"D": d, "Pos": pos}
		},
	}

	tmpl, err := template.New("layout").Funcs(funcMap).Parse(layoutTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layout template: %w", err)
	}

	// Load Material Symbols font and encode as base64 for embedding in HTML.
	// Try multiple paths: the Dockerfile saves it without brackets.
	var fontBase64 string
	fontPaths := []string{
		"/usr/share/fonts/material/MaterialSymbolsOutlined.ttf",
		"/usr/share/fonts/material/MaterialSymbolsOutlined[FILL,GRAD,opsz,wght].ttf",
	}
	for _, fontPath := range fontPaths {
		fontData, err := os.ReadFile(fontPath)
		if err == nil {
			fontBase64 = base64.StdEncoding.EncodeToString(fontData)
			log.Printf("Loaded Material Symbols font from %s (%d bytes)", fontPath, len(fontData))
			break
		}
	}
	if fontBase64 == "" {
		log.Printf("Warning: could not read Material Symbols font from any known path (weather icons will degrade to text)")
	}

	return &RendererService{tmpl: tmpl, fontBase64: fontBase64}, nil
}

// launchBrowser starts headless Chrome. Must be called with s.mu held.
func (s *RendererService) launchBrowser() error {
	log.Println("Launching headless Chrome for renderer...")
	path, found := launcher.LookPath()
	if !found {
		return fmt.Errorf("chromium/chrome not found")
	}

	u, err := launcher.New().Bin(path).
		Headless(true).
		Set("no-sandbox", "").
		Set("disable-gpu", "").
		Set("disable-dev-shm-usage", "").
		Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	s.browser = browser
	log.Println("Headless Chrome launched successfully")
	return nil
}

// closeBrowser shuts down Chrome. Must be called with s.mu held.
func (s *RendererService) closeBrowser() {
	if s.browser != nil {
		log.Println("Closing idle headless Chrome to free memory")
		s.browser.Close()
		s.browser = nil
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
}

// resetIdleTimer resets the idle shutdown timer. Must be called with s.mu held.
func (s *RendererService) resetIdleTimer() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(browserIdleTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closeBrowser()
	})
}

// ensureBrowser launches Chrome if not running, and resets the idle timer.
func (s *RendererService) ensureBrowser() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.browser == nil {
		if err := s.launchBrowser(); err != nil {
			return err
		}
	}
	s.resetIdleTimer()
	return nil
}

func (s *RendererService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeBrowser()
}

// Render renders the given options to an image.
func (s *RendererService) Render(opts RenderOptions) (image.Image, error) {
	if err := s.ensureBrowser(); err != nil {
		return nil, fmt.Errorf("renderer not available: %w", err)
	}

	// Encode photo as base64 JPEG
	photoBase64, err := imageToBase64(opts.Photo)
	if err != nil {
		return nil, fmt.Errorf("failed to encode photo: %w", err)
	}

	// Calculate DPI-aware sizes
	dpmm := calcDPMM(opts.Width, opts.Height, opts.NativeWidth, opts.NativeHeight)

	// Determine max events to show based on screen size and layout
	maxEvents := calcMaxEvents(opts.Layout, opts.Width, opts.Height)

	// Compute the photo area ratio based on layout
	photoRatio := calcPhotoRatio(opts.Layout, opts.Width, opts.Height)

	// Determine next event for overlay display.
	// For overlay layout on small screens, skip all-day events.
	var nextEvent *gcalendar.Event
	if opts.ShowCalendar && len(opts.Events) > 0 {
		filteredForNext := filterEventsForLayout(opts.Layout, opts.Events, maxEvents)
		if len(filteredForNext) > 0 {
			nextEvent = gcalendar.GetNextEvent(filteredForNext)
		}
	}

	displayMode := opts.DisplayMode
	if displayMode == "" {
		displayMode = "cover"
	}

	// Viewport-relative base unit with dampened scaling for large screens.
	// Uses power-law: baseUnit = 4.8 * (minDim/480)^0.62
	// At 800x480:    baseUnit = 4.8  (reference)
	// At 1200x1600:  baseUnit ≈ 8.5
	minDim := opts.Width
	if opts.Height < minDim {
		minDim = opts.Height
	}
	baseUnit := 4.8 * math.Pow(float64(minDim)/480.0, 0.62)

	// Use device timezone for date formatting if available.
	now := time.Now()
	if opts.Timezone != "" {
		if loc, err := time.LoadLocation(opts.Timezone); err == nil {
			now = now.In(loc)
		}
	}

	// Compute photo date string if enabled and available
	var photoDateStr string
	showPhotoDate := opts.ShowPhotoDate && opts.PhotoDate != nil
	if showPhotoDate {
		pd := *opts.PhotoDate
		if opts.Timezone != "" {
			if loc, err := time.LoadLocation(opts.Timezone); err == nil {
				pd = pd.In(loc)
			}
		}
		photoDateStr = pd.Format("Jan 02, 2006")
	}

	data := templateData{
		Layout:        opts.Layout,
		DisplayMode:   displayMode,
		Width:         opts.Width,
		Height:        opts.Height,
		PhotoBase64:   photoBase64,
		FontBase64:    s.fontBase64,
		DPMM:          dpmm,
		BaseUnit:      baseUnit,
		ShowDate:      opts.ShowDate,
		DateStr:       now.Format(dateFormat(opts.DateFormat)),
		DateStrLong:   now.Format("Monday, January 02, 2006"),
		TimeStr:       now.Format("15:04"),
		ShowPhotoDate: showPhotoDate,
		PhotoDateStr:  photoDateStr,
		ShowWeather:   opts.ShowWeather,
		Weather:       opts.Weather,
		ShowCalendar:  opts.ShowCalendar,
		Events:        filterEventsForLayout(opts.Layout, opts.Events, maxEvents),
		NextEvent:     nextEvent,
		IsPortrait:    opts.Height > opts.Width,
		IsSmall:       (opts.Width * opts.Height) < 500000,
		PhotoRatio:    photoRatio,
		ShowBattery:    opts.ShowBattery,
		BatteryPercent: opts.BatteryPercent,
		IsOverlayLayout: opts.Layout != model.LayoutPhotoInfo && opts.Layout != model.LayoutSidePanel,
		DatePosition:      model.NormalizeOverlayPosition(opts.DatePosition, "bottom-left"),
		PhotoDatePosition: model.NormalizeOverlayPosition(opts.PhotoDatePosition, "bottom-left"),
		WeatherPosition:   model.NormalizeOverlayPosition(opts.WeatherPosition, "bottom-right"),
		BatteryPosition:   model.NormalizeOverlayPosition(opts.BatteryPosition, "top-right"),
		ShowBatteryIcon:   model.NormalizeBatteryStyle(opts.BatteryStyle) != "text",
		ShowBatteryText:   model.NormalizeBatteryStyle(opts.BatteryStyle) != "icon",
		BatteryRotation:   model.NormalizeBatteryRotation(opts.BatteryRotation),
		BatteryTextSide:   model.NormalizeBatteryTextSide(opts.BatteryTextSide),
		BatteryIconScale:  model.NormalizeBatteryIconScale(opts.BatteryIconScale),
		OverlayScale:      model.NormalizeOverlayScale(opts.OverlayScale),
		// template.CSS marks this as known-safe CSS (it comes from a fixed
		// whitelist, not user input). Without it, html/template's CSS sanitizer
		// rewrites the quoted, comma-separated font list to "ZgotmplZ", so the
		// chips silently fall back to the body font and the choice never applies.
		OverlayFontFamily: template.CSS(overlayFontFamily(model.NormalizeOverlayFont(opts.OverlayFont))),
		OverlayFontWeight: overlayFontWeight(model.NormalizeOverlayWeight(opts.OverlayWeight)),
		ShowNames:         opts.ShowNames && opts.Names != "",
		Names:             opts.Names,
		NamesPosition:     model.NormalizeOverlayPosition(opts.NamesPosition, "top-left"),
		ShowLocation:      opts.ShowLocation && opts.Location != "",
		Location:          opts.Location,
		LocationPosition:  model.NormalizeOverlayPosition(opts.LocationPosition, "bottom-center"),
		ShowDescription:     opts.ShowDescription && opts.Description != "",
		Description:         opts.Description,
		DescriptionPosition: model.NormalizeOverlayPosition(opts.DescriptionPosition, "wide-bottom"),
	}

	// Determine whether the top/bottom corner rows hold any chip, so the wide
	// band in each region can collapse into the corner row's place when empty.
	used := map[string]bool{}
	markUsed := func(show bool, pos string) {
		if show {
			used[pos] = true
		}
	}
	ov := data.IsOverlayLayout
	markUsed(ov && data.ShowDate, data.DatePosition)
	markUsed(ov && data.ShowCalendar && data.NextEvent != nil, data.DatePosition)
	markUsed(ov && data.ShowPhotoDate, data.PhotoDatePosition)
	markUsed(ov && data.ShowWeather && data.Weather != nil, data.WeatherPosition)
	markUsed(ov && data.ShowNames, data.NamesPosition)
	markUsed(ov && data.ShowLocation, data.LocationPosition)
	markUsed(ov && data.ShowDescription, data.DescriptionPosition)
	markUsed(data.ShowBattery, data.BatteryPosition)
	data.TopRowUsed = used["top-left"] || used["top-center"] || used["top-right"]
	data.BottomRowUsed = used["bottom-left"] || used["bottom-center"] || used["bottom-right"]
	data.WideTopUsed = used["wide-top"]
	data.WideBottomUsed = used["wide-bottom"]

	var htmlBuf bytes.Buffer
	if err := s.tmpl.Execute(&htmlBuf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	// Render HTML to image using headless Chrome
	page, err := s.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	defer page.Close()

	// Set viewport to exact device dimensions
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             opts.Width,
		Height:            opts.Height,
		DeviceScaleFactor: 1,
	}); err != nil {
		return nil, fmt.Errorf("failed to set viewport: %w", err)
	}

	if err := page.SetDocumentContent(htmlBuf.String()); err != nil {
		return nil, fmt.Errorf("failed to set page content: %w", err)
	}

	// Wait for fonts and images to load
	page.MustWaitStable()

	// Take screenshot
	screenshot, err := page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
		Clip: &proto.PageViewport{
			X:      0,
			Y:      0,
			Width:  float64(opts.Width),
			Height: float64(opts.Height),
			Scale:  1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to take screenshot: %w", err)
	}

	// Decode PNG screenshot to image.Image
	img, err := png.Decode(bytes.NewReader(screenshot))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	return img, nil
}

type templateData struct {
	Layout        string
	DisplayMode   string // "cover" or "fit"
	Width         int
	Height        int
	PhotoBase64   string
	FontBase64    string
	DPMM          float64 // dots per mm (kept for compatibility)
	BaseUnit      float64 // min(width,height)/100, for viewport-relative sizing
	ShowDate      bool
	DateStr       string // short: "Mon, Jan 02"
	DateStrLong   string // long: "Monday, January 02, 2006"
	TimeStr       string
	ShowPhotoDate bool
	PhotoDateStr  string // photo creation date, short format
	ShowWeather   bool
	Weather       *weather.CurrentWeather
	ShowCalendar  bool
	Events        []gcalendar.Event
	NextEvent     *gcalendar.Event
	IsPortrait    bool
	IsSmall       bool
	PhotoRatio    float64 // fraction of screen for photo (0.0-1.0)
	ShowBattery    bool
	BatteryPercent int
	IsOverlayLayout   bool // full-photo layout: date/photo-date/weather float on the photo
	DatePosition      string
	PhotoDatePosition string
	WeatherPosition   string
	BatteryPosition   string
	ShowBatteryIcon   bool
	ShowBatteryText   bool
	BatteryRotation   int
	BatteryTextSide   string
	BatteryIconScale  float64
	OverlayScale      float64
	OverlayFontFamily template.CSS
	OverlayFontWeight int
	ShowNames         bool
	Names             string
	NamesPosition     string
	ShowLocation      bool
	Location          string
	LocationPosition  string
	ShowDescription     bool
	Description         string
	DescriptionPosition string
	// TopRowUsed/BottomRowUsed report whether any corner chip occupies the top
	// or bottom corner row. When false, the wide band in that region collapses
	// up/down into the corner row's place instead of leaving a blank row.
	// WideTopUsed/WideBottomUsed gate the wide band so an empty one adds no gap.
	TopRowUsed     bool
	BottomRowUsed  bool
	WideTopUsed    bool
	WideBottomUsed bool
}

func imageToBase64(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func calcDPMM(logicalW, logicalH, nativeW, nativeH int) float64 {
	key := fmt.Sprintf("%dx%d", nativeW, nativeH)
	profile, ok := displayProfiles[key]
	if !ok {
		// Also try with swapped dimensions (portrait vs landscape of same panel)
		key = fmt.Sprintf("%dx%d", nativeH, nativeW)
		profile, ok = displayProfiles[key]
		if !ok {
			// Default: assume ~150 DPI
			return 150.0 / 25.4
		}
	}
	// Use the horizontal DPI
	dpi := float64(nativeW) / (profile.WidthMM / 25.4)
	return dpi / 25.4
}

func calcMaxEvents(layout string, w, h int) int {
	pixels := w * h
	isSmall := pixels < 500000

	switch layout {
	case model.LayoutPhotoInfo:
		if isSmall {
			return 2
		}
		return 8
	case model.LayoutPhotoOverlay:
		if isSmall {
			return 1
		}
		return 3
	case model.LayoutSidePanel:
		if isSmall {
			return 2
		}
		return 6
	default:
		return 1
	}
}

func calcPhotoRatio(layout string, w, h int) float64 {
	isPortrait := h > w
	isSmall := (w * h) < 500000

	switch layout {
	case model.LayoutPhotoInfo:
		if isPortrait {
			if isSmall {
				return 0.65
			}
			return 0.60
		}
		return 0.75
	case model.LayoutSidePanel:
		if isPortrait {
			// Falls back to top/bottom layout in portrait
			return 0.60
		}
		return 0.80
	default:
		return 1.0 // photo_overlay: full screen
	}
}

// dateFormat returns the Go time format string to use for date rendering.
// An empty string falls back to the default English short format.
func dateFormat(fmt string) string {
	if fmt == "" {
		return "Mon, Jan 02"
	}
	return fmt
}

func limitEvents(events []gcalendar.Event, max int) []gcalendar.Event {
	if len(events) <= max {
		return events
	}
	return events[:max]
}

// filterEventsForLayout selects and limits events based on layout type.
// For layouts with very limited space (maxEvents=1), skip all-day events
// in favor of timed events that are more useful to display.
func filterEventsForLayout(layout string, events []gcalendar.Event, maxEvents int) []gcalendar.Event {
	if maxEvents <= 1 {
		return filterOverlayEvents(events, maxEvents)
	}
	return limitEvents(events, maxEvents)
}

// filterOverlayEvents filters events for the overlay layout on small screens.
// When maxEvents is 1, skip all-day events in favor of timed events that are
// more useful to display in limited space.
func filterOverlayEvents(events []gcalendar.Event, maxEvents int) []gcalendar.Event {
	if maxEvents > 1 || len(events) == 0 {
		return limitEvents(events, maxEvents)
	}
	// maxEvents == 1: prefer the first timed event over all-day events
	for i := range events {
		if !events[i].AllDay {
			return []gcalendar.Event{events[i]}
		}
	}
	// If we get here, all events are all-day events.
	// Fallback to showing the first all-day event instead of nothing.
	return []gcalendar.Event{events[0]}
}

func init() {
	// Suppress rod's own logging
	log.SetFlags(log.LstdFlags)
}

// The HTML/CSS template for all 3 layouts
const layoutTemplate = `
{{- define "el_date"}}<div class="ov-chip date">{{.DateStr}}</div>{{end}}
{{- define "el_photodate"}}<div class="ov-chip photo-date"><span class="material-symbols-outlined">photo_camera</span> {{.PhotoDateStr}}</div>{{end}}
{{- define "el_weather"}}{{if .Weather}}<div class="ov-chip weather"><span class="material-symbols-outlined">{{.Weather.IconName}}</span> {{printf "%.1f" .Weather.Temperature}}&deg;C &nbsp; {{.Weather.Humidity}}%</div>{{end}}{{end}}
{{- define "el_calendar"}}{{if .NextEvent}}<div class="ov-chip event">{{formatEventTime .NextEvent}} &mdash; {{.NextEvent.Summary}}</div>{{end}}{{end}}
{{- define "el_battery"}}<div class="ov-chip battery bat-rot-{{.BatteryRotation}} bat-text-{{.BatteryTextSide}}{{if le .BatteryPercent 15}} low{{end}}">{{if .ShowBatteryIcon}}<div class="battery-icon"><div class="battery-fill" style="width: {{.BatteryPercent}}%"></div></div>{{end}}{{if .ShowBatteryText}}<span class="battery-text">{{.BatteryPercent}}%</span>{{end}}</div>{{end}}
{{- define "el_names"}}<div class="ov-chip names"><span class="material-symbols-outlined">group</span> {{.Names}}</div>{{end}}
{{- define "el_location"}}<div class="ov-chip location"><span class="material-symbols-outlined">place</span> {{.Location}}</div>{{end}}
{{- define "el_description"}}<div class="ov-chip description"><span class="material-symbols-outlined">notes</span> {{.Description}}</div>{{end}}
{{- define "ov_slot"}}
  <div class="ov-slot {{.Pos}}">
    {{if and .D.IsOverlayLayout .D.ShowDate (eq .D.DatePosition .Pos)}}{{template "el_date" .D}}{{end}}
    {{if and .D.IsOverlayLayout .D.ShowCalendar (eq .D.DatePosition .Pos)}}{{template "el_calendar" .D}}{{end}}
    {{if and .D.IsOverlayLayout .D.ShowPhotoDate (eq .D.PhotoDatePosition .Pos)}}{{template "el_photodate" .D}}{{end}}
    {{if and .D.IsOverlayLayout .D.ShowWeather (eq .D.WeatherPosition .Pos)}}{{template "el_weather" .D}}{{end}}
    {{if and .D.IsOverlayLayout .D.ShowNames (eq .D.NamesPosition .Pos)}}{{template "el_names" .D}}{{end}}
    {{if and .D.IsOverlayLayout .D.ShowLocation (eq .D.LocationPosition .Pos)}}{{template "el_location" .D}}{{end}}
    {{if and .D.IsOverlayLayout .D.ShowDescription (eq .D.DescriptionPosition .Pos)}}{{template "el_description" .D}}{{end}}
    {{if and .D.ShowBattery (eq .D.BatteryPosition .Pos)}}{{template "el_battery" .D}}{{end}}
  </div>
{{- end}}
{{- define "floating"}}<div class="floating">
  <div class="float-region top">
    {{if .TopRowUsed}}<div class="corner-row">
      {{template "ov_slot" (slot . "top-left")}}
      {{template "ov_slot" (slot . "top-center")}}
      {{template "ov_slot" (slot . "top-right")}}
    </div>{{end}}
    {{if .WideTopUsed}}{{template "ov_slot" (slot . "wide-top")}}{{end}}
  </div>
  <div class="float-region bottom">
    {{if .WideBottomUsed}}{{template "ov_slot" (slot . "wide-bottom")}}{{end}}
    {{if .BottomRowUsed}}<div class="corner-row">
      {{template "ov_slot" (slot . "bottom-left")}}
      {{template "ov_slot" (slot . "bottom-center")}}
      {{template "ov_slot" (slot . "bottom-right")}}
    </div>{{end}}
  </div>
</div>{{end -}}
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  {{if .FontBase64}}
  @font-face {
    font-family: 'Material Symbols Outlined';
    src: url(data:font/ttf;base64,{{.FontBase64}}) format('truetype');
  }
  {{end}}
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    width: {{.Width}}px;
    height: {{.Height}}px;
    overflow: hidden;
    font-family: 'Noto Sans', 'Arial', sans-serif;
    background: #000;
    color: #fff;
  }

  /* Viewport-relative sizing: BaseUnit = 1% of smaller screen dimension */
  :root {
    /* Sizes as percentage of min(width, height) */
    --body-size: {{printf "%.1f" (mul .BaseUnit 4.7)}}px;
    --secondary-size: {{printf "%.1f" (mul .BaseUnit 4.0)}}px;
    --heading-size: {{printf "%.1f" (mul .BaseUnit 6.8)}}px;
    --time-size: {{printf "%.1f" (mul .BaseUnit 10.4)}}px;
    --icon-size: {{printf "%.1f" (mul .BaseUnit 13.5)}}px;
    --small-icon-size: {{printf "%.1f" (mul .BaseUnit 8.3)}}px;
    --padding: {{printf "%.1f" (mul .BaseUnit 3.6)}}px;
    --gap: {{printf "%.1f" (mul .BaseUnit 2.6)}}px;
  }

  .material-symbols-outlined {
    font-family: 'Material Symbols Outlined';
    font-weight: normal;
    font-style: normal;
    display: inline-block;
    line-height: 1;
    text-transform: none;
    letter-spacing: normal;
    word-wrap: normal;
    white-space: nowrap;
    direction: ltr;
    font-variation-settings: 'FILL' 1;
  }

  .photo-date {
    font-size: var(--secondary-size);
    opacity: 0.75;
    display: flex;
    align-items: center;
    gap: 0.3em;
  }
  .photo-date .material-symbols-outlined {
    font-size: 1.1em;
  }

  .photo-area {
    position: relative;
    overflow: hidden;
    z-index: 0; /* Create stacking context so child z-indices don't bleed into parent */
  }

  img.photo {
    width: 100%;
    height: 100%;
    object-fit: {{if eq .DisplayMode "fit"}}contain{{else}}cover{{end}};
    display: block;
    position: relative;
    z-index: 1;
  }

  .photo-blur {
    position: absolute;
    inset: -20px;
    width: calc(100% + 40px);
    height: calc(100% + 40px);
    object-fit: cover;
    filter: blur(20px) brightness(0.9);
    z-index: 0;
  }

  /* --- Floating overlay elements (date / photo-date / weather / battery) ---
     Each element is placed into one of six slots positioned over the photo.
     Every element carries its own translucent chip so it stays legible in any
     corner, instead of relying on a single bottom gradient. */
  .floating {
    position: absolute;
    inset: 0;
    z-index: 5;
    pointer-events: none;
    --ov-scale: {{printf "%.2f" .OverlayScale}};
    --bat-icon-scale: {{printf "%.2f" .BatteryIconScale}};
    --ov-font: {{.OverlayFontFamily}};
    --ov-weight: {{.OverlayFontWeight}};
  }
  /* Two stacked regions (top, bottom), each a flex column of: a corner row
     (3-cell grid: left / center / right) and a full-width band. Either is
     omitted when empty, so the band collapses into the corner row's place when
     no corner chip is shown. */
  .float-region {
    position: absolute;
    left: var(--padding);
    right: var(--padding);
    display: flex;
    flex-direction: column;
    gap: calc(var(--gap) * 0.6);
  }
  .float-region.top    { top: var(--padding); }
  .float-region.bottom { bottom: var(--padding); }
  .corner-row {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: start;
    gap: var(--gap);
  }
  .float-region.bottom .corner-row { align-items: end; }
  .ov-slot {
    display: flex;
    flex-direction: column;
    gap: calc(var(--gap) * 0.6);
    min-width: 0;
    max-width: 100%;
  }
  .ov-slot.top-left,   .ov-slot.bottom-left   { justify-self: start;  align-items: flex-start; }
  .ov-slot.top-center, .ov-slot.bottom-center { justify-self: center; align-items: center; }
  .ov-slot.top-right,  .ov-slot.bottom-right  { justify-self: end;    align-items: flex-end; }
  /* Full-width band for long content (location / names / description). */
  .ov-slot.wide-top, .ov-slot.wide-bottom { width: 100%; align-items: stretch; }
  .ov-slot.wide-top > .ov-chip, .ov-slot.wide-bottom > .ov-chip {
    width: 100%;
    max-width: 100%;
    justify-content: center;
    text-align: center;
  }

  /* All overlay chips share one look: same font family, size, weight and
     opacity. Only icon glyphs and the event ellipsis differ structurally. */
  .ov-chip {
    display: flex;
    align-items: center;
    gap: 0.4em;
    padding: 0.25em 0.55em;
    border-radius: 0.4em;
    background: rgba(0,0,0,0.45);
    color: #fff;
    line-height: 1.15;
    max-width: 100%;
    font-family: var(--ov-font);
    font-size: calc(var(--secondary-size) * var(--ov-scale));
    font-weight: var(--ov-weight);
    opacity: 1;
  }
  /* Chip icons render slightly larger than the text, but their line box is
     trimmed to the chip line-height (1.15) so no icon makes its chip taller
     than a text-only chip. Every overlay field keeps the same background
     height. The 0.25em chip padding absorbs the small visual overflow. */
  .ov-chip .material-symbols-outlined { font-size: 1.3em; line-height: 0.88; }
  .ov-chip.event { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; display: block; }

  /* Battery icon rotation. A 90/270-turned icon is 1.7em tall, so reserve
     vertical room via min-height to keep it inside the chip background. */
  .bat-rot-90 .battery-icon  { transform: rotate(90deg); }
  .bat-rot-180 .battery-icon { transform: rotate(180deg); }
  .bat-rot-270 .battery-icon { transform: rotate(270deg); }
  .ov-chip.battery.bat-rot-90, .ov-chip.battery.bat-rot-270 {
    min-height: calc(var(--secondary-size) * var(--bat-icon-scale) * 2.6);
  }

  /* Which side of the icon the % text sits on (right is the default row). */
  .ov-chip.battery.bat-text-left   { flex-direction: row-reverse; }
  .ov-chip.battery.bat-text-top    { flex-direction: column-reverse; }
  .ov-chip.battery.bat-text-bottom { flex-direction: column; }

  /* CSS battery icon — renders identically even if the icon font is missing. */
  .battery-icon {
    position: relative;
    /* Sized off the unscaled base * its own multiplier, so the icon size is
       independent of the text size (--ov-scale). All inner metrics are in em
       and therefore scale together with the icon. */
    font-size: calc(var(--secondary-size) * var(--bat-icon-scale));
    width: 1.7em;
    height: 0.9em;
    box-sizing: border-box;
    border: 0.12em solid #fff;
    border-radius: 0.16em;
    padding: 0.1em;
    flex: none;
    transform-origin: center;
  }
  .battery-icon::after {
    content: '';
    position: absolute;
    right: -0.24em;
    top: 50%;
    transform: translateY(-50%);
    width: 0.13em;
    height: 0.42em;
    background: #fff;
    border-radius: 0 0.1em 0.1em 0;
  }
  .battery-fill { height: 100%; background: #fff; border-radius: 0.04em; }
  .ov-chip.battery.low { color: #ffd6d6; }
  .ov-chip.battery.low .battery-fill { background: #e03b3b; }

  .info-panel {
    background: #ffffff;
    color: #000;
    padding: var(--padding);
    display: flex;
    flex-direction: column;
    justify-content: center;
    overflow: hidden;
  }

  .info-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: var(--gap);
  }

  /* Scoped to the info panel so it does not leak into the floating overlay
     date chip (.ov-chip.date), which must stay uniform with the other chips. */
  .info-panel .date {
    font-size: var(--heading-size);
    font-weight: 600;
  }

  .time-display {
    font-size: var(--time-size);
    font-weight: 300;
    letter-spacing: 2px;
  }

  .weather-block {
    display: flex;
    align-items: center;
    gap: var(--gap);
  }

  .weather-icon {
    font-size: var(--icon-size);
  }

  .weather-icon-small {
    font-size: var(--small-icon-size);
  }

  .weather-details {
    font-size: var(--secondary-size);
  }

  .weather-temp {
    font-size: var(--heading-size);
    font-weight: 600;
  }

  .divider {
    border: none;
    border-top: 1px solid #000;
    margin: var(--gap) 0;
  }

  .events-list {
    list-style: none;
    overflow: hidden;
  }

  .event-item {
    display: flex;
    align-items: baseline;
    gap: var(--gap);
    padding: calc(var(--gap) * 0.4) 0;
    font-size: var(--body-size);
  }

  .event-time {
    font-weight: 600;
    white-space: nowrap;
    min-width: 4em;
    font-size: var(--secondary-size);
  }

  .event-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Scale down info panel text */
  {{if .IsSmall}}
  .info-panel {
    --body-size: {{printf "%.1f" (mul .BaseUnit 3.5)}}px;
    --secondary-size: {{printf "%.1f" (mul .BaseUnit 3.0)}}px;
    --heading-size: {{printf "%.1f" (mul .BaseUnit 4.5)}}px;
    --icon-size: {{printf "%.1f" (mul .BaseUnit 9.0)}}px;
    --small-icon-size: {{printf "%.1f" (mul .BaseUnit 6.0)}}px;
    --padding: {{printf "%.1f" (mul .BaseUnit 2.4)}}px;
    --gap: {{printf "%.1f" (mul .BaseUnit 1.6)}}px;
  }
  {{else}}
  .info-panel {
    --body-size: {{printf "%.1f" (mul .BaseUnit 3.1)}}px;
    --secondary-size: {{printf "%.1f" (mul .BaseUnit 2.6)}}px;
    --heading-size: {{printf "%.1f" (mul .BaseUnit 4.4)}}px;
    --icon-size: {{printf "%.1f" (mul .BaseUnit 8.8)}}px;
    --small-icon-size: {{printf "%.1f" (mul .BaseUnit 5.5)}}px;
    --padding: {{printf "%.1f" (mul .BaseUnit 3.1)}}px;
    --gap: {{printf "%.1f" (mul .BaseUnit 2.1)}}px;
  }
  {{end}}

  /* --- LAYOUT 1: Photo + Info Strip --- */
  .layout-photo_info {
    display: flex;
    width: 100%;
    height: 100%;
  }
  .layout-photo_info.portrait {
    flex-direction: column;
  }
  .layout-photo_info.landscape {
    flex-direction: row;
  }
  .layout-photo_info .photo-area {
    flex: 1 1 0;
    min-height: 0;
    min-width: 0;
    overflow: hidden;
    position: relative;
  }
  .layout-photo_info.portrait .info-panel {
    flex: 0 0 auto;
    max-height: 20%;
    min-height: 0;
    overflow: hidden;
  }
  .layout-photo_info.landscape .info-panel {
    flex: 0 0 auto;
    max-width: 25%;
    min-width: 0;
    overflow: hidden;
  }

  /* --- LAYOUT 2: Full Photo + Bottom Overlay --- */
  .layout-photo_overlay {
    position: relative;
    width: 100%;
    height: 100%;
  }
  .layout-photo_overlay .photo-area {
    width: 100%;
    height: 100%;
  }
  .layout-photo_overlay .overlay {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    padding: var(--padding);
    padding-top: calc(var(--padding) * 4);
    color: #fff;
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: var(--padding);
    background: linear-gradient(to bottom, rgba(0,0,0,0) 0%, rgba(0,0,0,0.35) 30%, rgba(0,0,0,0.43) 60%, rgba(0,0,0,0.55) 100%);
  }
  .layout-photo_overlay .overlay-left {
    display: flex;
    flex-direction: column;
    gap: calc(var(--gap) * 0.5);
  }
  .layout-photo_overlay .overlay-right {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: calc(var(--gap) * 0.5);
  }
  .layout-photo_overlay .overlay .date {
    font-size: var(--heading-size);
  }
  .layout-photo_overlay .overlay .event-inline {
    font-size: var(--secondary-size);
    opacity: 0.9;
  }

  /* --- LAYOUT 3: Side Panel --- */
  .layout-side_panel {
    display: flex;
    width: 100%;
    height: 100%;
  }
  .layout-side_panel.portrait {
    flex-direction: column;
  }
  .layout-side_panel.landscape {
    flex-direction: row;
  }
  .layout-side_panel.landscape .photo-area {
    flex: 0 0 {{printf "%.0f" (mul .PhotoRatio 100)}}%;
    overflow: hidden;
  }
  .layout-side_panel.landscape .info-panel {
    flex: 1;
    min-width: 0;
    justify-content: center;
    padding: calc(var(--padding) * 1.2);
  }
  .layout-side_panel.landscape .info-header {
    flex-direction: column;
    align-items: flex-start;
    justify-content: flex-start;
    gap: calc(var(--gap) * 0.5);
    margin-bottom: 0;
  }
  .layout-side_panel.landscape .weather-icon {
    font-size: var(--small-icon-size);
  }
  .layout-side_panel.landscape .weather-block {
    gap: calc(var(--gap) * 0.6);
  }
  .layout-side_panel.landscape .divider {
    margin-top: calc(var(--gap) * 1.2);
  }
  .layout-side_panel.portrait .divider,
  .layout-side_panel.portrait .events-list {
    margin-top: auto;
  }
  .layout-side_panel.portrait .photo-area {
    flex: 0 0 80%;
    overflow: hidden;
  }
  .layout-side_panel.portrait .info-panel {
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

</style>
</head>
<body>

{{if eq .Layout "photo_info"}}
<!-- LAYOUT 1: Photo + Info Strip -->
<div class="layout-photo_info {{if .IsPortrait}}portrait{{else}}landscape{{end}}">
  <div class="photo-area">
    {{if eq .DisplayMode "fit"}}<img class="photo-blur" src="data:image/jpeg;base64,{{.PhotoBase64}}">{{end}}
    <img class="photo" src="data:image/jpeg;base64,{{.PhotoBase64}}">
    {{template "floating" .}}
  </div>
  <div class="info-panel">
    <div class="info-header">
      {{if .ShowDate}}
      <div>
        <div class="date">{{.DateStr}}</div>
        {{if .ShowPhotoDate}}<div class="photo-date"><span class="material-symbols-outlined">photo_camera</span> {{.PhotoDateStr}}</div>{{end}}
      </div>
      {{end}}
      {{if and .ShowWeather .Weather}}
      <div class="weather-block">
        <span class="material-symbols-outlined weather-icon">{{.Weather.IconName}}</span>
        <div>
          <div class="weather-temp">{{printf "%.1f" .Weather.Temperature}}&deg;C</div>
          <div class="weather-details">{{.Weather.Humidity}}% humidity</div>
        </div>
      </div>
      {{end}}
    </div>

    {{if and .ShowCalendar (gt (len .Events) 0)}}
    <hr class="divider">
    <ul class="events-list">
      {{range .Events}}
      <li class="event-item">
        <span class="event-time">{{formatEventTime .}}</span>
        <span class="event-title">{{.Summary}}</span>
      </li>
      {{end}}
    </ul>
    {{end}}
  </div>
</div>

{{else if eq .Layout "photo_overlay"}}
<!-- LAYOUT 2: Full Photo + Floating Overlay Elements -->
<div class="layout-photo_overlay">
  <div class="photo-area">
    {{if eq .DisplayMode "fit"}}<img class="photo-blur" src="data:image/jpeg;base64,{{.PhotoBase64}}">{{end}}
    <img class="photo" src="data:image/jpeg;base64,{{.PhotoBase64}}">
    {{template "floating" .}}
  </div>
</div>

{{else if eq .Layout "side_panel"}}
<!-- LAYOUT 3: Side Panel -->
<div class="layout-side_panel {{if .IsPortrait}}portrait{{else}}landscape{{end}}">
  <div class="photo-area">
    {{if eq .DisplayMode "fit"}}<img class="photo-blur" src="data:image/jpeg;base64,{{.PhotoBase64}}">{{end}}
    <img class="photo" src="data:image/jpeg;base64,{{.PhotoBase64}}">
    {{template "floating" .}}
  </div>
  <div class="info-panel">
    {{if or .ShowDate .ShowPhotoDate (and .ShowWeather .Weather)}}
    <div class="info-header">
      {{if .ShowDate}}
      <div class="date">{{.DateStr}}</div>
      {{if .ShowPhotoDate}}<div class="photo-date"><span class="material-symbols-outlined">photo_camera</span> {{.PhotoDateStr}}</div>{{end}}
      {{end}}
      {{if and .ShowWeather .Weather}}
      <div class="weather-block">
        <span class="material-symbols-outlined weather-icon">{{.Weather.IconName}}</span>
        <div>
          <div class="weather-temp">{{printf "%.1f" .Weather.Temperature}}&deg;C</div>
          <div class="weather-details">{{.Weather.Description}} &middot; {{.Weather.Humidity}}%</div>
        </div>
      </div>
      {{end}}
    </div>
    {{end}}

    {{if and .ShowCalendar (gt (len .Events) 0)}}
    <hr class="divider">
    <ul class="events-list">
      {{range .Events}}
      <li class="event-item">
        <span class="event-time">{{formatEventTime .}}</span>
        <span class="event-title">{{.Summary}}</span>
      </li>
      {{end}}
    </ul>
    {{end}}
  </div>
</div>

{{else}}
<!-- Default: same as photo_overlay -->
<div class="layout-photo_overlay">
  <div class="photo-area">
    {{if eq .DisplayMode "fit"}}<img class="photo-blur" src="data:image/jpeg;base64,{{.PhotoBase64}}">{{end}}
    <img class="photo" src="data:image/jpeg;base64,{{.PhotoBase64}}">
    {{template "floating" .}}
  </div>
</div>
{{end}}

</body>
</html>`

// mul is a template helper for multiplication
func mul(a, b float64) float64 {
	return math.Round(a * b)
}
