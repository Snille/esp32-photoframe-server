package service

// Shared building blocks for the DB-backed photo sources (gallery, immich,
// synology, google_photos). Each per-source plugin owns its own DB filter
// and image loader; everything else — random selection with exclusion
// fallback, orientation-aware smart collage, photo-date lookup — lives here
// so the plugins stay small and uniform.

import (
	"fmt"
	"image"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/imagesource"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/imageops"
)

// PhotoPicker selects one DB photo record matching the given orientation
// (empty string = any) and excluding the given IDs. Different sources use
// different filter clauses, but the contract is the same.
type PhotoPicker func(orientationFilter string, excludeIDs []uint) (model.Image, error)

// PhotoLoader decodes the underlying photo bytes for a DB record into an
// image. Synology / Immich call out to their services; gallery and Google
// Photos read from local files.
type PhotoLoader func(item model.Image) (image.Image, error)

// RunDBPhotoFlow is the shared workflow every DB-backed photo source uses:
// optionally compose a smart collage when the photo orientation mismatches
// the device, otherwise pick + load one photo, then look up PhotoTakenAt
// if the device shows photo dates. The two callbacks (pick / load) are
// the per-source bits. The ordered path derives its pool filters (Immich album,
// favorites-only, on-this-day) from the device itself via devicePoolScopes.
func RunDBPhotoFlow(
	req *imagesource.Request,
	db *gorm.DB,
	source string,
	pick PhotoPicker,
	load PhotoLoader,
) (*imagesource.Response, error) {
	var img image.Image
	var ids []uint
	var err error

	switch {
	case req.Device != nil && req.Device.EnableCollage:
		// Collage is inherently a random-pairing feature; it ignores the
		// device's display-order setting and keeps shuffling pairs.
		img, ids, err = smartCollage(req.Width, req.Height, req.ExcludeIDs, pick, load)
	case req.Device != nil:
		// Ordered single-photo selection (shuffle / chronological / custom).
		var item model.Image
		item, err = pickOrderedPhoto(db, req.Device, source, req.Preview, req.LastServedOverride,
			devicePoolScopes(req.Device, source, true)...)
		// On-this-day with no photos for today: fall back to the full pool
		// (minus the date filter) so the frame still shows something.
		if err == gorm.ErrRecordNotFound && req.Device.OnThisDay {
			item, err = pickOrderedPhoto(db, req.Device, source, req.Preview, req.LastServedOverride,
				devicePoolScopes(req.Device, source, false)...)
		}
		if err != nil {
			return nil, err
		}
		img, err = load(item)
		if err == nil {
			ids = []uint{item.ID}
		}
	default:
		// No device context (e.g. ad-hoc pulls): fall back to random.
		var item model.Image
		item, err = pickRandomWithFallback(pick, req.ExcludeIDs)
		if err != nil {
			return nil, err
		}
		img, err = load(item)
		if err == nil {
			ids = []uint{item.ID}
		}
	}
	if err != nil {
		return nil, err
	}

	resp := &imagesource.Response{Image: img, ImageIDs: ids}
	if req.Device != nil && len(ids) > 0 && ids[0] != 0 &&
		(req.Device.ShowPhotoDate || req.Device.ShowNames || req.Device.ShowLocation || req.Device.ShowDescription) {
		var stored model.Image
		if e := db.Select("photo_taken_at", "people_json", "location", "description", "caption").First(&stored, ids[0]).Error; e == nil {
			resp.PhotoTakenAt = stored.PhotoTakenAt
			resp.PeopleJSON = stored.PeopleJSON
			resp.Location = stored.Location
			// Prefer a real description; fall back to a user-set gallery caption.
			resp.Description = stored.Description
			if resp.Description == "" {
				resp.Description = stored.Caption
			}
		}
	}
	return resp, nil
}

