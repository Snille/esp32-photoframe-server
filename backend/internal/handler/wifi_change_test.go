package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/service"
	"github.com/labstack/echo/v4"
)

// pull runs applyConfigSync for one simulated frame pull and returns the
// X-Config-Payload header ("" when none was sent).
func pull(t *testing.T, h *ImageHandler, device *model.Device, deviceTS int64, query string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/image/immich"+query, nil)
	req.Header.Set("X-Config-Last-Updated", strconv.FormatInt(deviceTS, 10))
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	h.applyConfigSync(c, device, true)
	return rec.Header().Get("X-Config-Payload")
}

func payloadConfig(t *testing.T, payload string) map[string]interface{} {
	t.Helper()
	var p struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("payload is not JSON: %v (%s)", err, payload)
	}
	return p.Config
}

func TestWifiChangeSentOnceThenCleared(t *testing.T) {
	store := service.NewWifiChangeStore()
	h := &ImageHandler{wifiChanges: store}
	// Frame is current (same timestamp): no regular config push.
	device := &model.Device{ID: 5, ConfigLastUpdated: 100, DeviceConfig: `{"wifi_ssid":"IOT","device_name":"Mini"}`}
	store.Request(5, "Guest", "secret-pass", "IOT")

	// A preview render must not consume the change.
	if p := pull(t, h, device, 100, "?preview=1"); p != "" {
		t.Fatalf("preview got a payload: %s", p)
	}

	p := pull(t, h, device, 100, "")
	cfg := payloadConfig(t, p)
	if cfg["wifi_ssid"] != "Guest" || cfg["wifi_password"] != "secret-pass" {
		t.Fatalf("wifi fields missing: %s", p)
	}
	if strings.Contains(p, "config_last_updated") {
		t.Fatalf("wifi-only payload must not move the frame's config timestamp: %s", p)
	}
	if _, ok := cfg["device_name"]; ok {
		t.Fatalf("wifi-only payload carried regular config: %s", p)
	}

	// Next pull: nothing left to send, and the password is gone.
	if p := pull(t, h, device, 100, ""); p != "" {
		t.Fatalf("second pull got a payload: %s", p)
	}
}

func TestWifiChangeMergedIntoRegularPush(t *testing.T) {
	store := service.NewWifiChangeStore()
	h := &ImageHandler{wifiChanges: store}
	device := &model.Device{ID: 6, ConfigLastUpdated: 200, DeviceConfig: `{"wifi_ssid":"IOT","device_name":"Mini"}`}
	store.Request(6, "Gäst", "", "IOT")

	p := pull(t, h, device, 100, "")
	for _, r := range p {
		if r > 0x7e {
			t.Fatalf("header holds a non-ASCII byte: %q", p)
		}
	}
	cfg := payloadConfig(t, p)
	if cfg["wifi_ssid"] != "Gäst" || cfg["device_name"] != "Mini" {
		t.Fatalf("merged payload wrong: %s", p)
	}
	if _, ok := cfg["wifi_password"]; ok {
		t.Fatal("empty password must be left out (frame keeps its current one)")
	}
	if !strings.Contains(p, `"config_last_updated":200`) {
		t.Fatalf("regular push lost its timestamp: %s", p)
	}
}

func TestRegularPushNeverEchoesStoredSSID(t *testing.T) {
	h := &ImageHandler{wifiChanges: service.NewWifiChangeStore()}
	device := &model.Device{ID: 8, ConfigLastUpdated: 200, DeviceConfig: `{"wifi_ssid":"IOT","wifi_password":"x","device_name":"Mini"}`}
	cfg := payloadConfig(t, pull(t, h, device, 100, ""))
	if _, ok := cfg["wifi_ssid"]; ok {
		t.Fatal("stale stored wifi_ssid pushed back to the frame")
	}
	if _, ok := cfg["wifi_password"]; ok {
		t.Fatal("wifi_password pushed")
	}
}

func TestWifiChangeTooLongForHeaderFallsBack(t *testing.T) {
	big := `{"device_name":"` + strings.Repeat("x", 1990) + `"}`
	p := withWifiChange(`{"config":`+big+`,"config_last_updated":1}`, "Guest", "secret-pass")
	cfg := payloadConfig(t, p)
	if cfg["wifi_ssid"] != "Guest" || len(cfg) != 2 || len(p) > maxConfigPayloadBytes {
		t.Fatalf("fallback not wifi-only: %d bytes", len(p))
	}
}

func TestASCIIJSONSurrogates(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"s": "a😀ö"})
	out := asciiJSON(in)
	var back map[string]string
	if err := json.Unmarshal([]byte(out), &back); err != nil || back["s"] != "a😀ö" {
		t.Fatalf("round trip failed: %s -> %v %v", out, back, err)
	}
}
