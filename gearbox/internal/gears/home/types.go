package home

// BookmarkConfig is the type-specific config persisted on a bookmark tile.
type BookmarkConfig struct {
	URL         string `json:"url"`
	Name        string `json:"name"`
	IconURL     string `json:"icon_url,omitempty"`
	Description string `json:"description,omitempty"`
}

// AppConfig is the type-specific config persisted on an app tile.
// (Real widget rendering arrives in Phase 6 — Phase 3 just renders the
// launcher half: icon + name + URL + status dot.)
type AppConfig struct {
	URL                   string   `json:"url"`
	Name                  string   `json:"name"`
	IconURL               string   `json:"icon_url,omitempty"`
	AppSlug               string   `json:"app_slug,omitempty"`
	SelectedFields        []string `json:"selected_fields,omitempty"`
	StatusIntervalSeconds int      `json:"status_interval_seconds,omitempty"`
	StatusChecksDisabled  bool     `json:"status_checks_disabled,omitempty"`
}

// CustomAPIMapping is one field-to-display mapping inside a customapi tile.
type CustomAPIMapping struct {
	Field  string `json:"field"`            // dot-notation path, e.g. "origin.name"
	Label  string `json:"label"`            // display label
	Format string `json:"format,omitempty"` // text|number|float|percent|duration|bytes|bitrate|date|relativeDate
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

// CustomAPIAuthType enumerates the authentication modes a customapi tile supports.
type CustomAPIAuthType string

const (
	// CustomAPIAuthNone means no auth headers are added.
	CustomAPIAuthNone CustomAPIAuthType = "none"
	// CustomAPIAuthBasic uses HTTP Basic. The password lives in home_tile_secrets.
	CustomAPIAuthBasic CustomAPIAuthType = "basic"
	// CustomAPIAuthBearer adds an "Authorization: Bearer <token>" header. Token in secrets.
	CustomAPIAuthBearer CustomAPIAuthType = "bearer"
	// CustomAPIAuthHeader adds a single arbitrary header (e.g. X-Api-Key). Value in secrets.
	CustomAPIAuthHeader CustomAPIAuthType = "header"
)

// CustomAPIConfig is the config for a customapi tile.
// Sensitive values (basic password, bearer token, header value) live in
// home_tile_secrets — only the SecretSet boolean and the public fields
// (header name, basic username, etc.) are stored here.
type CustomAPIConfig struct {
	URL               string             `json:"url"`
	Name              string             `json:"name"`
	IconURL           string             `json:"icon_url,omitempty"`
	Method            string             `json:"method,omitempty"` // GET (default) or POST
	Headers           map[string]string  `json:"headers,omitempty"`
	RequestBody       string             `json:"request_body,omitempty"`
	RefreshIntervalMs int                `json:"refresh_interval_ms,omitempty"`
	Auth              CustomAPIAuthType  `json:"auth,omitempty"`
	BasicUsername     string             `json:"basic_username,omitempty"`
	HeaderName        string             `json:"header_name,omitempty"`
	Mappings          []CustomAPIMapping `json:"mappings,omitempty"`
}

// IframeConfig is the config for an iframe tile.
type IframeConfig struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

// SearchConfig is the config for a search-bar tile.
type SearchConfig struct {
	Engine      string `json:"engine,omitempty"`      // duckduckgo|google|kagi|bing
	Placeholder string `json:"placeholder,omitempty"` // optional override
}

// WeatherConfig is the config for a weather tile.
type WeatherConfig struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Label     string  `json:"label,omitempty"`
	Units     string  `json:"units,omitempty"` // metric|imperial
}

// ClockConfig is the config for a clock tile.
type ClockConfig struct {
	Timezone string `json:"timezone,omitempty"` // IANA, default user-agent locale
	Format   string `json:"format,omitempty"`   // 12|24
}
