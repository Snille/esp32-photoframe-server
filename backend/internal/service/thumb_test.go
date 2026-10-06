package service

import (
	"testing"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestThumbReferenced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:thumbref?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Device{}))
	require.NoError(t, db.Create(&model.Device{Name: "a", CurrentThumbID: "c1", PrevThumbID: "p1"}).Error)
	require.NoError(t, db.Create(&model.Device{Name: "b", NextThumbID: "n2"}).Error)

	for _, id := range []string{"c1", "p1", "n2"} {
		require.True(t, ThumbReferenced(db, id), id)
	}
	require.False(t, ThumbReferenced(db, "throwaway"))
	require.False(t, ThumbReferenced(db, ""))

	require.Equal(t, map[string]bool{"c1": true, "p1": true, "n2": true}, ReferencedThumbIDs(db))
}
