package service

import (
	"testing"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Setting{}))
	return db
}

func TestSettingsService_SetGet(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)

	err := svc.Set("foo", "bar")
	assert.NoError(t, err)

	val, err := svc.Get("foo")
	assert.NoError(t, err)
	assert.Equal(t, "bar", val)
}

func TestSettingsService_Update(t *testing.T) {
	db := setupTestDB(t)
	svc := NewSettingsService(db)

	svc.Set("foo", "bar")
	svc.Set("foo", "baz")

	val, err := svc.Get("foo")
	assert.NoError(t, err)
	assert.Equal(t, "baz", val)
}
