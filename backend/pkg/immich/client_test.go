package immich

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GetMemoryAssets must scope /api/memories with a `for` date and
// type=on_this_day, otherwise Immich returns every persisted memory lane
// instead of today's "on this day, past years". It must also flatten the
// returned lanes into a single asset slice.
func TestGetMemoryAssets_ScopesToTodayAndFlattens(t *testing.T) {
	var gotPath, gotFor, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotFor = r.URL.Query().Get("for")
		gotType = r.URL.Query().Get("type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"lane-2023","assets":[{"id":"a1"},{"id":"a2"}]},
			{"id":"lane-2022","assets":[{"id":"a3"}]}
		]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	assets, err := c.GetMemoryAssets()
	if err != nil {
		t.Fatalf("GetMemoryAssets returned error: %v", err)
	}

	if gotPath != "/api/memories" {
		t.Errorf("path = %q, want /api/memories", gotPath)
	}
	if gotFor == "" {
		t.Error("missing `for` query param — memories not scoped to today")
	}
	if gotType != "on_this_day" {
		t.Errorf("type = %q, want on_this_day", gotType)
	}
	if len(assets) != 3 {
		t.Fatalf("got %d assets, want 3 (lanes not flattened)", len(assets))
	}
	if assets[0].ID != "a1" || assets[2].ID != "a3" {
		t.Errorf("unexpected flattened order: %+v", assets)
	}
}

// GetAlbumAssets must go through POST /api/search/metadata with an albumIds
// filter: Immich v3 removed the `assets` property from the GET /api/albums/:id
// response, so the old "fetch album detail with withAssets=true" approach
// silently returns zero assets against a v3 server.
func TestGetAlbumAssets_UsesSearchMetadataWithAlbumIdsFilter(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets":{"count":1,"total":1,"nextPage":null,"items":[{"id":"a1","type":"IMAGE"}]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	assets, err := c.GetAlbumAssets("album-123")
	if err != nil {
		t.Fatalf("GetAlbumAssets returned error: %v", err)
	}

	if gotMethod != "POST" || gotPath != "/api/search/metadata" {
		t.Errorf("request = %s %s, want POST /api/search/metadata", gotMethod, gotPath)
	}
	albumIds, _ := gotBody["albumIds"].([]any)
	if len(albumIds) != 1 || albumIds[0] != "album-123" {
		t.Errorf("albumIds = %v, want [\"album-123\"]", gotBody["albumIds"])
	}
	if gotBody["withExif"] != true {
		t.Errorf("withExif = %v, want true — otherwise location/description come back empty", gotBody["withExif"])
	}
	if gotBody["withPeople"] != true {
		t.Errorf("withPeople = %v, want true — otherwise faces come back empty", gotBody["withPeople"])
	}
	if len(assets) != 1 || assets[0].ID != "a1" {
		t.Errorf("unexpected assets: %+v", assets)
	}
}
