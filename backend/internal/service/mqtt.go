package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	mqtt "github.com/eclipse/paho.mqtt.golang"
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
func (s *MQTTService) prevImageTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/previous_image", s.cfg.baseTopic, id)
}
func (s *MQTTService) nextImageTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/next_image", s.cfg.baseTopic, id)
}

// nextAvailabilityTopic gates the Next Image entity's availability: published
// "offline" when the frame can't show a deterministic next image (collage on /
// non-ordered source) so HA shows it as Unavailable ("disabled").
func (s *MQTTService) nextAvailabilityTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/next_available", s.cfg.baseTopic, id)
}

// prevAvailabilityTopic gates the Previous Image entity's availability: published
// "offline" when there is no previous thumbnail yet (fresh device / after a source
// change), so HA shows it as Unavailable instead of rendering the empty image
// payload as a broken-image icon.
func (s *MQTTService) prevAvailabilityTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/prev_available", s.cfg.baseTopic, id)
}

// thumbExists reports whether the 400px thumbnail file for thumbID is on disk.
func (s *MQTTService) thumbExists(thumbID string) bool {
	if thumbID == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(s.dataDir, fmt.Sprintf("thumb_%s.jpg", thumbID)))
	return err == nil
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
	for _, key := range []string{"current", "previous", "next"} {
		client.Publish(s.discoveryTopic("image", deviceID, key), 1, true, "")
	}
	client.Publish(s.discoveryTopic("binary_sensor", deviceID, "collage"), 1, true, "")
	client.Publish(s.discoveryTopic("binary_sensor", deviceID, "deep_sleep"), 1, true, "")
	client.Publish(s.stateTopic(deviceID), 1, true, "")
	client.Publish(s.imageTopic(deviceID), 1, true, "")
	client.Publish(s.prevImageTopic(deviceID), 1, true, "")
	client.Publish(s.nextImageTopic(deviceID), 1, true, "")
	client.Publish(s.nextAvailabilityTopic(deviceID), 1, true, "")
	client.Publish(s.prevAvailabilityTopic(deviceID), 1, true, "")
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
	// Gate the Next Image entity's availability on whether a deterministic next
	// image exists for this frame.
	avail := "offline"
	if s.deviceSupportsNext(device) {
		avail = "online"
	}
	client.Publish(s.nextAvailabilityTopic(device.ID), 1, true, avail)
	// Gate the Previous Image entity on whether a previous thumbnail actually
	// exists, so an absent one shows as Unavailable instead of a broken-image icon.
	prevAvail := "offline"
	if s.thumbExists(device.PrevThumbID) {
		prevAvail = "online"
	}
	client.Publish(s.prevAvailabilityTopic(device.ID), 1, true, prevAvail)
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
	// Frame poll / schedule config (from the synced device_config) + network id.
	pc := parsePollConfig(device.DeviceConfig)
	if pc.rotateInterval > 0 {
		state["refresh_interval"] = round1(float64(pc.rotateInterval) / 60.0)
		// "Next image pull" mirrors the firmware's wake scheduler: when the frame
		// uses aligned rotation it wakes on clock-grid boundaries (e.g. :00/:15/:30
		// for a 15-min interval), NOT last-check-in + interval — so an off-cycle
		// button press doesn't push the next auto-pull forward. Only published when
		// auto-rotate is on (otherwise there is no scheduled pull). Still not
		// sleep-schedule-adjusted; the Sleep Schedule sensor gives that context.
		if pc.autoRotate && est.HasData && !est.LastSampledAt.IsZero() {
			if next := computeNextPull(est.LastSampledAt, pc); !next.IsZero() {
				state["next_pull"] = next.UTC().Format(time.RFC3339)
			}
		}
	}
	state["sleep_schedule"] = pc.sleepScheduleString()
	if device.Host != "" {
		state["host"] = device.Host
	}
	if device.LastIP != "" {
		state["ip_address"] = device.LastIP
	}
	// Frame timezone (POSIX TZ string), how the frame is mounted (display rotation),
	// which photo-ordering mode it uses, the server it pulls images from, and what
	// triggered the most recent image change.
	if pc.timezone != "" {
		state["timezone"] = pc.timezone
	}
	state["rotation"] = device.DisplayRotationDeg
	state["display_order"] = displayOrderLabel(device.DisplayOrder)
	if host := serverHostFromURL(pc.imageURL); host != "" {
		state["server_host"] = host
	}
	if t := triggerLabel(device.LastTrigger); t != "" {
		state["trigger"] = t
	}
	// Deep-sleep on/off (binary_sensor); the frame deep-sleeps between rotations.
	if pc.deepSleep {
		state["deep_sleep"] = "ON"
	} else {
		state["deep_sleep"] = "OFF"
	}

	// Collage flag + an explanation of the Next Image entity's state. The Next
	// Image preview only works for ordered DB-backed sources with collage off
	// (collage shuffles random pairs, so there is no deterministic next image);
	// surface why so the (unavailable) Next Image entity isn't a mystery.
	if device.EnableCollage {
		state["collage"] = "ON"
	} else {
		state["collage"] = "OFF"
	}
	state["next_image_status"] = s.nextImageStatus(device)

	payload, _ := json.Marshal(state)
	client.Publish(s.stateTopic(device.ID), 1, true, payload)
}

