package photoframe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// MinEPDGZVersion is the minimum firmware version that supports epdgz format.
const MinEPDGZVersion = "2.6.1"

// SupportsEPDGZ returns true if the given firmware version supports the epdgz format.
//
// Dev builds ("dev-<sha>") are ranked below every release by compareVersions(),
// so they're treated as not supporting EPDGZ here too.
func SupportsEPDGZ(version string) bool {
	return compareVersions(version, MinEPDGZVersion) > 0
}

// compareVersions compares two semver strings (with optional "v" prefix).
// Returns -1 if v1 < v2, 0 if equal, 1 if v1 > v2.
// Dev versions (e.g. "dev-abc123") are considered older than any release.
func compareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	if strings.HasPrefix(v1, "dev-") {
		return -1
	}
	if strings.HasPrefix(v2, "dev-") {
		return 1
	}

	p1 := parseVersion(v1)
	p2 := parseVersion(v2)

	for i := 0; i < 3; i++ {
		if p1[i] < p2[i] {
			return -1
		}
		if p1[i] > p2[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	var parts [3]int
	segs := strings.SplitN(v, ".", 3)
	for i, s := range segs {
		if i < 3 {
			parts[i], _ = strconv.Atoi(s)
		}
	}
	return parts
}

// Shared HTTP client with mDNS-compatible resolver (reused across all Client instances)
var sharedHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
			Resolver:  &net.Resolver{PreferGo: false},
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
	Timeout: 120 * time.Second,
}

type Client struct {
	host       string
	resolvedIP string // Cached resolved IP
	httpClient *http.Client
}

func NewClient(host string) *Client {
	return &Client{
		host:       host,
		httpClient: sharedHTTPClient,
	}
}

