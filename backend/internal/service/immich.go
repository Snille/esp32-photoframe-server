package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/immich"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// formatImmichLocation joins the EXIF place fields into "City, State, Country",
// skipping empty parts. Returns "" when no location data is present.
func formatImmichLocation(exif immich.ExifInfo) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{exif.City, exif.State, exif.Country} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// encodePeopleJSON serializes the named faces of an asset to the JSON blob
// stored in Image.PeopleJSON. Unnamed faces are dropped (nothing to show).
func encodePeopleJSON(people []immich.Person) string {
	out := make([]Person, 0, len(people))
	for _, p := range people {
		if strings.TrimSpace(p.Name) == "" {
			continue
		}
		out = append(out, Person{Name: p.Name, BirthDate: p.BirthDate})
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// defaultImmichServerID is the seeded server-1 row (migration 000060), which
// mirrors the legacy single-server immich_url / immich_api_key settings. Global
// sync modes (album/all/favorites/memories) apply to this server; additional
// servers contribute only through per-device selected (shared) albums.
const defaultImmichServerID uint = 1

type ImmichService struct {
	db       *gorm.DB
	settings *SettingsService
	clients  map[uint]*immich.Client // keyed by immich_servers.id
	mu       sync.Mutex
	autoSync *AutoSyncScheduler
}

func NewImmichService(db *gorm.DB, settings *SettingsService) *ImmichService {
	svc := &ImmichService{db: db, settings: settings, clients: map[uint]*immich.Client{}}
	svc.autoSync = NewAutoSyncScheduler(AutoSyncSchedulerOptions{
		Name:     "Immich",
		Settings: settings,
		IsRelevantKey: func(key string) bool {
			switch key {
			case "immich_auto_sync_enabled", "immich_auto_sync_interval_minutes",
				"immich_source_mode", "immich_album_id", "immich_url", "immich_api_key",
				"immich_servers_rev":
				return true
			default:
				return false
			}
		},
		IsConfigured: svc.isAutoSyncConfigured,
		GetConfig:    svc.getAutoSyncConfig,
		// Incremental upsert + prune, NOT clear-and-reinsert: a periodic clear
		// hard-deletes every Immich row and re-imports with fresh auto-increment
		// IDs, orphaning DeviceHistory and silently restarting every frame's
		// rotation cursor on each sync. ImportPhotos keeps stable IDs.
		RunSync: svc.ImportPhotos,
	})
	return svc
}

// StartAutoSync starts a background loop that periodically syncs Immich photos
// when the corresponding settings are enabled.
func (s *ImmichService) StartAutoSync() {
	s.autoSync.Start()
}

// Immich source modes — what the GLOBAL sync pulls from. See issue #32.
// Per-device album selection is layered on top of all of these and is always
// synced regardless of the global mode.
const (
	ImmichModeAlbum        = "album"         // photos from one configured album (default)
	ImmichModeAll          = "all"           // entire library
	ImmichModeFavorites    = "favorites"     // only assets marked as Favorite
	ImmichModeMemories     = "memories"      // on-this-day across years
	ImmichModeDeviceAlbums = "device_albums" // nothing global — only each frame's selected albums
)

// errImmichNoAlbum is returned by fetchAssetsForMode when album mode is active
// but no global album is configured. ImportPhotos tolerates it when frames have
// their own per-device albums selected.
var errImmichNoAlbum = errors.New("please select an album to sync")

// immichSourceMode returns the configured sync mode, defaulting to album.
func (s *ImmichService) immichSourceMode() string {
	mode, _ := s.settings.Get("immich_source_mode")
	switch mode {
	case ImmichModeAll, ImmichModeFavorites, ImmichModeMemories, ImmichModeDeviceAlbums:
		return mode
	default:
		return ImmichModeAlbum
	}
}

func (s *ImmichService) isAutoSyncConfigured() bool {
	if len(s.listServers()) == 0 {
		return false
	}
	// Album mode is the only one that needs a (global) album picked; other modes
	// and any per-device selected albums are handled during the sync.
	if s.immichSourceMode() == ImmichModeAlbum {
		albumID, _ := s.settings.Get("immich_album_id")
		return albumID != "" || len(s.collectDeviceAlbumIDs()) > 0
	}
	return true
}

func (s *ImmichService) getAutoSyncConfig() (bool, time.Duration) {
	enabledStr, _ := s.settings.Get("immich_auto_sync_enabled")
	enabled := strings.EqualFold(enabledStr, "true")

	minutes := 60
	if intervalStr, err := s.settings.Get("immich_auto_sync_interval_minutes"); err == nil {
		if parsed, parseErr := strconv.Atoi(intervalStr); parseErr == nil && parsed > 0 {
			minutes = parsed
		}
	}

	return enabled, time.Duration(minutes) * time.Minute
}

// serverConfig returns the effective config for a server id. The DEFAULT server
// (id 1) keeps taking its url/key from the legacy immich_url/immich_api_key
// settings, so the existing single-server settings UI drives it unchanged and
// never diverges from its seeded row; additional servers (2+) are driven by
// their immich_servers rows.
func (s *ImmichService) serverConfig(serverID uint) (model.ImmichServer, error) {
	var srv model.ImmichServer
	rowErr := s.db.First(&srv, serverID).Error

	if serverID == defaultImmichServerID {
		baseURL, _ := s.settings.Get("immich_url")
		apiKey, _ := s.settings.Get("immich_api_key")
		srv.ID = defaultImmichServerID
		srv.URL = baseURL
		srv.APIKey = apiKey
		if srv.Label == "" {
			srv.Label = "Default"
		}
		if rowErr != nil {
			srv.Enabled = true // no row yet (pre-migration test) → treat as enabled
		}
		return srv, nil
	}

	if rowErr != nil {
		return model.ImmichServer{}, fmt.Errorf("immich server %d not found", serverID)
	}
	return srv, nil
}

// listServers returns the enabled Immich servers with effective config (server 1
// resolved from settings). A configured-but-empty server is dropped.
func (s *ImmichService) listServers() []model.ImmichServer {
	var rows []model.ImmichServer
	s.db.Where("enabled = 1").Order("id").Find(&rows)
	// Ensure the default server is considered even if its row is missing.
	haveDefault := false
	for _, r := range rows {
		if r.ID == defaultImmichServerID {
			haveDefault = true
		}
	}
	if !haveDefault {
		rows = append([]model.ImmichServer{{ID: defaultImmichServerID, Enabled: true}}, rows...)
	}
	out := make([]model.ImmichServer, 0, len(rows))
	for _, r := range rows {
		cfg, err := s.serverConfig(r.ID)
		if err != nil || cfg.URL == "" || cfg.APIKey == "" {
			continue
		}
		out = append(out, cfg)
	}
	return out
}

// getClient returns a cached client for the given server, rebuilding it if the
// server's url/key changed. Safe for concurrent use.
func (s *ImmichService) getClient(serverID uint) (*immich.Client, error) {
	if serverID == 0 {
		serverID = defaultImmichServerID // legacy rows with server 0 → the default server
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	srv, err := s.serverConfig(serverID)
	if err != nil {
		return nil, err
	}
	if srv.URL == "" || srv.APIKey == "" {
		return nil, errors.New("immich credentials not configured")
	}
	c := s.clients[serverID]
	if c == nil || c.BaseURL != srv.URL || c.APIKey != srv.APIKey {
		c = immich.NewClient(srv.URL, srv.APIKey)
		s.clients[serverID] = c
	}
	return c, nil
}

// TestConnection verifies connectivity + key for one configured server, forcing
// a fresh client so an edited url/key is picked up immediately.
func (s *ImmichService) TestConnection(serverID uint) error {
	s.mu.Lock()
	delete(s.clients, serverID)
	s.mu.Unlock()
	client, err := s.getClient(serverID)
	if err != nil {
		return err
	}
	return client.TestConnection()
}

// TestCredentials verifies an arbitrary url + api key without persisting a
// server, for the settings form's "Test connection" button before saving.
func (s *ImmichService) TestCredentials(url, apiKey string) error {
	if strings.TrimSpace(url) == "" || strings.TrimSpace(apiKey) == "" {
		return errors.New("url and api key are required")
	}
	return immich.NewClient(url, apiKey).TestConnection()
}

// AllServers returns every configured Immich server (enabled or not) for the
// settings UI.
func (s *ImmichService) AllServers() ([]model.ImmichServer, error) {
	var servers []model.ImmichServer
	if err := s.db.Order("id").Find(&servers).Error; err != nil {
		return nil, err
	}
	return servers, nil
}

// serversChanged drops cached clients and bumps a watched settings key so the
// auto-sync scheduler reconfigures + re-syncs after a server add/edit/remove.
func (s *ImmichService) serversChanged() {
	s.mu.Lock()
	s.clients = map[uint]*immich.Client{}
	s.mu.Unlock()
	_ = s.settings.Set("immich_servers_rev", strconv.FormatInt(time.Now().UnixNano(), 10))
}

// CreateServer adds a new Immich server.
func (s *ImmichService) CreateServer(label, url, apiKey string, enabled bool) (*model.ImmichServer, error) {
	srv := &model.ImmichServer{Label: label, URL: strings.TrimRight(strings.TrimSpace(url), "/"), APIKey: strings.TrimSpace(apiKey), Enabled: enabled, CreatedAt: time.Now()}
	if err := s.db.Create(srv).Error; err != nil {
		return nil, err
	}
	// GORM omits a zero-value (false) bool on insert and applies the column
	// default (1), so a server created disabled would come back enabled. Force it.
	if !enabled {
		s.db.Model(srv).Update("enabled", false)
		srv.Enabled = false
	}
	s.serversChanged()
	return srv, nil
}

// UpdateServer edits an existing Immich server.
func (s *ImmichService) UpdateServer(id uint, label, url, apiKey string, enabled bool) error {
	updates := map[string]interface{}{
		"label":   label,
		"url":     strings.TrimRight(strings.TrimSpace(url), "/"),
		"enabled": enabled,
	}
	// Empty api key on edit = keep the existing one (the UI doesn't echo secrets).
	if strings.TrimSpace(apiKey) != "" {
		updates["api_key"] = strings.TrimSpace(apiKey)
	}
	if err := s.db.Model(&model.ImmichServer{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	s.serversChanged()
	return nil
}

// DeleteServer removes a server and hard-deletes its synced photos + album rows
// (they can't be served without the server's key anyway).
func (s *ImmichService) DeleteServer(id uint) error {
	if id == defaultImmichServerID {
		return errors.New("the default Immich server cannot be removed; disable it instead")
	}
	s.db.Unscoped().Where("source = ? AND immich_server_id = ?", model.SourceImmich, id).Delete(&model.Image{})
	s.db.Where("immich_server_id = ?", id).Delete(&model.ImmichImageAlbum{})
	s.db.Where("immich_server_id = ?", id).Delete(&model.ImmichAlbum{})
	if err := s.db.Delete(&model.ImmichServer{}, id).Error; err != nil {
		return err
	}
	s.serversChanged()
	return nil
}

// AlbumInfo is one album offered by the per-device picker, tagged with the
// server it lives on so the UI can group / disambiguate by server (album names
// can collide across servers). Field names match the raw immich.Album the picker
// already consumed (albumName / assetCount) plus the server tags.
type AlbumInfo struct {
	ID          string `json:"id"`
	AlbumName   string `json:"albumName"`
	AssetCount  int    `json:"assetCount"`
	ServerID    uint   `json:"serverId"`
	ServerLabel string `json:"serverLabel"`
}

// ListAlbums returns one server's accessible albums (owned + shared, per the
// client), sorted by name, and refreshes the server-scoped album-name cache.
func (s *ImmichService) ListAlbums(serverID uint) ([]immich.Album, error) {
	client, err := s.getClient(serverID)
	if err != nil {
		return nil, err
	}
	albums, err := client.ListAlbums()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(albums, func(i, j int) bool {
		return strings.ToLower(albums[i].AlbumName) < strings.ToLower(albums[j].AlbumName)
	})
	for _, a := range albums {
		s.cacheAlbumName(serverID, a.ID, a.AlbumName)
	}
	return albums, nil
}

// ListAllAlbums aggregates the albums exposed by every enabled server, tagged
// with the server id + label, for the per-device album picker. A failing server
// is skipped (logged) rather than failing the whole picker.
func (s *ImmichService) ListAllAlbums() ([]AlbumInfo, error) {
	servers := s.listServers()
	out := []AlbumInfo{}
	for _, srv := range servers {
		albums, err := s.ListAlbums(srv.ID)
		if err != nil {
			log.Printf("Immich: album list failed for server %d (%s): %v", srv.ID, srv.Label, err)
			continue
		}
		label := srv.Label
		if label == "" {
			label = fmt.Sprintf("Server %d", srv.ID)
		}
		for _, a := range albums {
			out = append(out, AlbumInfo{ID: a.ID, AlbumName: a.AlbumName, AssetCount: a.AssetCount, ServerID: srv.ID, ServerLabel: label})
		}
	}
	return out, nil
}

// AlbumUsage is one Immich album that currently has synced photos, for the
// Gallery's per-album filter.
type AlbumUsage struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ListUsedAlbums returns the distinct Immich albums that currently have at
// least one synced (non-deleted) photo, resolved to names via the cached
// album-name table. Unlike ListAlbums (every album in the whole Immich
// library, which can be dozens), this only surfaces albums actually relevant
// to what's been synced — so the Gallery's filter row isn't cluttered with
// albums no frame has ever selected.
func (s *ImmichService) ListUsedAlbums() ([]AlbumUsage, error) {
	type row struct {
		ImmichAlbumID string
		AlbumName     string
		Count         int
	}
	var rows []row
	err := s.db.Table("immich_image_albums iia").
		Select("iia.immich_album_id AS immich_album_id, ia.album_name AS album_name, COUNT(DISTINCT iia.image_id) AS count").
		Joins("JOIN immich_albums ia ON ia.immich_album_id = iia.immich_album_id").
		Joins("JOIN images img ON img.id = iia.image_id AND img.deleted_at IS NULL").
		Group("iia.immich_album_id, ia.album_name").
		Order("ia.album_name COLLATE NOCASE").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AlbumUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, AlbumUsage{ID: r.ImmichAlbumID, Name: r.AlbumName, Count: r.Count})
	}
	return out, nil
}

// ImportPhotos syncs the global pool (per the configured mode) AND every Immich
// album any frame has selected, recording album membership so a frame can be
// filtered to its chosen albums. The global modes are unchanged; per-device
// album selection is layered on top.
func (s *ImmichService) ImportPhotos() error {
	servers := s.listServers()
	if len(servers) == 0 {
		return errors.New("immich credentials not configured")
	}

	// Union of every frame's selected album UUIDs (globally unique per instance,
	// so a UUID belongs to exactly one server — matched below via each server's
	// own album list).
	deviceAlbums := s.collectDeviceAlbumIDs()

	globalNew, albumNew := 0, 0
	pruned := int64(0)

	for _, srv := range servers {
		client, err := s.getClient(srv.ID)
		if err != nil {
			log.Printf("Immich: skipping server %d (%s): %v", srv.ID, srv.Label, err)
			continue
		}
		serverID := srv.ID

		// Per-server prune set: only prune THIS server's assets so syncing one
		// server never wipes another's. Stable IDs (see upsertAsset) keep
		// DeviceHistory / rotation cursors intact.
		seen := make(map[string]struct{})
		fetchFailed := false

		// Which albums this server exposes (refreshes the server-scoped name cache
		// too). Used to route each device-selected album to its owning server.
		serverAlbumIDs := map[string]bool{}
		if albums, aerr := client.ListAlbums(); aerr == nil {
			for _, a := range albums {
				serverAlbumIDs[a.ID] = true
				s.cacheAlbumName(serverID, a.ID, a.AlbumName)
			}
		} else {
			log.Printf("Immich: album list failed for server %d: %v", serverID, aerr)
			fetchFailed = true
		}

		// 1) Global pool per the configured mode — default (settings) server only.
		//    Other servers contribute solely through per-device selected albums.
		if serverID == defaultImmichServerID {
			assets, e := s.fetchAssetsForMode(client)
			if e != nil && !(errors.Is(e, errImmichNoAlbum) && len(deviceAlbums) > 0) {
				log.Printf("Immich: global fetch failed for server %d: %v", serverID, e)
				fetchFailed = true
			}
			for _, asset := range assets {
				id, isNew, ue := s.upsertAsset(client, serverID, asset)
				if ue != nil || id == 0 {
					continue
				}
				seen[asset.ID] = struct{}{}
				if isNew {
					globalNew++
				}
			}
		}

		// 2) Per-device selected albums that live on THIS server.
		for _, albumID := range deviceAlbums {
			if len(serverAlbumIDs) > 0 && !serverAlbumIDs[albumID] {
				continue // belongs to a different server
			}
			albumAssets, e := client.GetAlbumAssets(albumID)
			if e != nil {
				if len(serverAlbumIDs) > 0 { // known to be this server's album → real error
					log.Printf("Immich: failed to fetch album %s on server %d: %v", albumID, serverID, e)
					fetchFailed = true
				}
				continue
			}
			// Rebuild this album's membership (for this server) so removals propagate.
			s.db.Where("immich_album_id = ? AND immich_server_id = ?", albumID, serverID).
				Delete(&model.ImmichImageAlbum{})
			for _, asset := range albumAssets {
				id, isNew, ue := s.upsertAsset(client, serverID, asset)
				if ue != nil || id == 0 {
					continue
				}
				seen[asset.ID] = struct{}{}
				if isNew {
					albumNew++
				}
				s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(
					&model.ImmichImageAlbum{ImageID: id, ImmichAlbumID: albumID, ImmichServerID: int(serverID)})
			}
		}

		// 3) Soft-delete this server's assets no longer in any synced pool. Guarded
		// against a partial/failed fetch so a transient Immich error can't wipe it.
		if !fetchFailed && len(seen) > 0 {
			keep := make([]string, 0, len(seen))
			for id := range seen {
				keep = append(keep, id)
			}
			res := s.db.Where("source = ? AND immich_server_id = ? AND immich_asset_id NOT IN ?",
				model.SourceImmich, serverID, keep).Delete(&model.Image{})
			if res.Error != nil {
				log.Printf("Immich prune failed for server %d: %v", serverID, res.Error)
			} else {
				pruned += res.RowsAffected
			}
		}
	}

	log.Printf("Immich ImportPhotos complete across %d server(s): %d new (global) + %d new (albums); %d pruned; %d album(s) selected across frames",
		len(servers), globalNew, albumNew, pruned, len(deviceAlbums))
	return nil
}

// cacheAlbumName upserts the server-scoped album-name cache used by the picker
// and the HA "Immich Albums" sensor.
func (s *ImmichService) cacheAlbumName(serverID uint, albumID, name string) {
	s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "immich_album_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"album_name", "immich_server_id"}),
	}).Create(&model.ImmichAlbum{ImmichAlbumID: albumID, AlbumName: name, ImmichServerID: int(serverID)})
}

// upsertAsset inserts a new Immich asset (returns its id, true) or returns the
// existing row's id (id, false). Non-image and RAW assets are skipped (0, false).
func (s *ImmichService) upsertAsset(client *immich.Client, serverID uint, asset immich.Asset) (uint, bool, error) {
	if asset.Type != "IMAGE" {
		return 0, false, nil
	}
	// Skip RAW files — these can't be served via Immich's preview/thumbnail API.
	switch strings.ToLower(filepath.Ext(asset.OriginalFileName)) {
	case ".dng", ".cr2", ".cr3", ".nef", ".arw", ".raf", ".orf", ".rw2":
		return 0, false, nil
	}

	w, h := asset.ExifInfo.ExifImageWidth, asset.ExifInfo.ExifImageHeight
	photoDate := parseImmichDate(asset.ExifInfo.DateTimeOriginal)
	if photoDate == nil {
		photoDate = parseImmichDate(asset.LocalDateTime)
	}
	location := formatImmichLocation(asset.ExifInfo)
	description := strings.TrimSpace(asset.ExifInfo.Description)

	var existing model.Image
	if s.db.Unscoped().Where("immich_asset_id = ? AND source = ? AND immich_server_id = ?", asset.ID, model.SourceImmich, serverID).
		First(&existing).Error == nil {
		// Revive a previously-pruned asset that reappeared, keeping its row ID so
		// any DeviceHistory referencing it stays valid (the cursor survives).
		if existing.DeletedAt.Valid {
			s.db.Unscoped().Model(&existing).Update("deleted_at", nil)
		}
		// Refresh metadata that can change in Immich after first import (e.g. a
		// description/comment added later, a relocated photo). These come free from
		// the album/search listing — no extra API call — so update them in place.
		// Without this the incremental sync (which replaced the old clear+reinsert)
		// would never propagate edits to existing photos. People faces are NOT in
		// the listing and are left untouched to avoid a per-asset fetch every sync.
		updates := map[string]any{}
		if existing.Description != description {
			updates["description"] = description
		}
		if existing.Location != location {
			updates["location"] = location
		}
		if !sameTimePtr(existing.PhotoTakenAt, photoDate) {
			updates["photo_taken_at"] = photoDate
		}
		if len(updates) > 0 {
			if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
				log.Printf("Immich: failed to refresh metadata for asset %s: %v", asset.ID, err)
			}
		}
		return existing.ID, false, nil
	}

	img := model.Image{
		ImmichAssetID:  asset.ID,
		ImmichServerID: int(serverID),
		Source:         model.SourceImmich,
		FilePath:       asset.OriginalFileName,
		Width:          w,
		Height:         h,
		Orientation:    determineOrientation(w, h, asset.ExifInfo.Orientation),
		CreatedAt:      time.Now(),
		Status:         "pending",
	}
	img.PhotoTakenAt = photoDate
	img.Location = location
	img.Description = description

	// Album/search listings include people directly since SearchAssets always
	// asks for withPeople. The /api/memories endpoint has no such flag, so for
	// that source (and as a safety net) fall back to the per-asset detail call,
	// which always includes people. Best-effort: a failure just leaves names
	// empty for this photo.
	if len(asset.People) > 0 {
		img.PeopleJSON = encodePeopleJSON(asset.People)
	} else if detail, derr := client.GetAsset(asset.ID); derr == nil {
		img.PeopleJSON = encodePeopleJSON(detail.People)
	} else {
		log.Printf("Immich: people fetch failed for asset %s: %v", asset.ID, derr)
	}

	if err := s.db.Create(&img).Error; err != nil {
		log.Printf("Failed to insert immich asset %s: %v", asset.ID, err)
		return 0, false, err
	}
	return img.ID, true, nil
}

