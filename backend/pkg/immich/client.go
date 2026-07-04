package immich

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/pkg/mdns"
)

// Client is an Immich API client using API key authentication
type Client struct {
	BaseURL        string
	APIKey         string
	httpClient     *http.Client
	downloadClient *http.Client
}

// NewClient creates a new Immich client
func NewClient(baseURL, apiKey string) *Client {
	transport := mdns.NewTransport()
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		APIKey:  apiKey,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		downloadClient: &http.Client{
			Timeout:   2 * time.Minute,
			Transport: transport,
		},
	}
}

func (c *Client) do(method, path string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	return c.httpClient.Do(req)
}

// TestConnection verifies the server is reachable and the API key is valid.
//
// It first tries /api/users/me, but a scoped API key may lack the user.read
// permission (→ 403) and older/newer Immich versions may not expose that exact
// path (→ 404). In those cases we fall back to /api/albums, which is the
// permission the app actually needs for syncing, so the key is considered
// valid as long as it can list albums. A 401 anywhere means the key is bad.
func (c *Client) TestConnection() error {
	resp, err := c.do("GET", "/api/users/me")
	if err != nil {
		return err
	}
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("invalid API key")
	case http.StatusForbidden, http.StatusNotFound:
		// Scoped key without user.read, or endpoint absent on this version —
		// validate against the endpoint we actually use instead.
		return c.testViaAlbums(resp.StatusCode)
	default:
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}
}

// testViaAlbums confirms the key works against /api/albums. prevStatus is the
// status from the /api/users/me probe, reported if albums also fails for an
// unexpected reason.
func (c *Client) testViaAlbums(prevStatus int) error {
	resp, err := c.do("GET", "/api/albums")
	if err != nil {
		return err
	}
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("invalid API key")
	default:
		return fmt.Errorf("server returned status: %d", prevStatus)
	}
}

// ListAlbums returns all albums visible to the API key owner
func (c *Client) ListAlbums() ([]Album, error) {
	resp, err := c.do("GET", "/api/albums")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}
	var albums []Album
	if err := json.NewDecoder(resp.Body).Decode(&albums); err != nil {
		return nil, err
	}
	return albums, nil
}

// GetAlbumAssets returns all image assets in the given album.
//
// Immich v3 removed the `assets` property from GET /api/albums/:id (the album
// detail response no longer embeds its assets at all), so this goes through
// POST /api/search/metadata with an albumIds filter instead, per Immich's own
// v3 migration guide.
func (c *Client) GetAlbumAssets(albumID string) ([]Asset, error) {
	return c.SearchAssets(SearchMetadataRequest{AlbumIds: []string{albumID}})
}

// GetAsset fetches the full detail for one asset, including the recognized
// people (faces) — which album/search listings omit.
func (c *Client) GetAsset(assetID string) (*Asset, error) {
	resp, err := c.do("GET", "/api/assets/"+assetID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status: %d", resp.StatusCode)
	}
	var asset Asset
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		return nil, err
	}
	return &asset, nil
}

// GetThumbnail fetches thumbnail bytes for an asset.
// size is "thumbnail" (small) or "preview" (large).
func (c *Client) GetThumbnail(assetID, size string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/api/assets/"+assetID+"/thumbnail?size="+size, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "image/jpeg,image/*,*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("thumbnail fetch returned status %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

// doJSON is like do() but for POST bodies with a JSON payload.
func (c *Client) doJSON(method, path string, body interface{}) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, c.BaseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

// SearchAssets pages through POST /api/search/metadata until the server runs
// out of results, returning every IMAGE asset that matched. Use the
// pre-filled SearchMetadataRequest to pick a filter mode (favorites,
// date-bound, etc.); the function fills in Type, Page, Size, WithExif, and
// WithPeople itself — without the latter two Immich omits exifInfo/people
// from every item even though the data exists.
func (c *Client) SearchAssets(filter SearchMetadataRequest) ([]Asset, error) {
	const pageSize = 250
	filter.Type = "IMAGE"
	filter.Size = pageSize
	filter.WithExif = true
	filter.WithPeople = true

	var out []Asset
	for page := 1; ; page++ {
		filter.Page = page
		resp, err := c.doJSON("POST", "/api/search/metadata", filter)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("search returned status %d: %s", resp.StatusCode, string(b))
		}
		var body searchAssetsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()
		out = append(out, body.Assets.Items...)
		if len(body.Assets.Items) < pageSize || body.Assets.NextPage == nil {
			break
		}
	}
	return out, nil
}

// GetMemoryAssets returns the flattened set of "on this day" assets — one
// MemoryLane per past year that has a photo from this month/day.
//
// The /api/memories endpoint must be scoped with a `for` date, otherwise
// Immich returns every persisted memory lane the user has rather than the
// ones relevant to today. We pass today's date (UTC) plus type=on_this_day
// so the frame shows "this day, past years" instead of a random grab-bag.
func (c *Client) GetMemoryAssets() ([]Asset, error) {
	q := url.Values{}
	q.Set("for", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	q.Set("type", "on_this_day")
	resp, err := c.do("GET", "/api/memories?"+q.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("memories returned status %d: %s", resp.StatusCode, string(b))
	}
	var lanes []MemoryLane
	if err := json.NewDecoder(resp.Body).Decode(&lanes); err != nil {
		return nil, err
	}
	var out []Asset
	for _, lane := range lanes {
		out = append(out, lane.Assets...)
	}
	return out, nil
}

// DownloadOriginal fetches the original full-resolution asset.
func (c *Client) DownloadOriginal(assetID string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/api/assets/"+assetID+"/original", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("original download returned status %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}
