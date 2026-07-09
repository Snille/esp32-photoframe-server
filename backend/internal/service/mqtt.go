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
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/photoframe"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/safego"
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
	auth     *AuthService
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

func NewMQTTService(db *gorm.DB, settings *SettingsService, battery *BatteryService, auth *AuthService, dataDir string) *MQTTService {
	return &MQTTService{
		db:            db,
		settings:      settings,
		battery:       battery,
		auth:          auth,
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
	// Periodically re-publish device state so time-sensitive sensors stay fresh
	// without a frame check-in — chiefly the per-device online/offline sensor,
	// which must flip to offline once a frame stops reporting. A no-op while the
	// broker is disconnected (isReady guards it). Discovery isn't re-sent (it's
	// gated by discoverySent), only state.
	//
	// This ticker runs for the lifetime of the process, so a panic anywhere in
	// publishAllDevices (already self-guarded, but defense in depth) would
	// otherwise end the loop or, unrecovered, take the entire server down —
	// silently stopping every frame from being served.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			if _, ok := s.isReady(); ok {
				safego.Safe("mqtt publishAllDevices (ticker)", s.publishAllDevices)
			}
		}
	}()
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
		// Listen for control commands from Home Assistant (select/number/switch/
		// button entities publish the desired value here). Subscribed on every
		// (re)connect.
		c.Subscribe(s.cfg.baseTopic+"/device/+/cmd/+", 1, s.onCommand)
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
	safego.Go("mqtt connect", func() {
		token := client.Connect()
		token.WaitTimeout(12 * time.Second)
		if err := token.Error(); err != nil {
			fmt.Printf("[mqtt] initial connect error: %v\n", err)
		}
	})
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
		device := &devices[i]
		safego.Safe(fmt.Sprintf("mqtt publishDevice(%d)", device.ID), func() {
			s.publishDevice(device)
		})
	}
}

// NotifyDeviceUpdated publishes fresh state for one device (called when a frame
// checks in / its image changes). Non-blocking and safe to call when MQTT is off.
func (s *MQTTService) NotifyDeviceUpdated(deviceID uint) {
	if _, ok := s.isReady(); !ok {
		return
	}
	safego.Go(fmt.Sprintf("mqtt NotifyDeviceUpdated(%d)", deviceID), func() {
		// The serve path records the battery sample + updates current_thumb_id
		// asynchronously; a short delay lets those commit so we publish the
		// fresh reading and image rather than the previous rotation's.
		time.Sleep(1500 * time.Millisecond)
		var device model.Device
		if err := s.db.First(&device, deviceID).Error; err != nil {
			return
		}
		s.publishDevice(&device)
	})
}

// RepublishDiscovery forces HA discovery configs to be re-sent for one device,
// then republishes its state/image. Called when a device's identity-adjacent
// fields change in the web UI (notably its name): the discovery config carries
// the HA friendly name + object_id, but it is otherwise sent only ONCE per broker
// connection (gated by discoverySent), so without this a rename wouldn't reach HA
// until the next MQTT reconnect. Existing HA entities keep their history because
// their unique_id / identifiers are keyed on the device ID, not the name.
func (s *MQTTService) RepublishDiscovery(deviceID uint) {
	if _, ok := s.isReady(); !ok {
		return
	}
	s.mu.Lock()
	delete(s.discoverySent, deviceID)
	s.mu.Unlock()
	var device model.Device
	if err := s.db.First(&device, deviceID).Error; err != nil {
		return
	}
	s.publishDevice(&device)
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
	// Control entities (select/number/switch/button) + their gating topic.
	client.Publish(s.discoveryTopic("select", deviceID, "source"), 1, true, "")
	client.Publish(s.discoveryTopic("select", deviceID, "image_order"), 1, true, "")
	client.Publish(s.discoveryTopic("number", deviceID, "refresh_interval"), 1, true, "")
	client.Publish(s.discoveryTopic("switch", deviceID, "deep_sleep"), 1, true, "")
	client.Publish(s.discoveryTopic("switch", deviceID, "auto_rotate"), 1, true, "")
	client.Publish(s.discoveryTopic("button", deviceID, "rotate"), 1, true, "")
	client.Publish(s.discoveryTopic("button", deviceID, "reshuffle"), 1, true, "")
	client.Publish(s.discoveryTopic("number", deviceID, "skip"), 1, true, "")
	client.Publish(s.discoveryTopic("button", deviceID, "hide"), 1, true, "")
	client.Publish(s.discoveryTopic("button", deviceID, "favorite"), 1, true, "")
	client.Publish(s.discoveryTopic("switch", deviceID, "on_this_day"), 1, true, "")
	client.Publish(s.discoveryTopic("switch", deviceID, "favorites_only"), 1, true, "")
	client.Publish(s.discoveryTopic("switch", deviceID, "auto_update"), 1, true, "")
	client.Publish(s.discoveryTopic("binary_sensor", deviceID, "online"), 1, true, "")
	client.Publish(s.discoveryTopic("binary_sensor", deviceID, "current_favorite"), 1, true, "")
	client.Publish(s.controlsAvailabilityTopic(deviceID), 1, true, "")
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
	// Gate the "Rotate Now" button on whether the board can act on a live command.
	controls := "offline"
	if boardSupportsLiveControl(device.BoardName) {
		controls = "online"
	}
	client.Publish(s.controlsAvailabilityTopic(device.ID), 1, true, controls)
}

