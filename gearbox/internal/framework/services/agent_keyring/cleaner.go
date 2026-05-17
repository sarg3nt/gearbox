package agent_keyring

import (
	"context"
	"log/slog"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

// CleanerInterval is the cadence at which RetiredKeyCleaner walks the
// fleet looking for keys whose retired_at + overlap window has passed.
// One hour is a sensible default: short enough that a 24-hour overlap
// rotation cleans up within a few hours of its target, long enough that
// the sweep is cheap (one DB query per box, one DELETE per agent).
const CleanerInterval = 1 * time.Hour

// RetiredKeyCleaner is a background service that periodically walks
// every box and asks the Rotator to remove any keys whose retired_at
// is older than the overlap window. The Phase 3 manual-rotate buttons
// stamp retired_at but don't remove the old key — the cleaner does, so
// retired keys don't linger forever after a rotation.
//
// Phase 4 in the original plan also covered auto-rotation on a
// schedule; that needs a new global-settings surface in the dashboard
// (the dashboard doesn't have one yet for app-level config) and is
// intentionally deferred to a follow-up. The cleaner is the smaller
// piece that's genuinely needed regardless of whether auto-rotation
// is enabled.
type RetiredKeyCleaner struct {
	rotator       *Rotator
	db            *database.DB
	overlapWindow time.Duration
	interval      time.Duration
	logger        *slog.Logger
}

// NewCleaner builds a cleaner around an existing rotator. Pass
// overlapWindow=0 to use DefaultOverlapWindow; interval=0 → CleanerInterval.
func NewCleaner(rotator *Rotator, db *database.DB, overlapWindow, interval time.Duration, logger *slog.Logger) *RetiredKeyCleaner {
	if overlapWindow <= 0 {
		overlapWindow = DefaultOverlapWindow
	}
	if interval <= 0 {
		interval = CleanerInterval
	}
	return &RetiredKeyCleaner{
		rotator:       rotator,
		db:            db,
		overlapWindow: overlapWindow,
		interval:      interval,
		logger:        logger,
	}
}

// Run blocks until ctx is cancelled. Sweeps once immediately on start
// so a freshly-deployed dashboard catches up on any retired keys left
// over from manual rotations done while the previous instance was
// down, then ticks every interval.
//
// Errors are logged but not propagated — one bad box shouldn't break
// the fleet sweep, and we don't want to spam fatal errors at startup
// for a transient agent outage.
func (c *RetiredKeyCleaner) Run(ctx context.Context) {
	c.logger.Info("retired-key cleaner started",
		"interval", c.interval,
		"overlap_window", c.overlapWindow)

	c.sweepOnce(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("retired-key cleaner stopped")
			return
		case <-ticker.C:
			c.sweepOnce(ctx)
		}
	}
}

func (c *RetiredKeyCleaner) sweepOnce(ctx context.Context) {
	boxes, err := c.db.GetEnabledBoxes()
	if err != nil {
		c.logger.Warn("cleaner: failed to list boxes", "error", err)
		return
	}

	total := 0
	for _, box := range boxes {
		if ctx.Err() != nil {
			return
		}
		removed, err := c.rotator.CleanupRetiredKeys(box.ID, c.overlapWindow)
		if err != nil {
			c.logger.Warn("cleaner: box sweep failed",
				"box_id", box.ID, "name", box.Name, "error", err)
			continue
		}
		if removed > 0 {
			c.logger.Info("cleaner: swept retired keys",
				"box_id", box.ID, "name", box.Name, "removed", removed)
			total += removed
		}
	}
	if total > 0 {
		c.logger.Info("cleaner: sweep complete", "total_removed", total)
	}
}