// deviceSupportsNext reports whether a frame can show a truthful Next Image
// preview: an ordered DB-backed source with collage disabled.
func (s *MQTTService) deviceSupportsNext(device *model.Device) bool {
	return !device.EnableCollage && model.IsOrderedSource(deviceSourceFromConfig(device.DeviceConfig))
}

// nextImageStatus is the human explanation shown as the "Next Image Status"
// sensor, telling the user why the Next Image entity is (un)available.
func (s *MQTTService) nextImageStatus(device *model.Device) string {
	if s.deviceSupportsNext(device) {
		return "Active"
	}
	if device.EnableCollage {
		return "Disabled — collage mode shuffles random pairs, so there is no fixed next image"
	}
	return "Disabled — this image source has no deterministic next image"
}

// pollConfig holds the frame's rotation / sleep settings parsed from the synced
// device_config blob (the same JSON the config-sync pushes to the frame).
type pollConfig struct {
	rotateInterval int  // seconds between image pulls
	autoRotate     bool // frame auto-rotates on a timer (vs button-only)
	aligned        bool // wake snapped to clock-grid boundaries (auto_rotate_aligned)
	sleepEnabled   bool // quiet-hours schedule active
	sleepStart     int  // minutes since local midnight
	sleepEnd       int
	timezone       string // frame's POSIX TZ string (e.g. "UTC0", "CET-1CEST,M3.5.0,...")
	deepSleep      bool   // deep_sleep_enabled (frame deep-sleeps between rotations)
	imageURL       string // configured image source URL (to derive the server host)
}

func parsePollConfig(deviceConfig string) pollConfig {
	// auto_rotate + deep_sleep default on when the key is absent (older firmware /
	// the FireBeetle's normal mode); the others default to their zero value.
	pc := pollConfig{autoRotate: true, deepSleep: true}
	if deviceConfig == "" {
		return pc
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(deviceConfig), &cfg); err != nil {
		return pc
	}
	if v, ok := cfg["rotate_interval"].(float64); ok {
		pc.rotateInterval = int(v)
	}
	if v, ok := cfg["auto_rotate"].(bool); ok {
		pc.autoRotate = v
	}
	if v, ok := cfg["auto_rotate_aligned"].(bool); ok {
		pc.aligned = v
	}
	if v, ok := cfg["sleep_schedule_enabled"].(bool); ok {
		pc.sleepEnabled = v
	}
	if v, ok := cfg["sleep_schedule_start"].(float64); ok {
		pc.sleepStart = int(v)
	}
	if v, ok := cfg["sleep_schedule_end"].(float64); ok {
		pc.sleepEnd = int(v)
	}
	if v, ok := cfg["timezone"].(string); ok {
		pc.timezone = strings.TrimSpace(v)
	}
	if v, ok := cfg["deep_sleep_enabled"].(bool); ok {
		pc.deepSleep = v
	}
	if v, ok := cfg["image_url"].(string); ok {
		pc.imageURL = strings.TrimSpace(v)
	}
	return pc
}

// serverHostFromURL extracts the "host[:port]" the frame pulls images from out of
// its configured image_url, for the HA "Server Host" sensor. Empty when the URL
// is blank or unparseable.
func serverHostFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// displayOrderLabel maps the stored display-order mode to a human label for the
// HA "Image Order" sensor.
func displayOrderLabel(order string) string {
	switch order {
	case model.DisplayOrderShuffle:
		return "Shuffle"
	case model.DisplayOrderChronoNewest:
		return "Newest first"
	case model.DisplayOrderChronoOldest:
		return "Oldest first"
	case model.DisplayOrderCustom:
		return "Custom"
	default:
		return "Shuffle"
	}
}