// controlsAvailabilityTopic gates the "Rotate Now" button: "offline" for boards
// that can't act on a live command (deep-sleeping FireBeetle), so HA shows it
// Unavailable until a capable board is connected.
func (s *MQTTService) controlsAvailabilityTopic(id uint) string {
	return fmt.Sprintf("%s/device/%d/controls_available", s.cfg.baseTopic, id)
}

func (s *MQTTService) commandTopic(id uint, field string) string {
	return fmt.Sprintf("%s/device/%d/cmd/%s", s.cfg.baseTopic, id, field)
}

// boardSupportsLiveControl reports whether a board can act on an immediate
// "rotate now" push. The FireBeetle deep-sleeps almost always, so a live push
// only lands in a rare wake window — gate the button off there. Always-reachable
// boards (and unknown/newer boards) get it.
func boardSupportsLiveControl(boardName string) bool {
	switch boardName {
	case "dfrobot_firebeetle_esp32e":
		return false
	default:
		return true
	}
}

// onCommand routes an HA control command published to
// <base>/device/<id>/cmd/<field>.
// onCommand is invoked directly by the paho client's own internal goroutine
// (once per incoming HA command message) — outside Echo's Recover()
// middleware and outside our own goroutines, so it needs its own panic guard
// too, same reasoning as safego elsewhere in this file.
func (s *MQTTService) onCommand(_ mqtt.Client, msg mqtt.Message) {
	safego.Safe("mqtt onCommand", func() {
		parts := strings.Split(msg.Topic(), "/")
		// Parse from the right so a base topic containing slashes still works.
		if len(parts) < 4 || parts[len(parts)-2] != "cmd" {
			return
		}
		field := parts[len(parts)-1]
		id64, err := strconv.ParseUint(parts[len(parts)-3], 10, 64)
		if err != nil {
			return
		}
		s.handleCommand(uint(id64), field, strings.TrimSpace(string(msg.Payload())))
	})
}

func (s *MQTTService) handleCommand(deviceID uint, field, payload string) {
	var device model.Device
	if err := s.db.First(&device, deviceID).Error; err != nil {
		return
	}
	switch field {
	case "image_order":
		order := labelToDisplayOrder(payload)
		s.db.Model(&device).Update("display_order", order)
		device.DisplayOrder = order
	case "source":
		// Swap the /image/<source> tail of the configured image_url (which points
		// back at this server), reissuing a device token for the new URL.
		s.applyConfigChange(&device, func(cfg map[string]interface{}) {
			if url, ok := cfg["image_url"].(string); ok {
				cfg["image_url"] = replaceSourceInURL(url, payload)
			}
		}, true)
	case "refresh_interval":
		mins, err := strconv.ParseFloat(payload, 64)
		if err != nil || mins <= 0 {
			return
		}
		s.applyConfigChange(&device, func(cfg map[string]interface{}) {
			cfg["rotate_interval"] = int(mins * 60)
		}, false)
	case "deep_sleep":
		on := payload == "ON"
		s.applyConfigChange(&device, func(cfg map[string]interface{}) { cfg["deep_sleep_enabled"] = on }, false)
	case "auto_rotate":
		on := payload == "ON"
		s.applyConfigChange(&device, func(cfg map[string]interface{}) { cfg["auto_rotate"] = on }, false)
	case "reshuffle":
		// Bump the shuffle seed → the next pull computes a fresh shuffle order.
		s.db.Model(&device).Update("shuffle_seed", device.ShuffleSeed+1)
		device.ShuffleSeed++
	case "skip":
		// Jump N steps in the rotation queue (positive = forward, negative = back,
		// 0 = re-show current); the next ordered pull serves the pinned image. No-op
		// for collage / non-ordered sources. The HA number snaps back to 0 via the
		// republish below.
		if n, err := strconv.Atoi(payload); err == nil {
			ApplySkip(s.db, &device, n)
		}
	case "hide":
		// Globally hide the photo currently on the frame; it's skipped from the
		// next pull onward (the frame keeps showing it until then).
		if id := lastServedImageID(s.db, deviceID, deviceSourceFromConfig(device.DeviceConfig)); id != 0 {
			s.db.Model(&model.Image{}).Where("id = ?", id).Update("hidden", true)
		}
	case "favorite":
		// Toggle the star on the photo currently on the frame.
		if id := lastServedImageID(s.db, deviceID, deviceSourceFromConfig(device.DeviceConfig)); id != 0 {
			var img model.Image
			if s.db.Select("favorite").First(&img, id).Error == nil {
				s.db.Model(&model.Image{}).Where("id = ?", id).Update("favorite", !img.Favorite)
			}
		}
	case "on_this_day":
		on := payload == "ON"
		s.db.Model(&device).Update("on_this_day", on)
		device.OnThisDay = on
	case "favorites_only":
		on := payload == "ON"
		s.db.Model(&device).Update("favorites_only", on)
		device.FavoritesOnly = on
	case "auto_update":
		// Server-owned + pushed to the frame via config-sync, so bump
		// config_last_updated to fire the X-Config-Payload on the next pull.
		on := payload == "ON"
		s.db.Model(&device).Updates(map[string]interface{}{
			"auto_update":         on,
			"config_last_updated": time.Now().Unix(),
		})
		device.AutoUpdate = on
	case "rotate":
		if !boardSupportsLiveControl(device.BoardName) || device.Host == "" {
			return
		}
		host := device.Host
		safego.Go(fmt.Sprintf("mqtt rotate(%d)", deviceID), func() {
			if err := photoframe.NewClient(host).Rotate(); err != nil {
				fmt.Printf("[mqtt] rotate command failed for device %d: %v\n", deviceID, err)
			}
		})
		return
	default:
		return
	}
	// Reflect the change back to HA.
	s.publishDevice(&device)
}

