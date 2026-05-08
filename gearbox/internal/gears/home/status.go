package home

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

// TileStatus is the per-tile reachability summary.
type TileStatus string

const (
	// StatusUp — most recent probe succeeded with 2xx/3xx.
	StatusUp TileStatus = "up"
	// StatusDown — most recent probe failed (timeout / connection refused / 5xx).
	StatusDown TileStatus = "down"
	// StatusDegraded — probe got a non-success but not-clearly-down response (4xx).
	StatusDegraded TileStatus = "degraded"
	// StatusUnknown — never probed yet, or status checks are disabled for this tile.
	StatusUnknown TileStatus = "unknown"
)

// StatusEvent is the payload broadcast on the SSE bus when a tile's status changes.
type StatusEvent struct {
	TileID     int64      `json:"tile_id"`
	Status     TileStatus `json:"status"`
	LatencyMs  int64      `json:"latency_ms"`
	HTTPStatus int        `json:"http_status"`
	CheckedAt  time.Time  `json:"checked_at"`
	Error      string     `json:"error,omitempty"`
}

// statusWorker probes tile URLs on a per-tile schedule with exponential backoff
// on failure, and publishes StatusEvents to the SSE hub. It pauses entirely
// when no SSE clients are connected.
type statusWorker struct {
	logger *slog.Logger
	hub    *sseHub
	dbFn   func() *database.DB

	mu     sync.Mutex
	states map[int64]*tileState

	// latest is the snapshot of the last computed status for each tile.
	// Reads are protected by mu. Browsers that miss SSE events can fetch
	// from /home/api/tiles/{id}/status to recover.
	latest map[int64]StatusEvent

	stopCh chan struct{}
	doneCh chan struct{}

	// httpClient is configured for short timeouts and TLS-laxness; many
	// self-hosted services ship self-signed certs.
	httpClient *http.Client
}

// tileState is the worker's per-tile bookkeeping.
type tileState struct {
	URL             string
	IntervalSeconds int
	NextCheck       time.Time
	BackoffSeconds  int
	Disabled        bool
	LastStatus      TileStatus
}

// minStatusInterval / maxStatusInterval bound the per-tile cadence the user
// can configure, in seconds.
const (
	minStatusInterval = 10
	maxStatusInterval = 300
	defaultInterval   = 30
	probeTimeout      = 5 * time.Second
	dbRefreshInterval = 60 * time.Second
)

// backoffSchedule lists the on-failure intervals (seconds) the worker walks
// through before plateauing at the ceiling.
var backoffSchedule = []int{30, 60, 120, 300}

// newStatusWorker builds the worker. dbFn is a closure so the worker doesn't
// have to dance around the AuthAdapter downcast at every call site.
func newStatusWorker(logger *slog.Logger, hub *sseHub, dbFn func() *database.DB) *statusWorker {
	return &statusWorker{
		logger: logger,
		hub:    hub,
		dbFn:   dbFn,
		states: make(map[int64]*tileState),
		latest: make(map[int64]StatusEvent),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		httpClient: &http.Client{
			Timeout: probeTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				// Don't follow redirects past one — many self-hosted apps
				// redirect / to /login, which counts as "up" enough.
				DisableKeepAlives: false,
			},
		},
	}
}

// Start spins up the worker goroutine.
func (w *statusWorker) Start(ctx context.Context) {
	go w.run(ctx)
}

// Stop signals the worker to exit and waits for it.
func (w *statusWorker) Stop() {
	select {
	case <-w.stopCh:
		// already closed
	default:
		close(w.stopCh)
	}
	<-w.doneCh
}

// run is the worker loop.
func (w *statusWorker) run(ctx context.Context) {
	defer close(w.doneCh)

	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	dbTick := time.NewTicker(dbRefreshInterval)
	defer dbTick.Stop()

	w.refreshTiles()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-dbTick.C:
			w.refreshTiles()
		case <-tick.C:
			if w.hub.activeClients() == 0 {
				continue // Nobody watching — don't burn cycles or hammer upstreams.
			}
			w.runDueChecks(ctx)
		}
	}
}