// PushImage pushes an EPDGZ image and an optional thumbnail to the device.
func (c *Client) PushImage(imageBytes []byte, thumbBytes []byte) error {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	// Quick reachability check on IP
	if err := c.checkReachability(ip); err != nil {
		return fmt.Errorf("device %s (%s) is not reachable: %w", c.host, ip, err)
	}

	// Prepare multipart request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 1. Add image part
	part, err := writer.CreateFormFile("image", "image.epdgz")
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(imageBytes)); err != nil {
		return fmt.Errorf("failed to copy image bytes: %w", err)
	}

	// 2. Add Thumbnail part (if available)
	if len(thumbBytes) > 0 {
		thumbPart, err := writer.CreateFormFile("thumbnail", "thumbnail.jpg")
		if err != nil {
			return fmt.Errorf("failed to create thumbnail form file: %w", err)
		}
		if _, err := io.Copy(thumbPart, bytes.NewReader(thumbBytes)); err != nil {
			return fmt.Errorf("failed to copy thumbnail bytes: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Construct URL using IP address
	url := fmt.Sprintf("http://%s/api/display-image", ip)

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	// Set Host header just in case, though usually not needed for direct IP
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(readErrBody(resp.Body))
		if detail != "" {
			return fmt.Errorf("device returned status %d: %s", resp.StatusCode, detail)
		}
		return fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	return nil
}

// readErrBody reads a bounded snippet of an error response body so the device's
// own error message (e.g. the frame's httpd_resp_send_err text) reaches the
// server logs / UI instead of a bare status code.
func readErrBody(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	return string(b)
}

// PushRawEPD pushes uncompressed, display-ready 4bpp EPD bytes to the device as
// a plain request body with Content-Type: application/x-epd-raw. Used for
// SRAM-only boards (see SystemInfo.IsRawEPDOnly) that cannot inflate EPDGZ or
// hold a multipart upload in RAM — the device streams the body straight into
// its framebuffer, mirroring the raw-EPD pull path.
func (c *Client) PushRawEPD(rawBytes []byte) error {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	// Quick reachability check on IP
	if err := c.checkReachability(ip); err != nil {
		return fmt.Errorf("device %s (%s) is not reachable: %w", c.host, ip, err)
	}

	url := fmt.Sprintf("http://%s/api/display-image", ip)

	req, err := http.NewRequest("POST", url, bytes.NewReader(rawBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-epd-raw")
	req.ContentLength = int64(len(rawBytes))
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(readErrBody(resp.Body))
		if detail != "" {
			return fmt.Errorf("device returned status %d: %s", resp.StatusCode, detail)
		}
		return fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	return nil
}

// Host returns the client's target host.
func (c *Client) Host() string {
	return c.host
}

func (c *Client) resolveHost(host string) (string, error) {
	// Return cached result
	if c.resolvedIP != "" {
		return c.resolvedIP, nil
	}

	// If it's already an IP, cache and return it
	if net.ParseIP(host) != nil {
		c.resolvedIP = host
		return host, nil
	}

	// For .local (mDNS) on macOS, use dns-sd for fast resolution
	// (Go's net.LookupHost has a 5s timeout trying regular DNS first)
	if strings.HasSuffix(host, ".local") && runtime.GOOS == "darwin" {
		if ip, err := resolveMDNSDarwin(host); err == nil {
			c.resolvedIP = ip
			return ip, nil
		}
		// Fall through to standard resolver
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return "", err
	}

	// Prefer IPv4
	for _, ip := range ips {
		if strings.Contains(ip, ".") {
			c.resolvedIP = ip
			return ip, nil
		}
	}

	// Fallback to first (likely IPv6)
	if len(ips) > 0 {
		c.resolvedIP = ips[0]
		return ips[0], nil
	}

	return "", fmt.Errorf("no IP found for host %s", host)
}

// resolveMDNSDarwin uses macOS dns-sd for fast mDNS resolution (~10ms vs 5s).
func resolveMDNSDarwin(host string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dns-sd", "-G", "v4", host)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip header lines; look for the result line containing the hostname
		if strings.Contains(line, host) && !strings.HasPrefix(line, "DATE") && !strings.HasPrefix(line, "Timestamp") {
			for _, field := range strings.Fields(line) {
				if net.ParseIP(field) != nil && strings.Contains(field, ".") {
					log.Printf("mDNS resolved %s -> %s", host, field)
					cmd.Process.Kill()
					return field, nil
				}
			}
		}
	}

	cmd.Process.Kill()
	cmd.Wait()
	return "", fmt.Errorf("dns-sd: no result for %s", host)
}

func (c *Client) checkReachability(ip string) error {
	target := ip
	if !strings.Contains(target, ":") {
		target = net.JoinHostPort(target, "80")
	}

	conn, err := net.DialTimeout("tcp4", target, 2*time.Second)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

type SystemInfo struct {
	DeviceName      string `json:"device_name"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	BoardName       string `json:"board_name"`
	Version         string `json:"version"`
	HasFlashStorage bool   `json:"has_flash_storage"`
	SDCardInserted  bool   `json:"sdcard_inserted"`
	// HTTPSSupported is a pointer so we can tell "device reported false" (no
	// PSRAM → can't do HTTPS) apart from "firmware too old to report it" (nil,
	// leave the stored capability untouched).
	HTTPSSupported *bool `json:"https_supported"`
}

// IsRawEPDOnly reports whether the device has no persistent storage (no flash
// LittleFS, no SD card). Such SRAM-only boards (e.g. FireBeetle 2 ESP32-E)
// cannot inflate EPDGZ or buffer a multipart upload in RAM, so they must be
// pushed uncompressed, display-ready EPD bytes via PushRawEPD.
func (si *SystemInfo) IsRawEPDOnly() bool {
	return si != nil && !si.HasFlashStorage && !si.SDCardInserted
}

func (c *Client) FetchSystemInfo() (*SystemInfo, error) {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s/api/system-info", ip)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	var info SystemInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode system info: %w", err)
	}

	return &info, nil
}

// BatteryInfo mirrors the device's /api/battery response.
type BatteryInfo struct {
	BatteryLevel     int  `json:"battery_level"` // percent 0-100, -1 if unknown
	BatteryVoltage   int  `json:"battery_voltage"`
	Charging         bool `json:"charging"`
	USBConnected     bool `json:"usb_connected"`
	BatteryConnected bool `json:"battery_connected"`
}

// FetchBattery reads the device's current battery level. Used by the
// server-initiated push path, which (unlike the pull path) has no
// X-Battery-Percentage header to read the level from.
func (c *Client) FetchBattery() (*BatteryInfo, error) {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s/api/battery", ip)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	var info BatteryInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode battery info: %w", err)
	}

	return &info, nil
}

type ProcessingSettings struct {
	Exposure             float64 `json:"exposure"`
	Saturation           float64 `json:"saturation"`
	ToneMode             string  `json:"toneMode"`
	Contrast             float64 `json:"contrast"`
	Strength             float64 `json:"strength"`
	ShadowBoost          float64 `json:"shadowBoost"`
	HighlightCompress    float64 `json:"highlightCompress"`
	Midpoint             float64 `json:"midpoint"`
	ColorMethod          string  `json:"colorMethod"`
	ProcessingMode       string  `json:"processingMode"`
	DitherAlgorithm      string  `json:"ditherAlgorithm"`
	CompressDynamicRange bool    `json:"compressDynamicRange"`
}

type PaletteColor struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

type Palette struct {
	Black  PaletteColor `json:"black"`
	White  PaletteColor `json:"white"`
	Yellow PaletteColor `json:"yellow"`
	Red    PaletteColor `json:"red"`
	Blue   PaletteColor `json:"blue"`
	Green  PaletteColor `json:"green"`
}

// FetchConfig returns the full device config as a raw JSON string.
func (c *Client) FetchConfig() (string, error) {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return "", fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s/api/config", ip)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	return string(body), nil
}

// FetchProcessingSettings returns the device processing settings as a raw JSON string.
func (c *Client) FetchProcessingSettings() (string, error) {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return "", fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s/api/settings/processing", ip)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read processing settings: %w", err)
	}

	return string(body), nil
}

// FetchPalette returns the device color palette as a raw JSON string.
func (c *Client) FetchPalette() (string, error) {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return "", fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s/api/settings/palette", ip)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Host = c.host

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read palette: %w", err)
	}

	return string(body), nil
}

// postJSON POSTs a JSON body to a device endpoint (e.g. /api/config,
// /api/settings/processing, /api/settings/palette). Shared by the Push* config
// helpers so host resolution + request boilerplate lives in one place.
func (c *Client) postJSON(path string, jsonData []byte) error {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s%s", ip, path)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Host = c.host
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) PushConfig(config map[string]interface{}) error {
	jsonData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return c.postJSON("/api/config", jsonData)
}

// PushProcessingSettings pushes the device's image-processing settings to its
// /api/settings/processing endpoint. The body is the same JSON shape the device
// dialog saves (exposure, saturation, toneMode, …).
func (c *Client) PushProcessingSettings(settings json.RawMessage) error {
	return c.postJSON("/api/settings/processing", settings)
}

// PushPalette pushes the device's color palette to its /api/settings/palette
// endpoint. The body is the same JSON shape the device dialog saves
// ({ black: {r,g,b}, white: {…}, … }).
func (c *Client) PushPalette(palette json.RawMessage) error {
	return c.postJSON("/api/settings/palette", palette)
}

// Rotate asks an awake frame to advance to its next image via its own
// /api/rotate endpoint. Board-agnostic, but only effective while the frame is
// awake/reachable (e.g. a deep-sleeping frame won't see it until it next wakes).
func (c *Client) Rotate() error {
	return c.postJSON("/api/rotate", []byte("{}"))
}

// OTACheck asks the frame to check for a firmware update (POST /api/ota/check)
// and reports whether one is available. Boards with no OTA partition (e.g. the
// FireBeetle) always report false. Only effective on an awake/reachable frame.
func (c *Client) OTACheck() (bool, error) {
	ip, err := c.resolveHost(c.host)
	if err != nil {
		return false, fmt.Errorf("failed to resolve device %s: %w", c.host, err)
	}

	url := fmt.Sprintf("http://%s/api/ota/check", ip)
	req, err := http.NewRequest("POST", url, bytes.NewBufferString("{}"))
	if err != nil {
		return false, err
	}
	req.Host = c.host
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("device returned status: %d", resp.StatusCode)
	}

	var out struct {
		UpdateAvailable bool   `json:"update_available"`
		Status          string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("failed to decode ota check: %w", err)
	}
	if out.Status != "success" {
		return false, fmt.Errorf("frame could not check for updates")
	}
	return out.UpdateAvailable, nil
}

// OTAUpdate tells the frame to download + install the update found on the last
// OTACheck (POST /api/ota/update). Call OTACheck first — the frame refuses if
// no update is pending or one is already in progress.
func (c *Client) OTAUpdate() error {
	return c.postJSON("/api/ota/update", []byte("{}"))
}
