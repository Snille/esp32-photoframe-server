package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"gorm.io/gorm"
)

// thumbFileStems are the on-disk file prefixes written per rendered image id:
// the 400px thumbnail and the native-size full render.
var thumbFileStems = []string{"thumb", "full"}

func removeThumbFiles(dataDir, thumbID string) {
	if dataDir == "" || thumbID == "" {
		return
	}
	for _, stem := range thumbFileStems {
		os.Remove(filepath.Join(dataDir, fmt.Sprintf("%s_%s.jpg", stem, thumbID)))
	}
}

// thumbStillReferenced reports whether thumbID still backs one of the device's
// retained image slots (current / previous / next), so its files must be kept.
func thumbStillReferenced(device *model.Device, thumbID string) bool {
	return thumbID != "" &&
		(thumbID == device.CurrentThumbID || thumbID == device.PrevThumbID || thumbID == device.NextThumbID)
}

// SetCurrentThumb rotates a device's thumbnail history: the existing current
// image becomes "previous", newThumbID becomes the current image, and the photo
// pushed out of the previous slot has its files deleted — unless they still back
// the new current or the next slot. The DB row + the in-memory device are both
// updated so callers see the rotated ids. Used by both serve paths (pull in
// handler/image.go, push in service/device.go) so Previous/Current stay
// consistent for the Home Assistant MQTT image entities.
func SetCurrentThumb(db *gorm.DB, dataDir string, device *model.Device, newThumbID string) {
	if newThumbID == "" || newThumbID == device.CurrentThumbID {
		return
	}
	oldCurrent := device.CurrentThumbID
	oldPrev := device.PrevThumbID

	// The old previous image is leaving the retained set — delete its files
	// unless the new current or the next slot still points at the same id.
	if oldPrev != "" && oldPrev != oldCurrent && oldPrev != newThumbID && oldPrev != device.NextThumbID {
		removeThumbFiles(dataDir, oldPrev)
	}

	db.Model(device).Updates(map[string]interface{}{
		"current_thumb_id": newThumbID,
		"prev_thumb_id":    oldCurrent,
	})
	device.PrevThumbID = oldCurrent
	device.CurrentThumbID = newThumbID
}

// SetNextThumb stores a fresh "next image" preview thumbnail id, deleting the
// previous next render's files unless they still back the current/previous slot.
func SetNextThumb(db *gorm.DB, dataDir string, device *model.Device, newNextID string) {
	if newNextID == device.NextThumbID {
		return
	}
	old := device.NextThumbID
	if old != "" && old != newNextID && old != device.CurrentThumbID && old != device.PrevThumbID {
		removeThumbFiles(dataDir, old)
	}
	db.Model(device).Update("next_thumb_id", newNextID)
	device.NextThumbID = newNextID
}