// refreshTiles syncs the worker's tile set with the database. New tiles are
// added; deleted tiles drop off; existing tiles' interval/disabled flags
// are picked up.
func (w *statusWorker) refreshTiles() {
	db := w.dbFn()
	if db == nil {
		return
	}
	gearCfg, err := w.gearConfig(db)
	if err != nil {
		w.logger.Warn("home worker: load gear config failed", "error", err)
	}
	if !gearCfg.HealthChecksEnabled {
		// Globally disabled — clear state and skip.
		w.mu.Lock()
		w.states = make(map[int64]*tileState)
		w.mu.Unlock()
		return
	}

	boards, err := db.ListHomeBoards()
	if err != nil {
		w.logger.Warn("home worker: ListHomeBoards failed", "error", err)
		return
	}

	seen := make(map[int64]struct{})
	now := time.Now()

	for _, b := range boards {
		tiles, err := db.ListHomeTiles(b.ID)
		if err != nil {
			w.logger.Warn("home worker: ListHomeTiles failed", "board", b.ID, "error", err)
			continue
		}
		for _, t := range tiles {
			url, interval, disabled, ok := tileProbeMeta(t, gearCfg.DefaultStatusIntervalSeconds)
			if !ok {
				continue
			}
			seen[t.ID] = struct{}{}
			w.mu.Lock()
			st, exists := w.states[t.ID]
			if !exists {
				st = &tileState{
					URL:             url,
					IntervalSeconds: interval,
					NextCheck:       now, // Probe immediately on first appearance.
					Disabled:        disabled,
					LastStatus:      StatusUnknown,
				}
				w.states[t.ID] = st
			} else {
				// Pick up live edits — but if the URL changed, reset backoff
				// so a previously-down tile gets a fresh chance immediately.
				if st.URL != url {
					st.URL = url
					st.NextCheck = now
					st.BackoffSeconds = 0
				}
				st.IntervalSeconds = interval
				st.Disabled = disabled
			}
			w.mu.Unlock()
		}
	}

	// Drop state for tiles that no longer exist.
	w.mu.Lock()
	for id := range w.states {
		if _, ok := seen[id]; !ok {
			delete(w.states, id)
			delete(w.latest, id)
		}
	}
	w.mu.Unlock()
}

// runDueChecks fires probes for every tile whose NextCheck is due.
func (w *statusWorker) runDueChecks(ctx context.Context) {
	now := time.Now()
	w.mu.Lock()
	due := make([]int64, 0, len(w.states))
	for id, st := range w.states {
		if st.Disabled {
			continue
		}
		if !now.Before(st.NextCheck) {
			due = append(due, id)
		}
	}
	w.mu.Unlock()

	for _, id := range due {
		go w.probeTile(ctx, id)
	}
}

// probeTile runs one HEAD probe (with GET fallback for 405s), updates state,
// and publishes a StatusEvent.
func (w *statusWorker) probeTile(ctx context.Context, id int64) {
	w.mu.Lock()
	st := w.states[id]
	if st == nil {
		w.mu.Unlock()
		return
	}
	url := st.URL
	w.mu.Unlock()

	start := time.Now()
	status, code, err := w.probeURL(ctx, url)
	latency := time.Since(start).Milliseconds()

	w.mu.Lock()
	defer w.mu.Unlock()
	st = w.states[id] // re-fetch in case it disappeared
	if st == nil {
		return
	}

	now := time.Now()
	if status == StatusUp {
		st.BackoffSeconds = 0
		st.NextCheck = now.Add(time.Duration(st.IntervalSeconds) * time.Second)
	} else {
		st.BackoffSeconds = nextBackoff(st.BackoffSeconds)
		st.NextCheck = now.Add(time.Duration(st.BackoffSeconds) * time.Second)
	}
	st.LastStatus = status

	evt := StatusEvent{
		TileID:     id,
		Status:     status,
		LatencyMs:  latency,
		HTTPStatus: code,
		CheckedAt:  now,
	}
	if err != nil {
		evt.Error = err.Error()
	}
	w.latest[id] = evt
	w.hub.publish("tile.status", evt)
}

