package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/googlephotos"
	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/imageops"
	"gorm.io/gorm"
)

type PickerSessionResponse struct {
	ID            string `json:"id"`
	PickerUri     string `json:"pickerUri"`
	MediaItemsSet bool   `json:"mediaItemsSet"`
}

type PickedMediaItem struct {
	ID        string    `json:"id"`
	MediaFile MediaFile `json:"mediaFile"`
}

type MediaFile struct {
	BaseUrl  string `json:"baseUrl"`
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
}

type MediaItemsResponse struct {
	MediaItems    []PickedMediaItem `json:"mediaItems"`
	NextPageToken string            `json:"nextPageToken"`
}

type PickerProgress struct {
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Status    string `json:"status"` // "processing", "done", "error"
	Error     string `json:"error,omitempty"`
}

type PickerService struct {
	client  *googlephotos.Client
	db      *gorm.DB
	dataDir string

	// mu guards progress. ProcessSessionItems runs in its own background
	// goroutine (see handler/google.go's safego.Go call) while GetProgress is
	// polled concurrently from an HTTP handler, so both the map itself and
	// each PickerProgress entry's fields need protecting -- a plain map is
	// not safe for concurrent read+write, and this used to panic the whole
	// server ("fatal error: concurrent map read and map write") under real
	// polling load.
	mu       sync.Mutex
	progress map[string]*PickerProgress
}

func NewPickerService(client *googlephotos.Client, db *gorm.DB, dataDir string) *PickerService {
	return &PickerService{
		client:   client,
		db:       db,
		dataDir:  dataDir,
		progress: make(map[string]*PickerProgress),
	}
}

// GetProgress returns a snapshot copy, never the live pointer -- the caller
// must not see partially-updated fields while withProgress is mutating the
// same entry on another goroutine.
func (s *PickerService) GetProgress(sessionID string) *PickerProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.progress[sessionID]; ok {
		cp := *p
		return &cp
	}
	return nil
}

// withProgress mutates (creating if absent) the progress entry for
// sessionID under the lock. All progress updates in ProcessSessionItems go
// through this instead of touching s.progress directly.
func (s *PickerService) withProgress(sessionID string, mutate func(p *PickerProgress)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.progress[sessionID]
	if !ok {
		p = &PickerProgress{}
		s.progress[sessionID] = p
	}
	mutate(p)
}