// collectDeviceAlbumIDs returns the de-duplicated union of every frame's
// selected Immich album IDs.
func (s *ImmichService) collectDeviceAlbumIDs() []string {
	var rows []string
	s.db.Model(&model.Device{}).Pluck("immich_album_ids", &rows)
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		for _, id := range model.ParseImmichAlbumIDs(r) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// fetchAssetsForMode dispatches to the right Immich client method for the
// configured source mode. Returns the raw asset list — caller filters out
// videos / RAW / duplicates and persists into the local DB.
func (s *ImmichService) fetchAssetsForMode(client *immich.Client) ([]immich.Asset, error) {
	switch s.immichSourceMode() {
	case ImmichModeDeviceAlbums:
		// No global pool: ImportPhotos still syncs each frame's selected albums.
		return nil, nil
	case ImmichModeAlbum:
		albumID, _ := s.settings.Get("immich_album_id")
		if albumID == "" {
			return nil, errImmichNoAlbum
		}
		return client.GetAlbumAssets(albumID)
	case ImmichModeAll:
		return client.SearchAssets(immich.SearchMetadataRequest{})
	case ImmichModeFavorites:
		t := true
		return client.SearchAssets(immich.SearchMetadataRequest{IsFavorite: &t})
	case ImmichModeMemories:
		return client.GetMemoryAssets()
	default:
		return nil, fmt.Errorf("unknown immich source mode: %q", s.immichSourceMode())
	}
}

// ClearPhotos deletes all Immich photos from the database
func (s *ImmichService) ClearPhotos() error {
	if err := s.db.Unscoped().Where("source = ?", model.SourceImmich).Delete(&model.Image{}).Error; err != nil {
		return err
	}
	// Drop all album-membership rows too; they're rebuilt on the next sync.
	if err := s.db.Where("1 = 1").Delete(&model.ImmichImageAlbum{}).Error; err != nil {
		return err
	}
	log.Println("Cleared all Immich photos from database")
	return nil
}

// SyncNow runs the same non-destructive incremental sync the periodic auto-sync
// uses (upsert + soft-delete prune, stable IDs). This backs the manual "Sync Now"
// button: it pulls new/removed/edited assets without churning IDs or resetting any
// frame's rotation cursor. Goes through the scheduler so lastSuccessAt is updated
// and the next periodic run is rescheduled relative to this manual one.
func (s *ImmichService) SyncNow() error {
	return s.autoSync.SyncNow()
}

// ClearAndResync hard-deletes all Immich photos and re-imports from scratch. This
// is the explicit user-triggered "Rebuild Library" path (it intentionally resets
// rotation cursors and re-fetches people/faces for every asset); the periodic
// auto-sync and the "Sync Now" button use the non-destructive ImportPhotos.
func (s *ImmichService) ClearAndResync() error {
	if err := s.ClearPhotos(); err != nil {
		return err
	}
	return s.autoSync.SyncNow()
}

// sameTimePtr reports whether two *time.Time point at the same instant (both nil
// counts as equal), so a metadata refresh only writes when the date truly changed.
func sameTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// parseImmichDate parses ISO 8601 date strings from the Immich API.
func parseImmichDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return &t
		}
	}
	return nil
}

