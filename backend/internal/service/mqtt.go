package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"gorm.io/gorm"
)

// MQTTService publishes each photo frame to a Home Assistant MQTT broker using
// HA's MQTT-discovery convention, so frames show up as devices with battery /
// status / current-image entities that automations can react to. The server is
// a plain MQTT client — it does NOT need to run as an HA add-on; it just needs
// network access to the broker (e.g. HA's Mosquitto) and its own credentials.
type MQTTService struct {
	db       *gorm.DB
	settings *SettingsService
	battery  *BatteryService
	dataDir  string

	mu        sync.Mutex
	client    mqtt.Client
	cfg       mqttConfig
	connected bool
	// discoverySent tracks which devices already had their HA discovery configs
	// published on the current connection (reset on every (re)connect).
	discoverySent map[uint]bool

	reloadMu    sync.Mutex
	reloadTimer *time.Timer
}

type mqttConfig struct {
	enabled         bool
	host            string
	port            int
	username        string
	password        string
	discoveryPrefix string // HA discovery topic prefix, default "homeassistant"
	baseTopic       string // our own state/image topic root, default "esp32photoframe"
}

func NewMQTTService(db *gorm.DB, settings *SettingsService, battery *BatteryService, dataDir string) *MQTTService {
	return &MQTTService{
		db:            db,
		settings:      settings,
		battery:       battery,
		dataDir:       dataDir,
		discoverySent: make(map[uint]bool),
	}
}

func (s *MQTTService) loadConfig() mqttConfig {
	get := func(k string) string { v, _ := s.settings.Get(k); return v }
	port, _ := strconv.Atoi(get("mqtt_port"))
	if port <= 0 {
		port = 1883
	}
	discoveryPrefix := get("mqtt_discovery_prefix")
	if discoveryPrefix == "" {
		discoveryPrefix = "homeassistant"
	}
	baseTopic := get("mqtt_base_topic")
	if baseTopic == "" {
		baseTopic = "esp32photoframe"
	}
	return mqttConfig{
		enabled:         get("mqtt_enabled") == "true",
		host:            strings.TrimSpace(get("mqtt_host")),
		port:            port,
		username:        get("mqtt_username"),
		password:        get("mqtt_password"),
		discoveryPrefix: strings.Trim(discoveryPrefix, "/"),
		baseTopic:       strings.Trim(baseTopic, "/"),
	}
}

// Start brings the client up from stored settings and re-applies config whenever
// the MQTT settings change.
func (s *MQTTService) Start() {
	s.settings.RegisterOnChange(func(key, _ string) {
		if strings.HasPrefix(key, "mqtt_") {
			s.Reload()
		}
	})
	s.Reload()
}

// Reload reconnects with the current settings. Saving the MQTT form changes
// several settings keys in quick succession (each firing the change callback),
// so coalesce them with a short debounce to avoid spinning up competing clients
// that share a client ID and kick each other off the broker.
func (s *MQTTService) Reload() {
	s.reloadMu.Lock()
	if s.reloadTimer != nil {
		s.reloadTimer.Stop()
	}
	s.reloadTimer = time.AfterFunc(400*time.Millisecond, s.doReload)
	s.reloadMu.Unlock()
}

func (s *MQTTService) doReload() {
	cfg := s.loadConfig()
	s.mu.Lock()
	s.disconnectLocked()
	s.cfg = cfg
	s.mu.Unlock()
	if !cfg.enabled || cfg.host == "" {
		return
	}
	s.connect(cfg)
}

func (s *MQTTService) bridgeAvailabilityTopic() string {
	return s.cfg.baseTopic + "/bridge/availability"
}
func (s *MQTTService) stateTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/state", s.cfg.baseTopic, id)
}
func (s *MQTTService) imageTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/image", s.cfg.baseTopic, id)
}