// PickRandomDBPhoto returns a random model.Image for the given source,
// optionally filtered by orientation ("landscape" / "portrait" — "auto" is
// always matched alongside) and excluding ids. Generic over the four sources
// that follow the source = ? filter shape (gallery, immich, synology, google).
func PickRandomDBPhoto(db *gorm.DB, source, orientationFilter string, excludeIDs []uint, scope ...func(*gorm.DB) *gorm.DB) (model.Image, error) {
	query := db.Order("RANDOM()").Where("source = ? AND hidden = 0", source)
	if len(excludeIDs) > 0 {
		query = query.Where("id NOT IN ?", excludeIDs)
	}
	if orientationFilter != "" {
		query = query.Where("orientation IN ?", []string{orientationFilter, "auto"})
	}
	for _, fn := range scope {
		if fn != nil {
			query = fn(query)
		}
	}
	var item model.Image
	err := query.First(&item).Error
	return item, err
}

// orderedCandidateIDs builds the canonical ordered list of candidate image IDs
// for a device+source in the device's current DisplayOrder mode, using the
// device's CURRENT shuffle seed (it never bumps it). This is the shared
// list-building step behind both the next-photo cursor (pickOrderedPhoto) and
// the rotation position/size reporting (ComputeRotationStatus), so the two
// always agree on order and length.
func orderedCandidateIDs(db *gorm.DB, device *model.Device, source string, scope ...func(*gorm.DB) *gorm.DB) ([]uint, string, error) {
	mode := model.NormalizeDisplayOrder(device.DisplayOrder)
	// Hidden photos are globally excluded from every rotation.
	base := db.Model(&model.Image{}).Where("source = ? AND hidden = 0", source)
	for _, fn := range scope {
		if fn != nil {
			base = fn(base)
		}
	}
	switch mode {
	case model.DisplayOrderChronoNewest:
		base = base.Order("COALESCE(photo_taken_at, created_at) DESC, id DESC")
	case model.DisplayOrderChronoOldest:
		base = base.Order("COALESCE(photo_taken_at, created_at) ASC, id ASC")
	case model.DisplayOrderCustom:
		base = base.Order("display_order ASC, id ASC")
	default: // shuffle — pull in a stable order, then shuffle deterministically
		base = base.Order("id ASC")
	}
	var ids []uint
	if err := base.Pluck("id", &ids).Error; err != nil {
		return nil, mode, err
	}
	if mode == model.DisplayOrderShuffle {
		deterministicShuffle(ids, device.ShuffleSeed)
	}
	return ids, mode, nil
}

// deviceSourceScope returns the per-device pool filter for a source, or nil when
// the whole source pool applies. Today only Immich has one: a frame can restrict
// itself to selected albums (via the membership join table). Shared so the
// source plugin, the next-photo cursor, and the rotation-size report all count
// the exact same pool.
func deviceSourceScope(device *model.Device, source string) func(*gorm.DB) *gorm.DB {
	if device == nil || source != model.SourceImmich {
		return nil
	}
	ids := model.ParseImmichAlbumIDs(device.ImmichAlbumIDs)
	if len(ids) == 0 {
		return nil
	}
	return func(q *gorm.DB) *gorm.DB {
		return q.Where(
			"id IN (SELECT image_id FROM immich_image_albums WHERE immich_album_id IN ?)", ids)
	}
}

// onThisDayScope filters the pool to photos taken on today's month/day (any
// year), using the capture date and falling back to created_at. Server local
// time. nil when the device's on-this-day mode is off.
func onThisDayScope(device *model.Device) func(*gorm.DB) *gorm.DB {
	if device == nil || !device.OnThisDay {
		return nil
	}
	return func(q *gorm.DB) *gorm.DB {
		return q.Where(
			"strftime('%m-%d', COALESCE(photo_taken_at, created_at)) = strftime('%m-%d', 'now', 'localtime')")
	}
}

// favoritesScope filters the pool to starred photos. nil when off.
func favoritesScope(device *model.Device) func(*gorm.DB) *gorm.DB {
	if device == nil || !device.FavoritesOnly {
		return nil
	}
	return func(q *gorm.DB) *gorm.DB { return q.Where("favorite = 1") }
}