// probeURL fires a HEAD, falling back to GET on 405. Returns the inferred
// status and the HTTP status code (0 if no response).
func (w *statusWorker) probeURL(ctx context.Context, url string) (TileStatus, int, error) {
	if url == "" {
		return StatusUnknown, 0, nil
	}

	headCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(headCtx, http.MethodHead, url, nil)
	if err != nil {
		return StatusDown, 0, err
	}
	resp, err := w.httpClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusMethodNotAllowed {
			return w.probeGET(ctx, url)
		}
		return classify(resp.StatusCode), resp.StatusCode, nil
	}

	// HEAD network failure — try GET before giving up. Some apps reject HEAD.
	return w.probeGET(ctx, url)
}

func (w *statusWorker) probeGET(ctx context.Context, url string) (TileStatus, int, error) {
	getCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, url, nil)
	if err != nil {
		return StatusDown, 0, err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return StatusDown, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return classify(resp.StatusCode), resp.StatusCode, nil
}

// classify turns an HTTP status code into a TileStatus.
func classify(code int) TileStatus {
	switch {
	case code >= 200 && code < 400:
		return StatusUp
	case code == 401 || code == 403:
		// Auth-protected endpoints respond with 401/403 even when healthy.
		return StatusUp
	case code >= 400 && code < 500:
		return StatusDegraded
	default:
		return StatusDown
	}
}

// nextBackoff picks the next backoff value off the schedule. Once at the
// ceiling, stays there until a probe succeeds (caller resets to 0).
func nextBackoff(current int) int {
	for _, s := range backoffSchedule {
		if current < s {
			return s
		}
	}
	return backoffSchedule[len(backoffSchedule)-1]
}

// gearConfig loads the home gear's system row config, returning sensible
// defaults when missing.
func (w *statusWorker) gearConfig(db *database.DB) (database.HomeConfig, error) {
	out := database.HomeConfig{
		HealthChecksEnabled:          true,
		DefaultStatusIntervalSeconds: defaultInterval,
	}
	g, err := db.GetGear(database.SystemServerID, database.GearHome)
	if err != nil || g == nil {
		return out, err
	}
	if len(g.Config) > 0 && string(g.Config) != "{}" {
		_ = json.Unmarshal(g.Config, &out)
	}
	if out.DefaultStatusIntervalSeconds == 0 {
		out.DefaultStatusIntervalSeconds = defaultInterval
	}
	return out, nil
}

// snapshot returns the latest status for one tile, if known.
func (w *statusWorker) snapshot(id int64) (StatusEvent, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	evt, ok := w.latest[id]
	return evt, ok
}

// tileProbeMeta extracts (URL, interval, disabled, ok) from a tile row.
// Returns ok=false for tile types that aren't health-checked (clock, search, etc.).
func tileProbeMeta(t database.HomeTile, defaultInterval int) (string, int, bool, bool) {
	switch t.Type {
	case database.TileTypeApp:
		var cfg AppConfig
		_ = json.Unmarshal(t.Config, &cfg)
		interval := cfg.StatusIntervalSeconds
		if interval < minStatusInterval || interval > maxStatusInterval {
			interval = defaultInterval
		}
		return cfg.URL, interval, cfg.StatusChecksDisabled, cfg.URL != ""
	case database.TileTypeBookmark:
		var cfg BookmarkConfig
		_ = json.Unmarshal(t.Config, &cfg)
		return cfg.URL, defaultInterval, false, cfg.URL != ""
	case database.TileTypeCustomAPI:
		var cfg CustomAPIConfig
		_ = json.Unmarshal(t.Config, &cfg)
		return cfg.URL, defaultInterval, false, cfg.URL != ""
	}
	return "", 0, false, false
}
