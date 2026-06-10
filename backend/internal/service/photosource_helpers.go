package service

// Shared building blocks for the DB-backed photo sources (gallery, immich,
// synology, google_photos). Each per-source plugin owns its own DB filter
// and image loader; everything else — random selection with exclusion
// fallback, orientation-aware smart collage, photo-date lookup — lives here
// so the plugins stay small and uniform.

import (
	"image"
	"log"
	"math/rand"
	"os"
	"path/filepath"
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
// the per-source bits.
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
		item, err = pickOrderedPhoto(db, req.Device, source)
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
func PickRandomDBPhoto(db *gorm.DB, source, orientationFilter string, excludeIDs []uint) (model.Image, error) {
	query := db.Order("RANDOM()").Where("source = ?", source)
	if len(excludeIDs) > 0 {
		query = query.Where("id NOT IN ?", excludeIDs)
	}
	if orientationFilter != "" {
		query = query.Where("orientation IN ?", []string{orientationFilter, "auto"})
	}
	var item model.Image
	err := query.First(&item).Error
	return item, err
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
func pickOrderedPhoto(db *gorm.DB, device *model.Device, source string) (model.Image, error) {
	mode := model.NormalizeDisplayOrder(device.DisplayOrder)

	base := db.Model(&model.Image{}).Where("source = ?", source)
	var ids []uint
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
	if err := base.Pluck("id", &ids).Error; err != nil {
		return model.Image{}, err
	}
	if len(ids) == 0 {
		return model.Image{}, gorm.ErrRecordNotFound
	}
	if mode == model.DisplayOrderShuffle {
		deterministicShuffle(ids, device.ShuffleSeed)
	}

	next := 0
	if lastID := lastServedImageID(db, device.ID, source); lastID != 0 {
		if idx := indexOfUint(ids, lastID); idx >= 0 {
			next = idx + 1
			if next >= len(ids) {
				// Completed a full pass through the library.
				next = 0
				if mode == model.DisplayOrderShuffle {
					device.ShuffleSeed++
					db.Model(device).Update("shuffle_seed", device.ShuffleSeed)
					deterministicShuffle(ids, device.ShuffleSeed)
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