func (s *MQTTService) connect(cfg mqttConfig) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.host, cfg.port))
	// Unique client ID per instance: a fixed ID means two servers sharing one
	// broker (e.g. dev + prod, or two containers) kick each other off in an
	// endless connect/EOF loop. The hostname is the container ID under Docker,
	// so it is unique per running instance.
	clientID := "esp32-photoframe-server"
	if host, err := os.Hostname(); err == nil && host != "" {
		clientID = clientID + "-" + host
	}
	opts.SetClientID(clientID)
	if cfg.username != "" {
		opts.SetUsername(cfg.username)
		opts.SetPassword(cfg.password)
	}
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(30 * time.Second)
	opts.SetConnectTimeout(10 * time.Second)
	opts.SetWill(s.bridgeAvailabilityTopic(), "offline", 1, true)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		fmt.Println("[mqtt] connected to broker")
		s.mu.Lock()
		s.connected = true
		s.discoverySent = make(map[uint]bool) // re-publish discovery on every (re)connect
		s.mu.Unlock()
		c.Publish(s.bridgeAvailabilityTopic(), 1, true, "online").Wait()
		s.publishAllDevices()
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		fmt.Printf("[mqtt] connection lost: %v\n", err)
		s.mu.Lock()
		s.connected = false
		s.mu.Unlock()
	})

	client := mqtt.NewClient(opts)
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	// Connect in the background; SetConnectRetry keeps trying if the broker is
	// down so a wrong host never blocks startup.
	go func() {
		token := client.Connect()
		token.WaitTimeout(12 * time.Second)
		if err := token.Error(); err != nil {
			fmt.Printf("[mqtt] initial connect error: %v\n", err)
		}
	}()
}

func (s *MQTTService) disconnectLocked() {
	if s.client != nil {
		if s.client.IsConnectionOpen() {
			// Mark bridge offline cleanly before dropping the connection.
			s.client.Publish(s.bridgeAvailabilityTopic(), 1, true, "offline").WaitTimeout(2 * time.Second)
		}
		// Disconnect unconditionally so a mid-connect client's auto-reconnect
		// loop is also torn down (otherwise orphaned clients fight over the
		// shared client ID).
		s.client.Disconnect(250)
	}
	s.client = nil
	s.connected = false
}

// Status reports whether the broker connection is currently up (for the WebGUI).
func (s *MQTTService) Status() (enabled bool, connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.enabled, s.connected && s.client != nil && s.client.IsConnectionOpen()
}

func (s *MQTTService) isReady() (mqtt.Client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.enabled || s.client == nil || !s.client.IsConnectionOpen() {
		return nil, false
	}
	return s.client, true
}

func (s *MQTTService) publishAllDevices() {
	var devices []model.Device
	if err := s.db.Find(&devices).Error; err != nil {
		return
	}
	for i := range devices {
		s.publishDevice(&devices[i])
	}
}

// NotifyDeviceUpdated publishes fresh state for one device (called when a frame
// checks in / its image changes). Non-blocking and safe to call when MQTT is off.
func (s *MQTTService) NotifyDeviceUpdated(deviceID uint) {
	if _, ok := s.isReady(); !ok {
		return
	}
	go func() {
		// The serve path records the battery sample + updates current_thumb_id
		// asynchronously; a short delay lets those commit so we publish the
		// fresh reading and image rather than the previous rotation's.
		time.Sleep(1500 * time.Millisecond)
		var device model.Device
		if err := s.db.First(&device, deviceID).Error; err != nil {
			return
		}
		s.publishDevice(&device)
	}()
}

// RemoveDevice clears a device's retained topics + discovery (on delete).
func (s *MQTTService) RemoveDevice(deviceID uint) {
	client, ok := s.isReady()
	if !ok {
		return
	}
	for _, key := range mqttSensorKeys {
		client.Publish(s.discoveryTopic("sensor", deviceID, key), 1, true, "")
	}
	client.Publish(s.discoveryTopic("image", deviceID, "current"), 1, true, "")
	client.Publish(s.stateTopic(deviceID), 1, true, "")
	client.Publish(s.imageTopic(deviceID), 1, true, "")
	s.mu.Lock()
	delete(s.discoverySent, deviceID)
	s.mu.Unlock()
}

func (s *MQTTService) publishDevice(device *model.Device) {
	client, ok := s.isReady()
	if !ok {
		return
	}
	s.mu.Lock()
	if !s.discoverySent[device.ID] {
		s.publishDiscovery(client, device)
		s.discoverySent[device.ID] = true
	}
	s.mu.Unlock()
	s.publishState(client, device)
	s.publishImage(client, device)
}

func (s *MQTTService) publishState(client mqtt.Client, device *model.Device) {
	est := s.battery.Estimate(device.ID)
	state := map[string]interface{}{
		"name":   device.Name,
		"source": deviceSourceFromConfig(device.DeviceConfig),
	}
	if est.HasData {
		if est.CurrentPercent >= 0 {
			state["battery"] = est.CurrentPercent
		}
		if est.CurrentVoltageMV > 0 {
			state["battery_voltage"] = float64(est.CurrentVoltageMV) / 1000.0
		}
		if est.DaysRemaining >= 0 {
			state["days_remaining"] = round1(est.DaysRemaining)
		}
		if est.Trend != "" {
			state["trend"] = est.Trend
		}
		if !est.LastSampledAt.IsZero() {
			state["last_seen"] = est.LastSampledAt.UTC().Format(time.RFC3339)
		}
	}
	payload, _ := json.Marshal(state)
	client.Publish(s.stateTopic(device.ID), 1, true, payload)
}

