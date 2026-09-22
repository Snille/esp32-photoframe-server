package immich

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if want := time.Now().Format(time.DateOnly); gotFor != want {
		t.Errorf("for = %q, want date-only local date %q (Immich v3.0.3+ rejects timestamps)", gotFor, want)
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

// Immich v3.0.0-v3.0.2 validate `for` as a full ISO datetime and reject the
// date-only form with a 400; the client must retry once with the legacy
// timestamp layout so those versions keep working.
func TestGetMemoryAssets_RetriesLegacyTimestampOn400(t *testing.T) {
	var fors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := r.URL.Query().Get("for")
		fors = append(fors, f)
		if !strings.Contains(f, "T") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"for must be a valid ISO 8601 date string"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"lane","assets":[{"id":"a1"}]}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	assets, err := c.GetMemoryAssets()
	if err != nil {
		t.Fatalf("GetMemoryAssets returned error: %v", err)
	}
	if len(fors) != 2 {
		t.Fatalf("got %d requests, want 2 (date-only then legacy retry): %v", len(fors), fors)
	}
	if _, perr := time.Parse(legacyMemoriesForLayout, fors[1]); perr != nil {
		t.Errorf("retry for = %q, not in legacy layout: %v", fors[1], perr)
	}
	if len(assets) != 1 || assets[0].ID != "a1" {
		t.Errorf("unexpected assets after retry: %+v", assets)
	}
}

// Asset byte fetches must ask for the edited rendition (edited=true) so crops
// made in the Immich editor reach the frame, and fall back to the plain path
// when a server rejects the parameter.
func TestAssetFetch_SendsEditedTrueAndFallsBackOn400(t *testing.T) {
	var paths []string
	rejectEdited := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if rejectEdited && r.URL.Query().Get("edited") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("bytes"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "test-key")

	if _, err := c.DownloadOriginal("x1"); err != nil {
		t.Fatalf("DownloadOriginal: %v", err)
	}
	if _, err := c.GetThumbnail("x1", "preview"); err != nil {
		t.Fatalf("GetThumbnail: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/api/assets/x1/original?edited=true" ||
		paths[1] != "/api/assets/x1/thumbnail?size=preview&edited=true" {
		t.Fatalf("unexpected paths: %v", paths)
	}

	paths = nil
	rejectEdited = true
	data, err := c.DownloadOriginal("x2")
	if err != nil {
		t.Fatalf("DownloadOriginal with strict server: %v", err)
	}
	if string(data) != "bytes" || len(paths) != 2 || paths[1] != "/api/assets/x2/original" {
		t.Fatalf("fallback not taken: data=%q paths=%v", data, paths)
	}
}
