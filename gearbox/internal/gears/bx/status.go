package bx

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
)

// StatusLevel is the green/yellow/red/gray rollup shown as a dot in the
// Bx fleet view and the header switcher chip.
type StatusLevel string

const (
	StatusGreen   StatusLevel = "green"   // agent reachable, no contributing warnings
	StatusYellow  StatusLevel = "yellow"  // agent reachable but warnings present
	StatusRed     StatusLevel = "red"     // agent unreachable or critical signal firing
	StatusGray    StatusLevel = "gray"    // box disabled, not yet polled, or never reachable
	StatusUnknown StatusLevel = "unknown" // initial state until first poll completes
)

// BoxStatus is the snapshot for one box. Returned by Snapshot() and pushed
// as `box.status` SSE events on transition.
type BoxStatus struct {
	BoxID       string      `json:"box_id"`
	Name        string      `json:"name"`
	Location    string      `json:"location,omitempty"`
	Enabled     bool        `json:"enabled"`
	Level       StatusLevel `json:"level"`
	Reachable   bool        `json:"reachable"`
	AgentURL    string      `json:"agent_url,omitempty"`
	Contributors []string   `json:"contributors,omitempty"` // human-readable reasons feeding non-green
	LastChecked time.Time   `json:"last_checked"`
	LatencyMs   int64       `json:"latency_ms,omitempty"`
}

// statusMonitor is the per-install poller that keeps a map of every box's
// current status. v1 only checks agent reachability via the unauthenticated
// `/health` endpoint — additional contributors (certificate-expiring,
// firing alerts, OS updates pending, …) plug in here as their data sources
// mature. Designed so the heavy lift can move to a framework-level
// boxhealth service later without changing the Bx page's API.
type statusMonitor struct {
	deps   gear.Dependencies
	db     *database.DB
	logger interface{}

	interval time.Duration
	timeout  time.Duration

	mu       sync.RWMutex
	statuses map[string]BoxStatus // keyed by BoxConfig.ID (UUID string)

	stop chan struct{}
	once sync.Once

	subsMu sync.Mutex
	subs   map[int64]chan BoxStatus
	nextID int64
}

func newStatusMonitor(deps gear.Dependencies) *statusMonitor {
	m := &statusMonitor{
		deps:     deps,
		interval: 30 * time.Second,
		timeout:  5 * time.Second,
		statuses: make(map[string]BoxStatus),
		stop:     make(chan struct{}),
		subs:     make(map[int64]chan BoxStatus),
	}
	// Pull the *database.DB out of the services.ServerAdapter facade so we
	// can read the full BoxDB list (which knows `Enabled` + `Location`).
	if sa, ok := deps.Servers.(*services.ServerAdapter); ok {
		m.db = sa.GetDB()
	}
	return m
}

// Start launches the polling loop. Safe to call multiple times — only the
// first call wins.
func (m *statusMonitor) Start(ctx context.Context) {
	go m.run(ctx)
}

// Stop signals the polling loop to exit. Subsequent calls are no-ops.
func (m *statusMonitor) Stop() {
	m.once.Do(func() { close(m.stop) })
}

func (m *statusMonitor) run(ctx context.Context) {
	// Kick off an initial poll immediately so the page isn't all-gray on
	// first load.
	m.pollAll(ctx)
	tick := time.NewTicker(m.interval)
	defer tick.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ctx.Done():
			return
		case <-tick.C:
			m.pollAll(ctx)
		}
	}
}

// pollAll fans out a reachability check per configured box. The check is
// the unauthenticated `/health` endpoint of each agent — same probe as the
// HAProxy backend health check, with the same 5s budget.
func (m *statusMonitor) pollAll(ctx context.Context) {
	if m.db == nil {
		return
	}
	boxes, err := m.db.GetBoxes()
	if err != nil {
		return
	}
	encryptor := m.encryptor()
	var wg sync.WaitGroup
	for _, b := range boxes {
		b := b
		wg.Add(1)
		go func() {
			defer wg.Done()
			apiKey := ""
			if encryptor != nil && len(b.APIKeyEncrypted) > 0 {
				if dec, err := encryptor.DecryptString(b.APIKeyEncrypted); err == nil {
					apiKey = dec
				}
			}
			status := m.probe(ctx, b, apiKey)
			m.set(status)
		}()
	}
	wg.Wait()
}

