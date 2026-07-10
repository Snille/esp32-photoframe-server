package service

import (
	"bytes"
	"image"

	"gorm.io/gorm"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/imagesource"
	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
)

// immichSource is the registry plugin for Immich-hosted photos.
type immichSource struct {
	db     *gorm.DB
	immich *ImmichService
}

// NewImmichSource constructs the plugin.
func NewImmichSource(db *gorm.DB, immich *ImmichService) imagesource.Source {
	return &immichSource{db: db, immich: immich}
}

func (s *immichSource) Name() string { return model.SourceImmich }

func (s *immichSource) Fetch(req *imagesource.Request) (*imagesource.Response, error) {
	// If this frame has Immich albums selected, restrict its pool to assets in
	// those albums (via the membership join table). Empty = all Immich photos.
	// Shared with the rotation-status report so both count the same pool.
	albumScope := deviceSourceScope(req.Device, model.SourceImmich)
	pick := func(orientation string, exclude []uint) (model.Image, error) {
		return PickRandomDBPhoto(s.db, model.SourceImmich, orientation, exclude, albumScope)
	}
	load := func(item model.Image) (image.Image, error) {
		data, err := s.immich.DownloadPhoto(uint(item.ImmichServerID), item.ImmichAssetID)
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
	return RunDBPhotoFlow(req, s.db, model.SourceImmich, pick, load)
}