// triggerLabel maps the stored last-trigger token to a human label for the HA
// "Last Trigger" sensor.
func triggerLabel(t string) string {
	switch t {
	case "timer":
		return "Timer"
	case "button":
		return "Button"
	case "boot":
		return "Boot"
	case "push":
		return "Push"
	case "pull":
		return "Pull"
	default:
		return ""
	}
}

// computeNextPull predicts when the frame next wakes to fetch an image, mirroring
// the firmware's calculate_next_wakeup_interval. With aligned rotation the wake
// snaps to the next clock-grid boundary (a multiple of rotateInterval), with the
// firmware's "skip if <60s away" guard — so a mid-cycle button press doesn't push
// the estimate forward. Without alignment it's simply last-seen + interval.
// (Alignment is computed on the absolute UTC grid, which matches the firmware's
// local-time-of-day grid for the usual intervals that evenly divide both the hour
// and any whole/half-hour timezone offset. Not sleep-schedule adjusted.)
func computeNextPull(lastSeen time.Time, pc pollConfig) time.Time {
	iv := int64(pc.rotateInterval)
	if iv <= 0 {
		return time.Time{}
	}
	if !pc.aligned {
		return lastSeen.Add(time.Duration(iv) * time.Second)
	}
	t := lastSeen.Unix()
	next := (t/iv + 1) * iv // strictly the next grid point
	if next-t < 60 {
		next += iv
	}
	return time.Unix(next, 0).UTC()
}

// sleepScheduleString renders the quiet-hours window as "HH:MM–HH:MM" (local
// clock, as configured on the frame), or "Off" when disabled.
func (pc pollConfig) sleepScheduleString() string {
	if !pc.sleepEnabled {
		return "Off"
	}
	return fmt.Sprintf("%s–%s", formatMinutes(pc.sleepStart), formatMinutes(pc.sleepEnd))
}

