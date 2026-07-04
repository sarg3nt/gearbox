package home

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/webcore/core/crypto"
	"github.com/sarg3nt/gearbox/internal/gears/home/widget"
)

// WidgetEvent is the SSE payload for a widget data refresh.
type WidgetEvent struct {
	TileID    int64             `json:"tile_id"`
	Slug      string            `json:"slug"`
	Fields    map[string]string `json:"fields"`
	Error     string            `json:"error,omitempty"`
	FetchedAt time.Time         `json:"fetched_at"`
}

// widgetRunner pulls fresh data from registered Tier-1 providers on a
// per-tile cadence and broadcasts WidgetEvent updates on the SSE hub.
//
// It mirrors statusWorker's design — same tick rate, same pause-on-zero-
// SSE-clients behaviour, same DB-poll-every-60s — but kept separate so
// reachability (statusWorker) and data (widgetRunner) can evolve apart.
type widgetRunner struct {
	hub        *sseHub
	dbFn       func() *database.DB
	encryptor  func() *crypto.Encryptor
	stopCh     chan struct{}
	doneCh     chan struct{}
	mu         sync.Mutex
	nextRun    map[int64]time.Time
	lastResult map[int64]WidgetEvent
}

// widgetCadence is the per-tile widget refresh interval. Faster than
// status checks because users care about download speeds / queue counts
// updating more frequently than reachability.
const widgetCadence = 30 * time.Second

// newWidgetRunner builds a runner. encryptor() returns the *crypto.Encryptor
// at fetch time, since the lifecycle of the encryptor is wider than the runner.
func newWidgetRunner(hub *sseHub, dbFn func() *database.DB, encFn func() *crypto.Encryptor) *widgetRunner {
	return &widgetRunner{
		hub:        hub,
		dbFn:       dbFn,
		encryptor:  encFn,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		nextRun:    make(map[int64]time.Time),
		lastResult: make(map[int64]WidgetEvent),
	}
}

func (r *widgetRunner) Start(ctx context.Context) {
	go r.run(ctx)
}

func (r *widgetRunner) Stop() {
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
	<-r.doneCh
}

func (r *widgetRunner) run(ctx context.Context) {
	defer close(r.doneCh)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-tick.C:
			if r.hub.activeClients() == 0 {
				continue
			}
			r.runDue(ctx)
		}
	}
}

// runDue scans every tile, finds the ones whose Tier-1 widget data is due,
// and fires off goroutines to refresh them.
func (r *widgetRunner) runDue(ctx context.Context) {
	db := r.dbFn()
	if db == nil {
		return
	}
	boards, err := db.ListHomeBoards()
	if err != nil {
		return
	}
	now := time.Now()
	for _, b := range boards {
		tiles, err := db.ListHomeTiles(b.ID)
		if err != nil {
			continue
		}
		for _, t := range tiles {
			r.mu.Lock()
			when, ok := r.nextRun[t.ID]
			r.mu.Unlock()
			if ok && now.Before(when) {
				continue
			}
			switch t.Type {
			case database.TileTypeApp:
				cfg := tileAppConfigJSON(t)
				if cfg.AppSlug == "" || !widget.HasProvider(cfg.AppSlug) {
					continue
				}
				go r.fetchAndPublish(ctx, db, t, cfg)
			case database.TileTypeCustomAPI:
				go r.fetchCustomAPI(ctx, db, t)
			}
		}
	}
}

// fetchAndPublish runs one widget fetch, caches the result, and broadcasts.
func (r *widgetRunner) fetchAndPublish(ctx context.Context, db *database.DB, tile database.HomeTile, cfg AppConfig) {
	provider := widget.Get(cfg.AppSlug)
	if provider == nil {
		return
	}
	r.mu.Lock()
	r.nextRun[tile.ID] = time.Now().Add(widgetCadence)
	r.mu.Unlock()

	secret, basicUser := r.loadSecret(db, tile.ID)
	req := widget.Request{
		BaseURL:        cfg.URL,
		Secret:         secret,
		BasicUsername:  basicUser,
		SelectedFields: cfg.SelectedFields,
		Timeout:        5 * time.Second,
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	res, err := provider.Fetch(fetchCtx, req)
	evt := WidgetEvent{
		TileID:    tile.ID,
		Slug:      cfg.AppSlug,
		Fields:    res.Fields,
		FetchedAt: time.Now(),
	}
	if err != nil {
		evt.Error = err.Error()
	}
	r.mu.Lock()
	r.lastResult[tile.ID] = evt
	r.mu.Unlock()
	r.hub.publish("tile.widget", evt)
}

// fetchCustomAPI runs one customapi tile fetch.
func (r *widgetRunner) fetchCustomAPI(ctx context.Context, db *database.DB, tile database.HomeTile) {
	r.mu.Lock()
	r.nextRun[tile.ID] = time.Now().Add(widgetCadence)
	r.mu.Unlock()

	var cfg CustomAPIConfig
	if err := json.Unmarshal(tile.Config, &cfg); err != nil {
		return
	}
	secret, basicUser := r.loadSecret(db, tile.ID)
	wcfg := widget.CustomAPIConfig{
		URL:           cfg.URL,
		Method:        cfg.Method,
		Headers:       cfg.Headers,
		RequestBody:   cfg.RequestBody,
		Auth:          string(cfg.Auth),
		BasicUsername: cfg.BasicUsername,
		HeaderName:    cfg.HeaderName,
	}
	if cfg.Auth == CustomAPIAuthBasic && basicUser != "" {
		wcfg.BasicUsername = basicUser
	}
	for _, m := range cfg.Mappings {
		wcfg.Mappings = append(wcfg.Mappings, widget.CustomAPIMapping{
			Field:  m.Field,
			Label:  m.Label,
			Format: m.Format,
			Prefix: m.Prefix,
			Suffix: m.Suffix,
		})
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	res, err := widget.FetchCustomAPI(fetchCtx, cfg.URL, secret, wcfg, nil)
	evt := WidgetEvent{
		TileID:    tile.ID,
		Slug:      "customapi",
		Fields:    res.Fields,
		FetchedAt: time.Now(),
	}
	if err != nil {
		evt.Error = err.Error()
	}
	r.mu.Lock()
	r.lastResult[tile.ID] = evt
	r.mu.Unlock()
	r.hub.publish("tile.widget", evt)
}

// snapshot returns the most recent widget result for a tile.
func (r *widgetRunner) snapshot(id int64) (WidgetEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	evt, ok := r.lastResult[id]
	return evt, ok
}

// loadSecret decrypts the per-tile secret. For qBittorrent we encode
// "<basic_username>\n<password>" so a single secret blob covers both.
// Other providers store just the API key/token.
func (r *widgetRunner) loadSecret(db *database.DB, tileID int64) (secret, basicUsername string) {
	enc := r.encryptor()
	if enc == nil {
		return "", ""
	}
	payload, err := db.GetHomeTileSecret(tileID)
	if err != nil || len(payload) == 0 {
		return "", ""
	}
	plain, err := enc.DecryptString(payload)
	if err != nil {
		return "", ""
	}
	// Split on newline for combined username\npassword secrets.
	for i := 0; i < len(plain); i++ {
		if plain[i] == '\n' {
			return plain[i+1:], plain[:i]
		}
	}
	return plain, ""
}

// tileAppConfigJSON is a small wrapper around json.Unmarshal that returns
// a zero-value AppConfig on errors so callers can branch on AppSlug.
func tileAppConfigJSON(t database.HomeTile) AppConfig {
	var cfg AppConfig
	_ = json.Unmarshal(t.Config, &cfg)
	return cfg
}