// applyConfigChange merges mutate(cfg) into the device's stored config, bumps the
// config timestamp (so an asleep frame picks it up via config-sync on its next
// pull), persists it, and pushes to an awake frame — mirroring the web UI's
// UpdateDeviceConfig so commands behave identically.
func (s *MQTTService) applyConfigChange(device *model.Device, mutate func(map[string]interface{}), reinjectToken bool) {
	var cfg map[string]interface{}
	if device.DeviceConfig != "" && device.DeviceConfig != "{}" {
		json.Unmarshal([]byte(device.DeviceConfig), &cfg)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	mutate(cfg)
	if reinjectToken && s.auth != nil {
		if url, ok := cfg["image_url"].(string); ok && strings.Contains(url, "/image/") {
			var user model.User
			if err := s.db.Order("id").First(&user).Error; err == nil && user.ID > 0 {
				if tok, err := s.auth.GetOrGenerateDeviceToken(user.ID, user.Username, device.Name, &device.ID); err == nil {
					cfg["access_token"] = tok
				}
			}
		}
	}
	merged, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	ts := time.Now().Unix()
	s.db.Model(device).Updates(map[string]interface{}{
		"device_config":       string(merged),
		"config_last_updated": ts,
	})
	device.DeviceConfig = string(merged)
	device.ConfigLastUpdated = ts
	if device.Host != "" {
		// Push best-effort in the background so a slow/unreachable frame doesn't
		// block this command handler (and thus the next queued command). An asleep
		// frame picks the change up via config-sync on its next pull regardless.
		host, id := device.Host, device.ID
		safego.Go(fmt.Sprintf("mqtt pushConfig(%d)", id), func() {
			if err := photoframe.NewClient(host).PushConfig(cfg); err != nil {
				fmt.Printf("[mqtt] config push to device %d failed (will sync on next pull): %v\n", id, err)
			}
		})
	}
}

// replaceSourceInURL swaps the "/image/<source>" tail of an image URL, preserving
// any query string or trailing path.
func replaceSourceInURL(raw, newSource string) string {
	i := strings.Index(raw, "/image/")
	if i < 0 || newSource == "" {
		return raw
	}
	base := raw[:i+len("/image/")]
	rest := raw[i+len("/image/"):]
	suffix := ""
	if q := strings.IndexAny(rest, "?/"); q >= 0 {
		suffix = rest[q:]
	}
	return base + newSource + suffix
}

// labelToDisplayOrder maps the HA "Image Order" select option back to the stored
// display-order value (inverse of displayOrderLabel; also accepts raw values).
func labelToDisplayOrder(label string) string {
	switch label {
	case "Shuffle":
		return model.DisplayOrderShuffle
	case "Newest first":
		return model.DisplayOrderChronoNewest
	case "Oldest first":
		return model.DisplayOrderChronoOldest
	case "Custom":
		return model.DisplayOrderCustom
	default:
		return model.NormalizeDisplayOrder(label)
	}
}

func (s *MQTTService) publishState(client mqtt.Client, device *model.Device) {
	est := s.battery.Estimate(device.ID)
	source := deviceSourceFromConfig(device.DeviceConfig)
	state := map[string]interface{}{
		"name":   device.Name,
		"source": source,
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
	// Hardware-reported charge status is independent of the regression estimate,
	// so publish it whenever the frame has reported one.
	if device.BatteryStatus != "" {
		state["battery_status"] = device.BatteryStatus
	}
	// Frame poll / schedule config (from the synced device_config) + network id.
	pc := parsePollConfig(device.DeviceConfig)
	if pc.rotateInterval > 0 {
		state["refresh_interval"] = round1(float64(pc.rotateInterval) / 60.0)
		// "Next image pull" is derived purely from the OBSERVED cadence: the frame
		// checks in every rotate_interval, so last-check-in + interval (rolled
		// forward past now, sleep-schedule adjusted) tracks reality. We deliberately
		// do NOT use the frame's self-reported X-Next-Wake-Time: the firmware builds
		// that as a request header *before* the per-cycle config sync is applied, so a
		// frame on stale/default config reports a bogus time (e.g. top-of-the-hour
		// from the 3600s firmware default while it actually wakes every 15 min). The
		// cadence estimate is reset on every check-in and needs no frame cooperation.
		// Only published when auto-rotate is on.
		if pc.autoRotate && est.HasData && !est.LastSampledAt.IsZero() {
			if nextPull := computeNextPull(est.LastSampledAt, pc); !nextPull.IsZero() {
				state["next_pull"] = nextPull.UTC().Format(time.RFC3339)
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
	// "Skip Queue" is a momentary control: always reported as 0 so the HA number
	// snaps back after a jump is applied (the jump amount arrives via the command).
	state["skip"] = 0
	if host := serverHostFromURL(pc.imageURL); host != "" {
		state["server_host"] = host
	}
	if t := triggerLabel(device.LastTrigger); t != "" {
		state["trigger"] = t
	}
	// Deep-sleep + auto-rotate on/off (switches); state mirrors the config so the
	// HA control reflects the frame's current setting.
	if pc.deepSleep {
		state["deep_sleep"] = "ON"
	} else {
		state["deep_sleep"] = "OFF"
	}
	if pc.autoRotate {
		state["auto_rotate"] = "ON"
	} else {
		state["auto_rotate"] = "OFF"
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

	// Rotation position / size: how many images this frame cycles through and
	// where the current one sits. For shuffle the "remaining" is what's left in
	// the current cycle (counts down to a fresh shuffle); for chronological/custom
	// the "position" is the current image number. Only ordered DB-backed sources
	// (collage off) have a meaningful rotation; the report self-suppresses otherwise.
	rs := ComputeRotationStatus(s.db, device, source, 0)
	if rs.Ordered && rs.Total > 0 {
		state["rotation_total"] = rs.Total
		if rs.Position > 0 {
			state["rotation_position"] = rs.Position
			state["rotation_remaining"] = rs.Remaining
			state["rotation_status"] = rotationStatusString(rs)
			// Estimate when the current cycle completes (a fresh shuffle / wrap):
			// "images left after the current one" more pulls at the rotate interval.
			if pc.autoRotate && pc.rotateInterval > 0 && est.HasData && !est.LastSampledAt.IsZero() {
				completes := est.LastSampledAt.Add(time.Duration(rs.Remaining*pc.rotateInterval) * time.Second)
				state["rotation_completes"] = completes.UTC().Format(time.RFC3339)
			}
		}
	}
	// Capture date of the photo currently on the frame.
	if t := s.currentPhotoTakenAt(device.ID, source); t != nil {
		state["current_photo_date"] = t.UTC().Format(time.RFC3339)
	}
	// Names of the Immich albums this frame pulls from (per-device album filter).
	if names := s.immichAlbumNames(device); names != "" {
		state["immich_albums"] = names
	}
	// Per-device online/offline: heard from the frame within ~2 rotation cycles?
	// Refreshed by the periodic republish (below) so it flips to offline even with
	// no check-in.
	online := false
	if est.HasData && !est.LastSampledAt.IsZero() {
		threshold := 90 * time.Minute
		if pc.rotateInterval > 0 {
			if t := 2*time.Duration(pc.rotateInterval)*time.Second + 15*time.Minute; t > threshold {
				threshold = t
			}
		}
		online = time.Since(est.LastSampledAt) < threshold
	}
	state["online"] = onOff(online)
	// Whether the photo currently on the frame is starred, and the two
	// rotation-pool toggles (exposed as HA switches).
	state["current_favorite"] = onOff(s.currentImageFavorite(device.ID, source))
	state["on_this_day"] = onOff(device.OnThisDay)
	state["favorites_only"] = onOff(device.FavoritesOnly)
	state["auto_update"] = onOff(device.AutoUpdate)

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

// DeviceOnline reports whether a frame is currently considered online: it last
// checked in (lastSeenAt) within ~2 rotation cycles (min 90 min). Mirrors the
// HA per-device online sensor so the Devices list and Home Assistant agree.
// A frame that hasn't been heard from within that window is stuck, offline, or
// (legitimately) asleep — detected purely from absence, so it works even when
// the frame is hung and can't report anything itself.
func DeviceOnline(lastSeenAt *time.Time, deviceConfig string) bool {
	if lastSeenAt == nil || lastSeenAt.IsZero() {
		return false
	}
	pc := parsePollConfig(deviceConfig)
	threshold := 90 * time.Minute
	if pc.rotateInterval > 0 {
		if t := 2*time.Duration(pc.rotateInterval)*time.Second + 15*time.Minute; t > threshold {
			threshold = t
		}
	}
	return time.Since(*lastSeenAt) < threshold
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

// rotationStatusString is the human "Rotation Status" sensor value: images left
// in the cycle for shuffle, the current image number for chronological/custom.
func rotationStatusString(rs RotationStatus) string {
	if rs.Position <= 0 {
		return ""
	}
	if rs.Mode == model.DisplayOrderShuffle {
		return fmt.Sprintf("%d of %d left", rs.Remaining, rs.Total)
	}
	return fmt.Sprintf("Image %d of %d", rs.Position, rs.Total)
}

// onOff maps a bool to the "ON"/"OFF" payload HA switches/binary_sensors expect.
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// currentImageFavorite reports whether the photo most recently served to the
// device from the given source is starred.
func (s *MQTTService) currentImageFavorite(deviceID uint, source string) bool {
	id := lastServedImageID(s.db, deviceID, source)
	if id == 0 {
		return false
	}
	var img model.Image
	if err := s.db.Select("favorite").First(&img, id).Error; err != nil {
		return false
	}
	return img.Favorite
}

// currentPhotoTakenAt returns the capture date of the photo most recently served
// to the device from the given source, or nil if unknown.
func (s *MQTTService) currentPhotoTakenAt(deviceID uint, source string) *time.Time {
	id := lastServedImageID(s.db, deviceID, source)
	if id == 0 {
		return nil
	}
	var img model.Image
	if err := s.db.Select("photo_taken_at").First(&img, id).Error; err != nil {
		return nil
	}
	return img.PhotoTakenAt
}

// immichAlbumNames resolves a frame's selected Immich album UUIDs to a
// comma-separated name list (cached in immich_albums; falls back to the raw UUID
// for any not yet cached). Empty when the frame has no album filter.
func (s *MQTTService) immichAlbumNames(device *model.Device) string {
	ids := model.ParseImmichAlbumIDs(device.ImmichAlbumIDs)
	if len(ids) == 0 {
		return ""
	}
	var albums []model.ImmichAlbum
	s.db.Where("immich_album_id IN ?", ids).Find(&albums)
	nameByID := make(map[string]string, len(albums))
	for _, a := range albums {
		nameByID[a.ImmichAlbumID] = a.AlbumName
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := nameByID[id]; n != "" {
			out = append(out, n)
		} else {
			out = append(out, id)
		}
	}
	return strings.Join(out, ", ")
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

// computeNextPull predicts when the frame next wakes to fetch an image from the
// OBSERVED cadence: the frame checks in every rotate_interval, so the next pull
// is the last check-in plus one interval (the clock-aligned phase, if any, is
// already baked into lastSeen — anchoring on it tracks reality better than
// re-deriving a grid the frame may not actually use). Missed wakes are rolled
// forward so the result is always in the future. When a quiet-hours sleep
// schedule is configured and the candidate falls inside it, the wake is pushed
// to the local end-of-window, mirroring the firmware's calculate_next_wakeup_interval.
func computeNextPull(lastSeen time.Time, pc pollConfig) time.Time {
	return computeNextPullAt(lastSeen, time.Now(), pc)
}

// computeNextPullAt is computeNextPull with an injectable "now" for testing.
func computeNextPullAt(lastSeen, now time.Time, pc pollConfig) time.Time {
	iv := pc.rotateInterval
	if iv <= 0 || lastSeen.IsZero() {
		return time.Time{}
	}
	step := time.Duration(iv) * time.Second
	candidate := lastSeen.Add(step)
	for candidate.Before(now) {
		candidate = candidate.Add(step)
	}
	if !pc.sleepEnabled {
		return candidate
	}
	offset, ok := parsePosixOffsetSeconds(pc.timezone)
	if !ok {
		// Can't place the candidate on the frame's local clock, so we can't tell
		// whether it lands in the quiet window — return the interval estimate.
		return candidate
	}
	startSec := normMinutes(pc.sleepStart) * 60
	endSec := normMinutes(pc.sleepEnd) * 60
	if startSec == endSec {
		return candidate // zero-length / disabled window
	}
	loc := time.FixedZone("frame", offset)
	lt := candidate.In(loc)
	sod := lt.Hour()*3600 + lt.Minute()*60 + lt.Second()
	if !inSleepWindow(sod, startSec, endSec) {
		return candidate
	}
	// In the quiet window: resume at the local end-of-window.
	delta := endSec - sod
	if delta <= 0 {
		delta += 86400
	}
	return candidate.Add(time.Duration(delta) * time.Second)
}

// inSleepWindow reports whether second-of-day sod lies in [start, end), handling
// windows that wrap past local midnight (start > end, e.g. 22:00–07:00).
func inSleepWindow(sod, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return sod >= start && sod < end
	}
	return sod >= start || sod < end
}

// normMinutes clamps a minutes-since-midnight value into [0,1440).
func normMinutes(m int) int {
	return ((m % 1440) + 1440) % 1440
}

// parsePosixOffsetSeconds extracts the standard-time UTC offset (seconds EAST of
// UTC, for time.FixedZone) from a POSIX TZ string like "UTC-2", "CET-1CEST,...",
// "UTC0" or "<+05>-5". POSIX offsets are inverted (they are added to local time
// to reach UTC), so we negate. DST transitions are not modelled — the standard
// offset is used year-round, which is exact for fixed zones (e.g. "UTC-2") and
// only off by the DST hour during summer for zones that switch. Returns ok=false
// when no numeric offset is present (e.g. a bare "UTC").
func parsePosixOffsetSeconds(tz string) (int, bool) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return 0, false
	}
	i := 0
	if tz[0] == '<' {
		j := strings.IndexByte(tz, '>')
		if j < 0 {
			return 0, false
		}
		i = j + 1
	} else {
		for i < len(tz) && ((tz[i] >= 'A' && tz[i] <= 'Z') || (tz[i] >= 'a' && tz[i] <= 'z')) {
			i++
		}
	}
	rest := tz[i:]
	if rest == "" {
		return 0, false
	}
	sign := 1
	switch rest[0] {
	case '+':
		rest = rest[1:]
	case '-':
		sign = -1
		rest = rest[1:]
	}
	end := 0
	for end < len(rest) && ((rest[end] >= '0' && rest[end] <= '9') || rest[end] == ':') {
		end++
	}
	numStr := rest[:end]
	if numStr == "" {
		return 0, false
	}
	parts := strings.Split(numStr, ":")
	hh, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	mm, ss := 0, 0
	if len(parts) > 1 {
		mm, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		ss, _ = strconv.Atoi(parts[2])
	}
	posix := sign * (hh*3600 + mm*60 + ss)
	return -posix, true // seconds east of UTC
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

// mqttSensorKeys are the value_json keys exposed as plain (read-only) HA sensors.
// Image source / refresh interval / image order are now controllable select /
// number entities (see publishDiscovery), so they are not in this list.
var mqttSensorKeys = []string{"battery", "battery_voltage", "days_remaining", "trend", "battery_status", "last_seen", "sleep_schedule", "next_pull", "host", "ip_address", "next_image_status", "timezone", "rotation", "server_host", "trigger", "rotation_total", "rotation_position", "rotation_remaining", "rotation_status", "rotation_completes", "current_photo_date", "immich_albums"}

// commandSourceOptions are the image sources offered by the HA "Image Source"
// select. Matches the server's registered sources (model source constants).
var commandSourceOptions = []string{
	model.SourceGallery, model.SourceImmich, model.SourceSynologyPhotos,
	model.SourceGooglePhotos, model.SourcePublicArt, model.SourceAIGeneration,
	model.SourceURLProxy, model.SourceFractal, model.SourceDLA,
}

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
	// Hardware-reported charge status (charging | full | on_battery), distinct
	// from the voltage-regression "trend": only frames that can sense USB report
	// it, so the value is omitted (and the sensor stays "unknown") on boards that
	// can't — see X-Battery-Status / board_hal_supports_charge_status().
	sensor("battery_status", "Battery Status", "battery_status", map[string]interface{}{
		"icon": "mdi:battery-charging", "entity_category": "diagnostic",
	})
	sensor("last_seen", "Last Seen", "last_seen", map[string]interface{}{"device_class": "timestamp"})

	// (Image Source / Refresh Interval are now controllable select / number
	// entities — see the controls section below.)
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
	sensor("server_host", "Server Host", "server_host", map[string]interface{}{
		"icon": "mdi:server-network", "entity_category": "diagnostic",
	})
	sensor("trigger", "Last Trigger", "trigger", map[string]interface{}{
		"icon": "mdi:gesture-tap-button",
	})

	// Rotation position / size. rotation_status is the human one-liner; the
	// numeric ones are for templates/automations. current_photo_date + the
	// cycle-completion estimate round out the "where in the album am I" picture.
	sensor("rotation_total", "Rotation Size", "rotation_total", map[string]interface{}{
		"icon": "mdi:image-multiple", "unit_of_measurement": "images", "state_class": "measurement",
	})
	sensor("rotation_status", "Rotation Status", "rotation_status", map[string]interface{}{
		"icon": "mdi:playlist-play",
	})
	sensor("rotation_position", "Rotation Position", "rotation_position", map[string]interface{}{
		"icon": "mdi:format-list-numbered", "entity_category": "diagnostic",
	})
	sensor("rotation_remaining", "Rotation Remaining", "rotation_remaining", map[string]interface{}{
		"icon": "mdi:counter", "entity_category": "diagnostic",
	})
	sensor("rotation_completes", "Rotation Completes", "rotation_completes", map[string]interface{}{
		"device_class": "timestamp", "icon": "mdi:restart",
	})
	sensor("current_photo_date", "Current Photo Date", "current_photo_date", map[string]interface{}{
		"device_class": "timestamp", "icon": "mdi:calendar-image",
	})
	sensor("immich_albums", "Immich Albums", "immich_albums", map[string]interface{}{
		"icon": "mdi:image-album", "entity_category": "diagnostic",
	})

	// --- Controllable entities (HA writes to a command topic; the server applies
	// the change + pushes/config-syncs to the frame, then republishes state). These
	// supersede the former read-only Image Source / Refresh Interval / Image Order /
	// Deep Sleep entities; clear those stale discovery configs so HA doesn't keep a
	// duplicate read-only copy alongside the new control.
	client.Publish(s.discoveryTopic("sensor", device.ID, "source"), 1, true, "")
	client.Publish(s.discoveryTopic("sensor", device.ID, "refresh_interval"), 1, true, "")
	client.Publish(s.discoveryTopic("sensor", device.ID, "display_order"), 1, true, "")
	client.Publish(s.discoveryTopic("binary_sensor", device.ID, "deep_sleep"), 1, true, "")

	control := func(component, key string, cfg map[string]interface{}) {
		cfg["unique_id"] = fmt.Sprintf("photoframe_%d_%s", device.ID, key)
		cfg["object_id"] = fmt.Sprintf("photoframe_%s_%s", sanitize(device.Name), key)
		cfg["command_topic"] = s.commandTopic(device.ID, key)
		cfg["availability"] = avail
		cfg["device"] = dev
		payload, _ := json.Marshal(cfg)
		client.Publish(s.discoveryTopic(component, device.ID, key), 1, true, payload)
	}

	control("select", "source", map[string]interface{}{
		"name": "Image Source", "icon": "mdi:image-multiple",
		"state_topic": state, "value_template": "{{ value_json.source }}",
		"options": commandSourceOptions,
	})
	control("select", "image_order", map[string]interface{}{
		"name": "Image Order", "icon": "mdi:sort",
		"state_topic": state, "value_template": "{{ value_json.display_order }}",
		"options": []string{"Shuffle", "Newest first", "Oldest first", "Custom"},
	})
	control("number", "refresh_interval", map[string]interface{}{
		"name": "Refresh Interval", "icon": "mdi:timer-sync-outline",
		"state_topic": state, "value_template": "{{ value_json.refresh_interval }}",
		"min": 1, "max": 1440, "step": 1, "unit_of_measurement": "min", "mode": "box",
		"entity_category": "config",
	})
	control("switch", "deep_sleep", map[string]interface{}{
		"name": "Deep Sleep", "icon": "mdi:power-sleep",
		"state_topic": state, "value_template": "{{ value_json.deep_sleep }}",
		"payload_on": "ON", "payload_off": "OFF", "entity_category": "config",
	})
	control("switch", "auto_rotate", map[string]interface{}{
		"name": "Auto Rotate", "icon": "mdi:rotate-right",
		"state_topic": state, "value_template": "{{ value_json.auto_rotate }}",
		"payload_on": "ON", "payload_off": "OFF", "entity_category": "config",
	})
	// "Rotate Now" button — gated Unavailable on boards that can't act on a live
	// command (deep-sleeping FireBeetle), available on always-reachable boards.
	rotateAvail := append([]map[string]string{}, avail...)
	rotateAvail = append(rotateAvail, map[string]string{
		"topic":                 s.controlsAvailabilityTopic(device.ID),
		"payload_available":     "online",
		"payload_not_available": "offline",
	})
	rotateCfg := map[string]interface{}{
		"name":              "Rotate Now",
		"unique_id":         fmt.Sprintf("photoframe_%d_rotate", device.ID),
		"object_id":         fmt.Sprintf("photoframe_%s_rotate", sanitize(device.Name)),
		"command_topic":     s.commandTopic(device.ID, "rotate"),
		"payload_press":     "PRESS",
		"icon":              "mdi:rotate-3d-variant",
		"availability":      rotateAvail,
		"availability_mode": "all",
		"device":            dev,
	}
	rotatePayload, _ := json.Marshal(rotateCfg)
	client.Publish(s.discoveryTopic("button", device.ID, "rotate"), 1, true, rotatePayload)

	// "Reshuffle" button — bumps the shuffle seed so the next pull starts a fresh
	// shuffle order. Server-side only (no frame contact needed), so it's always
	// available regardless of board. Only meaningful in shuffle mode; harmless
	// otherwise.
	reshuffleCfg := map[string]interface{}{
		"name":          "Reshuffle",
		"unique_id":     fmt.Sprintf("photoframe_%d_reshuffle", device.ID),
		"object_id":     fmt.Sprintf("photoframe_%s_reshuffle", sanitize(device.Name)),
		"command_topic": s.commandTopic(device.ID, "reshuffle"),
		"payload_press": "PRESS",
		"icon":          "mdi:shuffle-variant",
		"availability":  avail,
		"device":        dev,
	}
	reshufflePayload, _ := json.Marshal(reshuffleCfg)
	client.Publish(s.discoveryTopic("button", device.ID, "reshuffle"), 1, true, reshufflePayload)

	// "Skip Queue" — jump N steps in the rotation (positive forward, negative back).
	// Set the number and the next ordered pull jumps to that image, then the control
	// snaps back to 0. Server-side, always available; a no-op for collage / non-
	// ordered sources.
	control("number", "skip", map[string]interface{}{
		"name": "Skip Queue", "icon": "mdi:debug-step-over",
		"state_topic": state, "value_template": "{{ value_json.skip }}",
		"min": -500, "max": 500, "step": 1, "mode": "box",
	})

	// "Hide Current Photo" + "Toggle Favorite" buttons — act on the photo the frame
	// is showing now. Server-side, always available.
	button := func(key, name, icon string) {
		cfg := map[string]interface{}{
			"name":          name,
			"unique_id":     fmt.Sprintf("photoframe_%d_%s", device.ID, key),
			"object_id":     fmt.Sprintf("photoframe_%s_%s", sanitize(device.Name), key),
			"command_topic": s.commandTopic(device.ID, key),
			"payload_press": "PRESS",
			"icon":          icon,
			"availability":  avail,
			"device":        dev,
		}
		payload, _ := json.Marshal(cfg)
		client.Publish(s.discoveryTopic("button", device.ID, key), 1, true, payload)
	}
	button("hide", "Hide Current Photo", "mdi:eye-off")
	button("favorite", "Toggle Favorite", "mdi:star")

	// On This Day / Favorites Only rotation-pool switches.
	control("switch", "on_this_day", map[string]interface{}{
		"name": "On This Day", "icon": "mdi:calendar-star",
		"state_topic": state, "value_template": "{{ value_json.on_this_day }}",
		"payload_on": "ON", "payload_off": "OFF", "entity_category": "config",
	})
	control("switch", "favorites_only", map[string]interface{}{
		"name": "Favorites Only", "icon": "mdi:star-circle",
		"state_topic": state, "value_template": "{{ value_json.favorites_only }}",
		"payload_on": "ON", "payload_off": "OFF", "entity_category": "config",
	})

	// Server-controlled OTA auto-update: when on, the frame's daily check
	// self-installs a found update (battery-gated on-device). Default off.
	control("switch", "auto_update", map[string]interface{}{
		"name": "Auto-Update Firmware", "icon": "mdi:cloud-download",
		"state_topic": state, "value_template": "{{ value_json.auto_update }}",
		"payload_on": "ON", "payload_off": "OFF", "entity_category": "config",
	})

	// Per-device online/offline + current-photo-favorite binary sensors.
	binarySensor := func(key, name, valueKey, devClass, icon string) {
		cfg := map[string]interface{}{
			"name":           name,
			"unique_id":      fmt.Sprintf("photoframe_%d_%s", device.ID, key),
			"object_id":      fmt.Sprintf("photoframe_%s_%s", sanitize(device.Name), key),
			"state_topic":    state,
			"value_template": fmt.Sprintf("{{ value_json.%s }}", valueKey),
			"payload_on":     "ON",
			"payload_off":    "OFF",
			"availability":   avail,
			"device":         dev,
		}
		if devClass != "" {
			cfg["device_class"] = devClass
		}
		if icon != "" {
			cfg["icon"] = icon
		}
		payload, _ := json.Marshal(cfg)
		client.Publish(s.discoveryTopic("binary_sensor", device.ID, key), 1, true, payload)
	}
	binarySensor("online", "Online", "online", "connectivity", "")
	binarySensor("current_favorite", "Current Photo Favorite", "current_favorite", "", "mdi:star")

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
