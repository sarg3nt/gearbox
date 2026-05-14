package gear

// MetricCategory groups metric data by what it describes so the agent
// can pick a single "primary" producer per category when multiple
// sources could supply the same thing on one host.
//
// Most metric data is single-source on any given box. The category
// abstraction matters for HTTP request statistics (and a couple of
// other future cases) where two or more proxies / web servers might
// each be capable of producing requests/sec, response codes, and the
// like — for instance a host running HAProxy in front of nginx, where
// both genuinely see traffic. Without a primary-source concept the
// dashboard would double-count or have to ask the user which numbers
// to trust.
//
// Selection of the primary is auto-detection first: the manager walks
// preferenceOrder for the category and picks the first gear that
// probed Available. Operators override via per-category env vars
// (GEARBOX_AGENT_HTTP_SOURCE, etc.) when auto-detection picks wrong on
// their host — an extreme edge case but the one the override exists
// for.
type MetricCategory string

const (
	// CategoryHTTPRequests covers request volume, response codes,
	// response times, and per-vhost/backend breakdowns. Today only
	// HAProxy produces these; nginx/Apache/Caddy/Traefik join the
	// category as their detection lands (issue #95).
	CategoryHTTPRequests MetricCategory = "http_requests"
)

// AllMetricCategories lists every defined category in a stable order.
// Used by the manager to iterate when resolving primary sources and by
// tests that need to enumerate the set.
func AllMetricCategories() []MetricCategory {
	return []MetricCategory{
		CategoryHTTPRequests,
	}
}

// SourceSelection records the resolution of one metric category: which
// gear is primary, how the selection was made, and what alternatives
// were also available. Surfaced in the capability manifest so the
// dashboard can render which source's metrics it's about to display
// (and a "switch to X instead" hint when alternatives exist).
type SourceSelection struct {
	// Category is the metric category this selection applies to.
	Category MetricCategory `json:"category"`

	// Source is the gear name (matching Info().Name) that was picked
	// as primary. Empty means no available producer for this category
	// on this host — the field is then omitted from the manifest.
	Source string `json:"source,omitempty"`

	// Reason is operator-readable: "auto-detected from preference
	// order", "operator override via GEARBOX_AGENT_HTTP_SOURCE",
	// "operator override fell back to auto-detect — chosen gear is
	// not available", etc. Surfaces in the manifest so an operator
	// who sets an override can confirm at a glance that it took
	// effect.
	Reason string `json:"reason,omitempty"`

	// Alternatives lists the other gears that probed Available and
	// could have produced metrics for this category. Useful for the
	// dashboard's "you have N sources for this; click to switch"
	// affordance, and for operators trying to choose an override.
	// Stable order (preferenceOrder for this category).
	Alternatives []string `json:"alternatives,omitempty"`
}

// MetricSourceGear is implemented by gears that produce metrics for one
// or more MetricCategory values. The manager builds the
// category-to-producers map from these declarations and uses it to
// resolve primary sources. Gears that don't implement this interface
// don't participate in source selection — they're still in the
// manifest, but no category points at them.
type MetricSourceGear interface {
	Gear

	// MetricCategories returns the categories this gear produces
	// data for. Returning an empty slice is allowed but pointless —
	// such a gear should simply not implement the interface.
	MetricCategories() []MetricCategory
}
