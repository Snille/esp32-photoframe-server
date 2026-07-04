package immich

// Album represents an Immich album
type Album struct {
	ID         string `json:"id"`
	AlbumName  string `json:"albumName"`
	AssetCount int    `json:"assetCount"`
}

// ExifInfo holds EXIF metadata for an asset
type ExifInfo struct {
	ExifImageWidth   int    `json:"exifImageWidth"`
	ExifImageHeight  int    `json:"exifImageHeight"`
	Orientation      string `json:"orientation"` // EXIF orientation e.g. "1", "6", "Rotate 90 CW"
	DateTimeOriginal string `json:"dateTimeOriginal"`
	City             string `json:"city"`
	State            string `json:"state"`
	Country          string `json:"country"`
	Description      string `json:"description"`
}

// Person is a face recognized in an asset. Populated by the per-asset detail
// endpoint (GET /api/assets/{id}) always, and by album/search listings only
// when the request sets withPeople (see SearchAssets).
type Person struct {
	Name      string `json:"name"`
	BirthDate string `json:"birthDate"` // "YYYY-MM-DD" or empty
}

// Asset represents an Immich media asset
type Asset struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"` // "IMAGE", "VIDEO"
	OriginalFileName string   `json:"originalFileName"`
	LocalDateTime    string   `json:"localDateTime"`
	ExifInfo         ExifInfo `json:"exifInfo"`
	People           []Person `json:"people"`
}

// SearchMetadataRequest is the body for POST /api/search/metadata. Only the
// fields we use are declared; Immich ignores unknown JSON keys on input.
//
// Immich only joins in exifInfo/people when the request explicitly asks for
// them (withExif/withPeople) — omitting them returns those fields empty even
// though the data exists server-side. SearchAssets always sets both to true
// so location/description/people survive the sync.
type SearchMetadataRequest struct {
	Type        string   `json:"type,omitempty"`       // "IMAGE" — we filter out videos
	IsFavorite  *bool    `json:"isFavorite,omitempty"` // pointer so we can send false vs unset
	TakenAfter  string   `json:"takenAfter,omitempty"` // RFC3339
	TakenBefore string   `json:"takenBefore,omitempty"`
	AlbumIds    []string `json:"albumIds,omitempty"` // restrict results to these albums
	WithExif    bool     `json:"withExif,omitempty"`
	WithPeople  bool     `json:"withPeople,omitempty"`
	Page        int      `json:"page,omitempty"`
	Size        int      `json:"size,omitempty"`
}

// searchAssetsResponse matches /api/search/metadata's "assets" envelope.
type searchAssetsResponse struct {
	Assets struct {
		Count    int     `json:"count"`
		Total    int     `json:"total"`
		NextPage *string `json:"nextPage"`
		Items    []Asset `json:"items"`
	} `json:"assets"`
}

// MemoryLane is one "on this day" group returned by GET /api/memories.
type MemoryLane struct {
	ID     string  `json:"id"`
	Assets []Asset `json:"assets"`
}