// devicePoolScopes collects every per-device pool filter for a source: the
// album filter, plus the optional favorites-only and (when includeOnThisDay)
// on-this-day filters. The on-this-day filter is separable so callers can drop
// it and fall back to the full pool on days with no matching photos.
func devicePoolScopes(device *model.Device, source string, includeOnThisDay bool) []func(*gorm.DB) *gorm.DB {
	var scopes []func(*gorm.DB) *gorm.DB
	if s := deviceSourceScope(device, source); s != nil {
		scopes = append(scopes, s)
	}
	if s := favoritesScope(device); s != nil {
		scopes = append(scopes, s)
	}
	if includeOnThisDay {
		if s := onThisDayScope(device); s != nil {
			scopes = append(scopes, s)
		}
	}
	return scopes
}

// RotationStatus describes where a frame is in its image rotation, for the Home
// Assistant sensors and the on-image overlay. Ordered is false for sources that
// have no deterministic sequence (collage / synthetic / url_proxy), in which case
// the numeric fields are meaningless and callers should skip the report.
type RotationStatus struct {
	Total     int    // number of images in this frame's rotation pool
	Position  int    // 1-based index of the current image (0 = unknown)
	Remaining int    // images left in this cycle after the current one (Total-Position)
	Mode      string // normalized DisplayOrder
	Ordered   bool   // false for collage / non-ordered sources
}

// ComputeRotationStatus reports the size of a frame's rotation pool and where the
// current image sits in it. currentID is the image being served right now (so the
// overlay/sensor reflects the in-flight pull before DeviceHistory is written);
// pass 0 to derive it from the most recently served photo. The order, seed and
// pool filter match pickOrderedPhoto exactly, so Position/Remaining line up with
// what the frame actually shows next.
func ComputeRotationStatus(db *gorm.DB, device *model.Device, source string, currentID uint) RotationStatus {
	rs := RotationStatus{Mode: model.NormalizeDisplayOrder(device.DisplayOrder)}
	if device == nil || device.EnableCollage || !model.IsOrderedSource(source) {
		return rs
	}
	ids, mode, err := orderedCandidateIDs(db, device, source, devicePoolScopes(device, source, true)...)
	if err != nil || len(ids) == 0 {
		return rs
	}
	rs.Ordered = true
	rs.Mode = mode
	rs.Total = len(ids)
	cur := currentID
	if cur == 0 {
		cur = lastServedImageID(db, device.ID, source)
	}
	if cur != 0 {
		if idx := indexOfUint(ids, cur); idx >= 0 {
			rs.Position = idx + 1
			rs.Remaining = rs.Total - rs.Position
		}
	}
	return rs
}

// FormatRotationOverlay builds the compact on-image chip text + Material Symbols
// icon name for a rotation status. For shuffle it shows images left in the cycle
// (a shuffle glyph); for chronological/custom it shows the current image number
// (a "#" tag glyph). showTotal appends "/<pool size>". Returns empty text when
// there's nothing meaningful to show (no ordered position yet).
func FormatRotationOverlay(rs RotationStatus, showTotal bool) (text, icon string) {
	if !rs.Ordered || rs.Total <= 0 || rs.Position <= 0 {
		return "", ""
	}
	n := rs.Position
	icon = "tag"
	if rs.Mode == model.DisplayOrderShuffle {
		n = rs.Remaining
		icon = "shuffle"
	}
	if showTotal {
		text = fmt.Sprintf("%d/%d", n, rs.Total)
	} else {
		text = strconv.Itoa(n)
	}
	return text, icon
}

// ApplySkip pins the image N steps from the current one in the device's ordered
// rotation (positive = forward, negative = back, 0 = re-show current), so the
// next ordered pull jumps there and then continues from that point. Wraps at
// both ends. Returns the pinned image ID, or 0 on a no-op: collage / non-ordered
// sources, an empty pool, or steps == 0. Shared by the HA "Skip Queue" command
// and the server WebGUI / REST skip endpoint so both behave identically. The
// caller is responsible for republishing state (e.g. NotifyDeviceUpdated).
func ApplySkip(db *gorm.DB, device *model.Device, steps int) uint {
	if device == nil || steps == 0 || device.EnableCollage {
		return 0
	}
	source := deviceSourceFromConfig(device.DeviceConfig)
	if !model.IsOrderedSource(source) {
		return 0
	}
	ids, _, err := orderedCandidateIDs(db, device, source, devicePoolScopes(device, source, true)...)
	if err != nil || len(ids) == 0 {
		return 0
	}
	curIdx := indexOfUint(ids, lastServedImageID(db, device.ID, source))
	if curIdx < 0 {
		curIdx = 0
	}
	target := ((curIdx+steps)%len(ids) + len(ids)) % len(ids)
	pin := ids[target]
	db.Model(device).Update("pending_next_image_id", pin)
	device.PendingNextImageID = pin
	return pin
}