func (s *PickerService) CreateSession() (string, string, error) {
	httpClient, err := s.client.GetClient()
	if err != nil {
		return "", "", err
	}

	// Create session
	body := []byte(`{}`) // Empty body
	req, err := http.NewRequest("POST", "https://photospicker.googleapis.com/v1/sessions", bytes.NewBuffer(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("failed to create session: status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var session PickerSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", "", err
	}

	return session.ID, session.PickerUri, nil
}

func (s *PickerService) PollSession(sessionID string) (bool, error) {
	httpClient, err := s.client.GetClient()
	if err != nil {
		return false, err
	}

	req, err := http.NewRequest("GET", "https://photospicker.googleapis.com/v1/sessions/"+sessionID, nil)
	if err != nil {
		return false, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false, fmt.Errorf("failed to get session: status %d", resp.StatusCode)
	}

	var session PickerSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return false, err
	}

	return session.MediaItemsSet, nil
}

func (s *PickerService) ProcessSessionItems(sessionID string) (int, error) {
	httpClient, err := s.client.GetClient()
	if err != nil {
		return 0, err
	}

	// Initialize Progress
	s.withProgress(sessionID, func(p *PickerProgress) {
		p.Status = "listing"
	})

	// Pagination loop
	var allItems []PickedMediaItem
	pageToken := ""

	for {
		// sessionId is REQUIRED for listing picked items
		url := fmt.Sprintf("https://photospicker.googleapis.com/v1/mediaItems?pageSize=100&sessionId=%s", sessionID)
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			s.withProgress(sessionID, func(p *PickerProgress) {
				p.Status = "error"
				p.Error = err.Error()
			})
			return 0, err
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			s.withProgress(sessionID, func(p *PickerProgress) {
				p.Status = "error"
				p.Error = err.Error()
			})
			return 0, err
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			errStr := fmt.Sprintf("failed to list items: %s", string(body))
			s.withProgress(sessionID, func(p *PickerProgress) {
				p.Status = "error"
				p.Error = errStr
			})
			return 0, fmt.Errorf("%s", errStr)
		}

		var listResp MediaItemsResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&listResp)
		resp.Body.Close()
		if decodeErr != nil {
			s.withProgress(sessionID, func(p *PickerProgress) {
				p.Status = "error"
				p.Error = decodeErr.Error()
			})
			return 0, decodeErr
		}

		allItems = append(allItems, listResp.MediaItems...)
		pageToken = listResp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	// Update Total
	s.withProgress(sessionID, func(p *PickerProgress) {
		p.Total = len(allItems)
		p.Status = "downloading"
	})

	// Download items
	count := 0
	photosDir := filepath.Join(s.dataDir, "photos")
	if err := os.MkdirAll(photosDir, 0755); err != nil {
		s.withProgress(sessionID, func(p *PickerProgress) {
			p.Status = "error"
			p.Error = err.Error()
		})
		return 0, err
	}

	markProcessed := func() {
		s.withProgress(sessionID, func(p *PickerProgress) {
			p.Processed++
		})
	}

	for _, item := range allItems {
		// Download High Quality
		if item.MediaFile.BaseUrl == "" {
			markProcessed() // Count skipped as processed? yes
			continue
		}

		// Skip videos
		if len(item.MediaFile.MimeType) >= 5 && item.MediaFile.MimeType[:5] == "video" {
			fmt.Printf("Skipping video: %s (%s)\n", item.MediaFile.Filename, item.MediaFile.MimeType)
			markProcessed()
			continue
		}

		downloadUrl := item.MediaFile.BaseUrl + "=w1600-h1600"

		resp, err := httpClient.Get(downloadUrl)
		if err != nil {
			fmt.Printf("Failed to download %s: %v\n", item.MediaFile.Filename, err)
			markProcessed()
			continue
		}

		// resp.Body is closed explicitly on every exit path below instead of
		// via defer -- defer only runs when ProcessSessionItems itself
		// returns, not per loop iteration, so a deferred close here would
		// leak one open response body per downloaded photo until the entire
		// import finished.
		if resp.StatusCode != 200 {
			fmt.Printf("Failed to download %s: status %d\n", item.MediaFile.Filename, resp.StatusCode)
			resp.Body.Close()
			markProcessed()
			continue
		}

		// Save to file
		// Use ID to avoid collisions?
		ext := ".jpg"
		localFilename := fmt.Sprintf("%s%s", item.ID, ext)
		localPath := filepath.Join(photosDir, localFilename)

		// Check for duplicate in DB
		var existing model.Image
		if err := s.db.Where("file_path = ?", localPath).First(&existing).Error; err == nil {
			// Record exists. Check if file exists.
			if _, err := os.Stat(localPath); err == nil {
				// Both exist. Skip.
				fmt.Printf("Skipping duplicate: %s\n", localFilename)
				resp.Body.Close()
				markProcessed()
				continue
			}
			// File missing, delete old record so we can re-download and strictly create new one
			s.db.Unscoped().Delete(&existing)
		}

		// Create file
		out, err := os.Create(localPath)
		if err != nil {
			resp.Body.Close()
			markProcessed()
			continue
		}

		// Write to file
		_, err = io.Copy(out, resp.Body)
		out.Close() // Close before opening for decode
		resp.Body.Close()
		if err != nil {
			markProcessed()
			continue
		}

		// Bake EXIF orientation into the pixels. Google's CDN with size
		// hints usually returns pre-rotated pixels, but the API doesn't
		// guarantee it; auto-orient is a cheap safety net. Non-fatal.
		if err := imageops.AutoOrient(localPath); err != nil {
			fmt.Printf("auto-orient failed for google photo %s: %v\n", localPath, err)
		}

		// Decode image config to get dimensions
		f, err := os.Open(localPath)
		if err != nil {
			markProcessed()
			continue
		}
		imgConfig, _, err := image.DecodeConfig(f)
		f.Close()

		width := 0
		height := 0
		orientation := "landscape"

		if err == nil {
			width = imgConfig.Width
			height = imgConfig.Height
			if height > width {
				orientation = "portrait"
			}
		}

		// Add to DB queue
		image := model.Image{
			FilePath:    localPath,
			Source:      model.SourceGooglePhotos, // Set source to google
			UserID:      1,                        // Default user
			Status:      "pending",
			CreatedAt:   time.Now(),
			Caption:     "", // Google Picker provides no useful caption
			Width:       width,
			Height:      height,
			Orientation: orientation,
		}
		s.db.Create(&image)
		count++

		// Update Progress
		markProcessed()
	}

	s.withProgress(sessionID, func(p *PickerProgress) {
		p.Status = "done"
	})
	return count, nil
}