// GetPhotoCount returns the number of Immich photos in the database
func (s *ImmichService) GetPhotoCount() (int64, error) {
	var count int64
	if err := s.db.Model(&model.Image{}).Where("source = ?", model.SourceImmich).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// GetPhoto fetches the image bytes for an Immich asset by its UUID, from the
// server the asset belongs to. size is "thumbnail" (small, gallery) or "preview"
// (large, serving).
func (s *ImmichService) GetPhoto(serverID uint, assetID, size string) ([]byte, error) {
	client, err := s.getClient(serverID)
	if err != nil {
		return nil, err
	}
	return client.GetThumbnail(assetID, size)
}

// DownloadOriginal fetches the full-resolution original image for an asset from
// its owning server.
func (s *ImmichService) DownloadOriginal(serverID uint, assetID string) ([]byte, error) {
	client, err := s.getClient(serverID)
	if err != nil {
		return nil, err
	}
	return client.DownloadOriginal(assetID)
}

// DownloadPhoto downloads the original full-resolution image (from the asset's
// owning server) and converts it to JPEG using ImageMagick (handles HEIC, RAW
// formats and EXIF auto-orient). Falls back to Immich's preview API if original
// download or conversion fails.
func (s *ImmichService) DownloadPhoto(serverID uint, assetID string) ([]byte, error) {
	data, err := s.DownloadOriginal(serverID, assetID)
	if err != nil {
		log.Printf("Immich original download failed for asset %s: %v, falling back to preview", assetID, err)
		return s.downloadPreviewFallback(serverID, assetID, err)
	}

	tmpDir, err := os.MkdirTemp("", "immich-convert-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input")
	outputPath := filepath.Join(tmpDir, "output.jpg")

	if err := os.WriteFile(inputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Use ImageMagick to convert any format to JPEG with EXIF auto-orientation
	cmd := exec.Command("magick", inputPath, "-auto-orient", "-quality", "95", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("ImageMagick conversion failed for asset %s: %v (output: %s), falling back to preview", assetID, err, string(output))
		return s.downloadPreviewFallback(serverID, assetID, err)
	}

	return os.ReadFile(outputPath)
}

// downloadPreviewFallback tries the Immich preview API as a fallback when
// original download or conversion fails.
func (s *ImmichService) downloadPreviewFallback(serverID uint, assetID string, originalErr error) ([]byte, error) {
	previewData, previewErr := s.GetPhoto(serverID, assetID, "preview")
	if previewErr != nil {
		return nil, fmt.Errorf("original failed: %w; preview fallback also failed: %v", originalErr, previewErr)
	}
	return previewData, nil
}