// pickOrderedPhoto selects the next photo for a device according to its
// DisplayOrder mode. Every mode reduces to the same idea: build the canonical
// ordered list of candidate image IDs for the source, find where the
// last-served photo sits, and return the next one (wrapping at the end).
//
//   - chrono_newest / chrono_oldest: sort by capture date (fallback created_at)
//   - custom: sort by Image.DisplayOrder
//   - shuffle: deterministic shuffle seeded by Device.ShuffleSeed; when a full
//     cycle completes the seed is bumped so the next pass is a fresh order.
//
// The cursor is derived from DeviceHistory (the most recent served photo for
// this device+source), so no separate per-device position needs persisting.
func pickOrderedPhoto(db *gorm.DB, device *model.Device, source string, preview bool, lastServedOverride uint, scope ...func(*gorm.DB) *gorm.DB) (model.Image, error) {
	ids, mode, err := orderedCandidateIDs(db, device, source, scope...)
	if err != nil {
		return model.Image{}, err
	}
	if len(ids) == 0 {
		return model.Image{}, gorm.ErrRecordNotFound
	}

	// A manual "skip" pins the exact next image (HA "Skip Queue"). Honor it before
	// the normal cursor: a real pull serves it and clears the pin (rotation then
	// continues from there); a preview shows it without consuming. A pin no longer
	// in the pool (hidden/deleted/out of the current scope) is simply ignored, so a
	// stale pin can never get the rotation stuck.
	if device.PendingNextImageID != 0 {
		if idx := indexOfUint(ids, device.PendingNextImageID); idx >= 0 {
			pinned := device.PendingNextImageID
			if !preview {
				device.PendingNextImageID = 0
				db.Model(device).Update("pending_next_image_id", 0)
			}
			var item model.Image
			if err := db.First(&item, pinned).Error; err == nil {
				return item, nil
			}
		}
	}

	next := 0
	lastID := lastServedOverride
	if lastID == 0 {
		lastID = lastServedImageID(db, device.ID, source)
	}
	if lastID != 0 {
		if idx := indexOfUint(ids, lastID); idx >= 0 {
			next = idx + 1
			if next >= len(ids) {
				// Completed a full pass through the library.
				next = 0
				if mode == model.DisplayOrderShuffle {
					// Next cycle's order. A preview must reflect what the next
					// real pull will show, so compute it with the bumped seed —
					// but never persist the bump for a preview (read-only).
					seed := device.ShuffleSeed + 1
					if !preview {
						device.ShuffleSeed = seed
						db.Model(device).Update("shuffle_seed", seed)
					}
					deterministicShuffle(ids, seed)
				}
			}
		}
	}

	var item model.Image
	if err := db.First(&item, ids[next]).Error; err != nil {
		return model.Image{}, err
	}
	return item, nil
}

