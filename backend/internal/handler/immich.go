package handler

import (
	"net/http"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/service"
	"github.com/labstack/echo/v4"
)

type ImmichHandler struct {
	immich *service.ImmichService
}

func NewImmichHandler(s *service.ImmichService) *ImmichHandler {
	return &ImmichHandler{immich: s}
}

func (h *ImmichHandler) TestConnection(c echo.Context) error {
	if err := h.immich.TestConnection(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *ImmichHandler) ListAlbums(c echo.Context) error {
	albums, err := h.immich.ListAlbums()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, albums)
}

// Sync runs the non-destructive incremental sync (new/removed/edited assets,
// stable IDs, rotation cursors preserved). Backs the "Sync Now" button.
func (h *ImmichHandler) Sync(c echo.Context) error {
	if err := h.immich.SyncNow(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "synced"})
}

// Resync hard-deletes all Immich photos and re-imports from scratch. Destructive:
// resets every frame's rotation cursor and re-fetches people/faces. Backs the
// "Rebuild Library" button (UI confirms first).
func (h *ImmichHandler) Resync(c echo.Context) error {
	if err := h.immich.ClearAndResync(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "resynced"})
}

func (h *ImmichHandler) Clear(c echo.Context) error {
	if err := h.immich.ClearPhotos(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "cleared"})
}

func (h *ImmichHandler) GetPhotoCount(c echo.Context) error {
	count, err := h.immich.GetPhotoCount()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"count": count})
}

// UsedAlbums returns the Immich albums that currently have synced photos, for
// the Gallery's per-album filter (see ListUsedAlbums).
func (h *ImmichHandler) UsedAlbums(c echo.Context) error {
	albums, err := h.immich.ListUsedAlbums()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, albums)
}
