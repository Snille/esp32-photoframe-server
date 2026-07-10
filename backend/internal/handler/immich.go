package handler

import (
	"net/http"
	"strconv"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/service"
	"github.com/labstack/echo/v4"
)

type ImmichHandler struct {
	immich *service.ImmichService
}

func NewImmichHandler(s *service.ImmichService) *ImmichHandler {
	return &ImmichHandler{immich: s}
}

// serverRequest is the add/edit body for an Immich server. An empty APIKey on
// edit means "keep the existing key" (the UI never echoes secrets back).
type serverRequest struct {
	Label   string `json:"label"`
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
}

// TestConnection tests arbitrary credentials (the add/edit form's "Test" button,
// before the server is saved). Body: {url, api_key}.
func (h *ImmichHandler) TestConnection(c echo.Context) error {
	var req serverRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if err := h.immich.TestCredentials(req.URL, req.APIKey); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// ListServers returns all configured Immich servers.
func (h *ImmichHandler) ListServers(c echo.Context) error {
	servers, err := h.immich.AllServers()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, servers)
}

func (h *ImmichHandler) CreateServer(c echo.Context) error {
	var req serverRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	srv, err := h.immich.CreateServer(req.Label, req.URL, req.APIKey, req.Enabled)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, srv)
}

func (h *ImmichHandler) UpdateServer(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var req serverRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if err := h.immich.UpdateServer(uint(id), req.Label, req.URL, req.APIKey, req.Enabled); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ImmichHandler) DeleteServer(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.immich.DeleteServer(uint(id)); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// TestServer tests a saved server by id.
func (h *ImmichHandler) TestServer(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.immich.TestConnection(uint(id)); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// ListAlbums returns the albums exposed by every enabled server, tagged with the
// server id + label so the per-device picker can group by server.
func (h *ImmichHandler) ListAlbums(c echo.Context) error {
	albums, err := h.immich.ListAllAlbums()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, albums)
}

// Sync runs the non-destructive incremental sync (new/removed/edited assets,
// stable IDs, rotation cursors preserved) across all servers. "Sync Now" button.
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