// lastServedImageID returns the image ID most recently served to the device
// from the given source, or 0 if none. Soft-deleted images are ignored so the
// cursor doesn't get stuck on a removed photo.
func lastServedImageID(db *gorm.DB, deviceID uint, source string) uint {
	var ids []uint
	db.Model(&model.DeviceHistory{}).
		Joins("JOIN images ON images.id = device_histories.image_id").
		Where("device_histories.device_id = ? AND images.source = ? AND images.deleted_at IS NULL",
			deviceID, source).
		Order("device_histories.served_at DESC").
		Limit(1).
		Pluck("device_histories.image_id", &ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return 0
}

// deterministicShuffle shuffles ids in place using a seeded PRNG, so the same
// (seed, input order) always yields the same permutation — letting us recompute
// a shuffle cycle's order across requests without persisting the whole sequence.
func deterministicShuffle(ids []uint, seed int64) {
	r := rand.New(rand.NewSource(seed))
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
}

func indexOfUint(s []uint, v uint) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// pickRandomWithFallback retries with exclusions dropped if the first
// query found nothing — matches existing behavior of fetchRandomPhoto.
func pickRandomWithFallback(pick PhotoPicker, excludeIDs []uint) (model.Image, error) {
	item, err := pick("", excludeIDs)
	if err == nil {
		return item, nil
	}
	if len(excludeIDs) == 0 {
		return item, err
	}
	return pick("", nil)
}

// smartCollage fetches one or two photos and composes them into a collage
// when the first photo's orientation doesn't match the device's. The
// callbacks let each source pick / load through its own backend.
func smartCollage(
	screenW, screenH int,
	excludeIDs []uint,
	pick PhotoPicker,
	load PhotoLoader,
) (image.Image, []uint, error) {
	devicePortrait := screenH > screenW

	item1, err := pickRandomWithFallback(pick, excludeIDs)
	if err != nil {
		return nil, nil, err
	}
	img1, err := load(item1)
	if err != nil {
		return nil, nil, err
	}
	servedIDs := []uint{item1.ID}

	bounds := img1.Bounds()
	isPhotoPortrait := bounds.Dy() > bounds.Dx()
	if isPhotoPortrait == devicePortrait {
		return img1, servedIDs, nil
	}

	// Each collage slot has the *opposite* shape of the device: a
	// portrait device stacks two landscape-shaped slots vertically, a
	// landscape device places two portrait-shaped slots side-by-side.
	// We only reach this branch when the first photo's orientation
	// differs from the device's, so the first photo already matches the
	// slot shape — request a second photo of the same orientation.
	targetType := "landscape"
	if isPhotoPortrait {
		targetType = "portrait"
	}

	// 1. Exclude history + the first photo.
	excludeWithHistory := append(append([]uint(nil), excludeIDs...), item1.ID)
	item2, err := pick(targetType, excludeWithHistory)
	if err != nil || item2.ID == item1.ID {
		log.Printf("smartCollage: %s query with history exclusion failed: %v, retrying without history", targetType, err)
		// 2. Just exclude the first photo, ignore history.
		item2, err = pick(targetType, []uint{item1.ID})
	}

	var img2 image.Image
	if err == nil && item2.ID != item1.ID {
		img2, err = load(item2)
	}
	if err != nil || item2.ID == item1.ID {
		log.Printf("smartCollage: no different %s photo found, using same photo twice", targetType)
		img2 = img1
		servedIDs = append(servedIDs, item1.ID)
	} else {
		servedIDs = append(servedIDs, item2.ID)
	}

	if devicePortrait {
		return createVerticalCollage(img1, img2, screenW, screenH), servedIDs, nil
	}
	return createHorizontalCollage(img1, img2, screenW, screenH), servedIDs, nil
}

func createVerticalCollage(img1, img2 image.Image, width, height int) image.Image {
	slotHeight := height / 2
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	imageops.DrawCover(dst, image.Rect(0, 0, width, slotHeight), img1)
	imageops.DrawCover(dst, image.Rect(0, slotHeight, width, height), img2)
	return dst
}

func createHorizontalCollage(img1, img2 image.Image, width, height int) image.Image {
	slotWidth := width / 2
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	imageops.DrawCover(dst, image.Rect(0, 0, slotWidth, height), img1)
	imageops.DrawCover(dst, image.Rect(slotWidth, 0, width, height), img2)
	return dst
}

// ResolveLocalPath handles path differences between docker (/data/...) and
// local dev (./data/...). Used by sources that store photos on disk
// (gallery, google_photos). Returns the original path if nothing resolves.
func ResolveLocalPath(dataDir, path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	for _, prefix := range []string{"/data/", "/app/data/"} {
		if strings.HasPrefix(path, prefix) {
			rel := strings.TrimPrefix(path, prefix)
			candidate := filepath.Join(dataDir, rel)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return path
}

// LoadLocalPhoto opens a model.Image record stored on disk and decodes it.
func LoadLocalPhoto(dataDir string, item model.Image) (image.Image, error) {
	resolved := ResolveLocalPath(dataDir, item.FilePath)
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}
