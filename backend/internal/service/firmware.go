package service

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Where the frames' firmware is published. Overridable so a fork (or a test) can
// point elsewhere without a rebuild.
const defaultFirmwareRepo = "Snille/esp32-photoframe"

// Only the newest few releases are ever interesting: we want the most recent one
// carrying a given board's asset. Asking for more just moves megabytes around —
// the full list is well over 250 KB with a dozen assets per release.
const firmwareReleasePageSize = 5

// How long a lookup is reused. Firmware releases are cut by hand, minutes apart
// at worst, so an hour is plenty — and it keeps the whole fleet down to a couple
// of GitHub calls a day instead of one per frame per day (the unauthenticated
// rate limit is 60/hour per IP, which several frames behind one NAT can exhaust).
const firmwareCacheTTL = time.Hour

// Negative results are cached briefly too, so a GitHub outage doesn't turn every
// image serve into a failing API call.
const firmwareErrorCacheTTL = 5 * time.Minute

// FirmwareRelease is the newest published release carrying a specific board's
// OTA image.
type FirmwareRelease struct {
	Version string // release tag, e.g. "v2.17.4"
	URL     string // browser_download_url of that board's .bin
}

// FirmwareService answers "is there newer firmware for this board, and where is
// it" from a shared cache.
//
// This exists so the SERVER can tell a frame that an update is waiting, on the
// image response it is already fetching. Before this, every frame asked GitHub
// itself on a 24 h timer: an extra TLS handshake and a multi-hundred-KB JSON
// parse per frame per day, up to a day of latency before a release was noticed,
// and each frame independently exposed to a rate limit it shares with its
// siblings. One cached lookup here serves the whole fleet.
type FirmwareService struct {
	repo   string
	client *http.Client

	mu        sync.Mutex
	byBoard   map[string]FirmwareRelease
	fetchedAt time.Time
	lastErr   error
}

func NewFirmwareService() *FirmwareService {
	repo := os.Getenv("FIRMWARE_REPO")
	if repo == "" {
		repo = defaultFirmwareRepo
	}
	return &FirmwareService{
		repo:   repo,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// LatestForBoard returns the newest published release carrying that board's
// asset. ok is false when the board is unknown, no release carries its image, or
// the lookup failed — callers should then simply say nothing to the frame, which
// falls back to checking for itself.
func (s *FirmwareService) LatestForBoard(board string) (FirmwareRelease, bool) {
	if board == "" {
		return FirmwareRelease{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.needsRefreshLocked() {
		s.refreshLocked()
	}
	rel, ok := s.byBoard[board]
	return rel, ok
}

func (s *FirmwareService) needsRefreshLocked() bool {
	if s.fetchedAt.IsZero() {
		return true
	}
	ttl := firmwareCacheTTL
	if s.lastErr != nil {
		ttl = firmwareErrorCacheTTL
	}
	return time.Since(s.fetchedAt) > ttl
}

// refreshLocked reloads the board→release map. On failure it keeps whatever was
// cached before: stale-but-real data beats telling every frame "no update".
func (s *FirmwareService) refreshLocked() {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", s.repo, firmwareReleasePageSize)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		s.fetchedAt, s.lastErr = time.Now(), err
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "esp32-photoframe-server")

	resp, err := s.client.Do(req)
	if err != nil {
		s.fetchedAt, s.lastErr = time.Now(), err
		log.Printf("Firmware lookup failed: %v (keeping previous result)", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.fetchedAt = time.Now()
		s.lastErr = fmt.Errorf("github returned %d", resp.StatusCode)
		log.Printf("Firmware lookup: GitHub returned %d (keeping previous result)", resp.StatusCode)
		return
	}

	var releases []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		s.fetchedAt, s.lastErr = time.Now(), err
		log.Printf("Firmware lookup: could not parse releases (%v), keeping previous result", err)
		return
	}

	// Releases come newest-first. Keep the first hit per board so a release cut
	// for other boards doesn't hide an older one that does carry this board's
	// image — the same rule the frames' own check uses.
	byBoard := map[string]FirmwareRelease{}
	for _, r := range releases {
		if r.Draft || r.Prerelease || r.TagName == "" {
			continue
		}
		for _, a := range r.Assets {
			board := boardFromAssetName(a.Name)
			if board == "" {
				continue
			}
			if _, seen := byBoard[board]; !seen {
				byBoard[board] = FirmwareRelease{Version: r.TagName, URL: a.URL}
			}
		}
	}

	if len(byBoard) == 0 {
		s.fetchedAt = time.Now()
		s.lastErr = fmt.Errorf("no board assets found in the %d newest releases", len(releases))
		log.Printf("Firmware lookup: %v", s.lastErr)
		return
	}

	s.byBoard = byBoard
	s.fetchedAt = time.Now()
	s.lastErr = nil
	log.Printf("Firmware lookup refreshed: %d boards, newest %s", len(byBoard), newestOf(byBoard))
}

// boardFromAssetName maps "esp32-photoframe-<board>.bin" to <board>. The merged
// factory images ("photoframe-firmware-<board>-merged.bin") are deliberately not
// matched: they are for a fresh USB flash and would wipe NVS if a frame ever
// pulled one as an OTA image.
func boardFromAssetName(name string) string {
	const prefix = "esp32-photoframe-"
	const suffix = ".bin"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
}

func newestOf(byBoard map[string]FirmwareRelease) string {
	for _, r := range byBoard {
		return r.Version
	}
	return "?"
}