func (s *MQTTService) publishImage(client mqtt.Client, device *model.Device) {
	if device.CurrentThumbID == "" {
		return
	}
	path := filepath.Join(s.dataDir, fmt.Sprintf("thumb_%s.jpg", device.CurrentThumbID))
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	client.Publish(s.imageTopic(device.ID), 1, true, data)
}

// mqttSensorKeys are the value_json keys exposed as HA sensors.
var mqttSensorKeys = []string{"battery", "battery_voltage", "days_remaining", "trend", "source", "last_seen"}

func (s *MQTTService) discoveryTopic(component string, id uint, key string) string {
	return fmt.Sprintf("%s/%s/photoframe_%d_%s/config", s.cfg.discoveryPrefix, component, id, key)
}

func (s *MQTTService) publishDiscovery(client mqtt.Client, device *model.Device) {
	model_ := device.BoardName
	if model_ == "" {
		model_ = "ESP32 PhotoFrame"
	}
	dev := map[string]interface{}{
		"identifiers":  []string{fmt.Sprintf("esp32photoframe_%d", device.ID)},
		"name":         device.Name,
		"manufacturer": "ESP32 PhotoFrame",
		"model":        model_,
	}
	avail := []map[string]string{{
		"topic":                 s.bridgeAvailabilityTopic(),
		"payload_available":     "online",
		"payload_not_available": "offline",
	}}
	state := s.stateTopic(device.ID)

	sensor := func(key, name, valueKey string, extra map[string]interface{}) {
		cfg := map[string]interface{}{
			"name":           name,
			"unique_id":      fmt.Sprintf("photoframe_%d_%s", device.ID, key),
			"object_id":      fmt.Sprintf("photoframe_%s_%s", sanitize(device.Name), key),
			"state_topic":    state,
			"value_template": fmt.Sprintf("{{ value_json.%s }}", valueKey),
			"availability":   avail,
			"device":         dev,
		}
		for k, v := range extra {
			cfg[k] = v
		}
		payload, _ := json.Marshal(cfg)
		client.Publish(s.discoveryTopic("sensor", device.ID, key), 1, true, payload)
	}

	sensor("battery", "Battery", "battery", map[string]interface{}{
		"device_class": "battery", "unit_of_measurement": "%", "state_class": "measurement",
	})
	sensor("battery_voltage", "Battery Voltage", "battery_voltage", map[string]interface{}{
		"device_class": "voltage", "unit_of_measurement": "V", "state_class": "measurement",
		"entity_category": "diagnostic",
	})
	sensor("days_remaining", "Battery Days Remaining", "days_remaining", map[string]interface{}{
		"unit_of_measurement": "d", "icon": "mdi:battery-clock", "state_class": "measurement",
	})
	sensor("trend", "Battery Trend", "trend", map[string]interface{}{"icon": "mdi:trending-down"})
	sensor("source", "Image Source", "source", map[string]interface{}{"icon": "mdi:image-multiple"})
	sensor("last_seen", "Last Seen", "last_seen", map[string]interface{}{"device_class": "timestamp"})

	// Current image entity.
	imgCfg := map[string]interface{}{
		"name":         "Current Image",
		"unique_id":    fmt.Sprintf("photoframe_%d_image", device.ID),
		"object_id":    fmt.Sprintf("photoframe_%s_image", sanitize(device.Name)),
		"image_topic":  s.imageTopic(device.ID),
		"content_type": "image/jpeg",
		"availability": avail,
		"device":       dev,
	}
	payload, _ := json.Marshal(imgCfg)
	client.Publish(s.discoveryTopic("image", device.ID, "current"), 1, true, payload)
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func sanitize(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "frame"
	}
	return out
}

// deviceSourceFromConfig extracts the "/image/<source>" tail from the device's
// stored config image_url, matching how the frame's rotation source is set.
func deviceSourceFromConfig(deviceConfig string) string {
	if deviceConfig == "" {
		return "unknown"
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(deviceConfig), &cfg); err != nil {
		return "unknown"
	}
	url, _ := cfg["image_url"].(string)
	if url == "" {
		return "unknown"
	}
	if i := strings.Index(url, "/image/"); i >= 0 {
		tail := url[i+len("/image/"):]
		if q := strings.IndexAny(tail, "?/"); q >= 0 {
			tail = tail[:q]
		}
		if tail != "" {
			return tail
		}
	}
	return "unknown"
}