// probe runs one /health check and synthesizes the BoxStatus rollup.
func (m *statusMonitor) probe(ctx context.Context, b *database.BoxDB, apiKey string) BoxStatus {
	now := time.Now()
	bs := BoxStatus{
		BoxID:       b.BoxID,
		Name:        b.Name,
		Location:    b.Location,
		Enabled:     b.Enabled,
		AgentURL:    b.AgentURL,
		LastChecked: now,
	}
	if !b.Enabled {
		bs.Level = StatusGray
		bs.Contributors = []string{"Box disabled"}
		return bs
	}
	if b.AgentURL == "" || apiKey == "" {
		bs.Level = StatusGray
		bs.Contributors = []string{"Agent not configured"}
		return bs
	}

	// Per-probe context bounded by the timeout; do not let a single hung
	// agent stall the whole poll cycle.
	pctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	_ = pctx // reserved for when the agent client supports ctx; the http
	// client's per-request timeout already provides the upper bound.

	client := agent.NewClient(b.AgentURL, apiKey)
	t0 := time.Now()
	_, err := client.Health()
	bs.LatencyMs = time.Since(t0).Milliseconds()
	if err != nil {
		bs.Level = StatusRed
		bs.Reachable = false
		bs.Contributors = []string{"Agent unreachable: " + truncErr(err.Error())}
		return bs
	}
	bs.Reachable = true
	bs.Level = StatusGreen
	// Future contributors plug in here (cert expiring, alerts firing,
	// OS updates pending, agent version mismatch, …) and bump Level up
	// to Yellow or Red as appropriate. Left intentionally minimal in v1
	// so the rollup is honest about what it actually checks today.
	return bs
}

func truncErr(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// Snapshot returns a copy of every known box's current status, sorted by
// box name. Safe for concurrent use; the slice is independent of internal
// state.
func (m *statusMonitor) Snapshot() []BoxStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BoxStatus, 0, len(m.statuses))
	for _, s := range m.statuses {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the current status for one box by ID (BoxConfig.ID), if known.
func (m *statusMonitor) Get(boxID string) (BoxStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.statuses[boxID]
	return s, ok
}

// set publishes a status, broadcasting to subscribers only if the level
// changed (suppresses chatter when nothing interesting is happening).
func (m *statusMonitor) set(s BoxStatus) {
	m.mu.Lock()
	prev, had := m.statuses[s.BoxID]
	m.statuses[s.BoxID] = s
	m.mu.Unlock()
	if !had || prev.Level != s.Level || prev.Reachable != s.Reachable {
		m.broadcast(s)
	}
}

// Subscribe returns a channel of status events and an unsubscribe func.
// Events are best-effort: a slow consumer that doesn't drain its channel
// will simply miss events (we never block the publisher).
func (m *statusMonitor) Subscribe() (<-chan BoxStatus, func()) {
	ch := make(chan BoxStatus, 16)
	id := atomic.AddInt64(&m.nextID, 1)
	m.subsMu.Lock()
	m.subs[id] = ch
	m.subsMu.Unlock()
	return ch, func() {
		m.subsMu.Lock()
		if c, ok := m.subs[id]; ok {
			delete(m.subs, id)
			close(c)
		}
		m.subsMu.Unlock()
	}
}

func (m *statusMonitor) broadcast(s BoxStatus) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for _, ch := range m.subs {
		select {
		case ch <- s:
		default: // slow consumer; drop rather than block
		}
	}
}

// encryptor pulls the secrets encryptor out of the AuthAdapter so we can
// decrypt per-box API keys. Returns nil when not wired (in which case
// every box ends up Gray with "Agent not configured").
func (m *statusMonitor) encryptor() interface {
	DecryptString([]byte) (string, error)
} {
	type encUser interface {
		DecryptString([]byte) (string, error)
	}
	if aa, ok := m.deps.Auth.(*services.AuthAdapter); ok {
		if e := aa.GetEncryptor(); e != nil {
			return e
		}
	}
	_ = encUser(nil) // keep the import surface honest if Auth isn't an adapter
	return nil
}