func formatMinutes(m int) string {
	m = ((m % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func (s *MQTTService) publishImage(client mqtt.Client, device *model.Device) {
	s.publishOneImage(client, s.imageTopic(device.ID), device.CurrentThumbID)
	s.publishOneImage(client, s.prevImageTopic(device.ID), device.PrevThumbID)
	s.publishOneImage(client, s.nextImageTopic(device.ID), device.NextThumbID)
}

// publishOneImage publishes the JPEG thumbnail for thumbID to topic (retained),
// or clears the retained image with an empty payload when there is none (e.g. a
// device with no previous image yet, or a source without a deterministic next).
func (s *MQTTService) publishOneImage(client mqtt.Client, topic, thumbID string) {
	if thumbID == "" {
		client.Publish(topic, 1, true, []byte{})
		return
	}
	data, err := os.ReadFile(filepath.Join(s.dataDir, fmt.Sprintf("thumb_%s.jpg", thumbID)))
	if err != nil {
		client.Publish(topic, 1, true, []byte{})
		return
	}
	client.Publish(topic, 1, true, data)
}

// mqttSensorKeys are the value_json keys exposed as HA sensors.
var mqttSensorKeys = []string{"battery", "battery_voltage", "days_remaining", "trend", "source", "last_seen", "refresh_interval", "sleep_schedule", "next_pull", "host", "ip_address", "next_image_status", "timezone", "rotation", "display_order", "server_host", "trigger"}

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

	// Frame poll / schedule + network identity.
	sensor("refresh_interval", "Refresh Interval", "refresh_interval", map[string]interface{}{
		"unit_of_measurement": "min", "icon": "mdi:timer-sync-outline", "entity_category": "diagnostic",
	})
	sensor("next_pull", "Next Image Pull", "next_pull", map[string]interface{}{
		"device_class": "timestamp", "icon": "mdi:timer-play-outline",
	})
	sensor("sleep_schedule", "Sleep Schedule", "sleep_schedule", map[string]interface{}{
		"icon": "mdi:weather-night", "entity_category": "diagnostic",
	})
	sensor("host", "Host", "host", map[string]interface{}{
		"icon": "mdi:lan", "entity_category": "diagnostic",
	})
	sensor("ip_address", "IP Address", "ip_address", map[string]interface{}{
		"icon": "mdi:ip-network-outline", "entity_category": "diagnostic",
	})
	// Explains why Next Image is (un)available — see nextImageStatus().
	sensor("next_image_status", "Next Image Status", "next_image_status", map[string]interface{}{
		"icon": "mdi:image-sync-outline", "entity_category": "diagnostic",
	})

	// Frame timezone, mount rotation, photo-ordering mode, the server it pulls
	// from, and what triggered the most recent image change.
	sensor("timezone", "Timezone", "timezone", map[string]interface{}{
		"icon": "mdi:map-clock", "entity_category": "diagnostic",
	})
	sensor("rotation", "Display Rotation", "rotation", map[string]interface{}{
		"unit_of_measurement": "°", "icon": "mdi:screen-rotation", "entity_category": "diagnostic",
	})
	sensor("display_order", "Image Order", "display_order", map[string]interface{}{
		"icon": "mdi:sort",
	})
	sensor("server_host", "Server Host", "server_host", map[string]interface{}{
		"icon": "mdi:server-network", "entity_category": "diagnostic",
	})
	sensor("trigger", "Last Trigger", "trigger", map[string]interface{}{
		"icon": "mdi:gesture-tap-button",
	})

	// Deep-sleep on/off flag (binary_sensor like collage).
	deepSleepCfg := map[string]interface{}{
		"name":            "Deep Sleep",
		"unique_id":       fmt.Sprintf("photoframe_%d_deep_sleep", device.ID),
		"object_id":       fmt.Sprintf("photoframe_%s_deep_sleep", sanitize(device.Name)),
		"state_topic":     state,
		"value_template":  "{{ value_json.deep_sleep }}",
		"payload_on":      "ON",
		"payload_off":     "OFF",
		"icon":            "mdi:power-sleep",
		"entity_category": "diagnostic",
		"availability":    avail,
		"device":          dev,
	}
	deepSleepPayload, _ := json.Marshal(deepSleepCfg)
	client.Publish(s.discoveryTopic("binary_sensor", device.ID, "deep_sleep"), 1, true, deepSleepPayload)

	// Collage on/off flag.
	collageCfg := map[string]interface{}{
		"name":            "Collage",
		"unique_id":       fmt.Sprintf("photoframe_%d_collage", device.ID),
		"object_id":       fmt.Sprintf("photoframe_%s_collage", sanitize(device.Name)),
		"state_topic":     state,
		"value_template":  "{{ value_json.collage }}",
		"payload_on":      "ON",
		"payload_off":     "OFF",
		"icon":            "mdi:grid",
		"entity_category": "diagnostic",
		"availability":    avail,
		"device":          dev,
	}
	collagePayload, _ := json.Marshal(collageCfg)
	client.Publish(s.discoveryTopic("binary_sensor", device.ID, "collage"), 1, true, collagePayload)

	// Image entities: what's on the frame now (current), what was on it before
	// (previous), and a non-mutating preview of what the next pull will show
	// (next). availability lets "next" be gated independently: it goes Unavailable
	// ("disabled") when the frame can't show a deterministic next image.
	imageEntity := func(discoveryKey, idSuffix, name, topic string, availability []map[string]string, mode string) {
		cfg := map[string]interface{}{
			"name":         name,
			"unique_id":    fmt.Sprintf("photoframe_%d_%s", device.ID, idSuffix),
			"object_id":    fmt.Sprintf("photoframe_%s_%s", sanitize(device.Name), idSuffix),
			"image_topic":  topic,
			"content_type": "image/jpeg",
			"availability": availability,
			"device":       dev,
		}
		if mode != "" {
			cfg["availability_mode"] = mode
		}
		payload, _ := json.Marshal(cfg)
		client.Publish(s.discoveryTopic("image", device.ID, discoveryKey), 1, true, payload)
	}
	// "current" keeps its original unique_id ("..._image") so existing HA
	// entities aren't orphaned; previous/next are new.
	imageEntity("current", "image", "Current Image", s.imageTopic(device.ID), avail, "")
	// Previous is available only when BOTH the bridge is online AND a previous
	// thumbnail exists (prev_available topic) — hence availability_mode "all" — so
	// a frame with no previous image yet shows Unavailable, not a broken icon.
	prevAvail := append([]map[string]string{}, avail...)
	prevAvail = append(prevAvail, map[string]string{
		"topic":                 s.prevAvailabilityTopic(device.ID),
		"payload_available":     "online",
		"payload_not_available": "offline",
	})
	imageEntity("previous", "previous_image", "Previous Image", s.prevImageTopic(device.ID), prevAvail, "all")
	// Next is available only when BOTH the bridge is online AND a deterministic
	// next image exists (next_available topic) — hence availability_mode "all".
	nextAvail := append([]map[string]string{}, avail...)
	nextAvail = append(nextAvail, map[string]string{
		"topic":                 s.nextAvailabilityTopic(device.ID),
		"payload_available":     "online",
		"payload_not_available": "offline",
	})
	imageEntity("next", "next_image", "Next Image", s.nextImageTopic(device.ID), nextAvail, "all")
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
