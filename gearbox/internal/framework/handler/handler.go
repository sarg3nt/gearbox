package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/collector"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/services/email"
	"github.com/sarg3nt/gearbox/internal/framework/services/geoip"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	authManager      *auth.Manager
	webAuthnMgr      *auth.WebAuthnManager
	collectors       map[string]*collector.Manager // boxID -> collector
	servers          []models.BoxConfig
	logger           *slog.Logger
	db               *database.DB
	emailService     *email.Service
	encryptor        *crypto.Encryptor
	registry         *collector.Registry
	eventHub         *events.Hub
	wsManager        *collector.WebSocketManager
	geoipClient      *geoip.Client

	// capabilities caches per-box probe tables so gear-page handlers don't
	// fire a fresh agent call on every render. TTL is short enough that a
	// restarted agent's new probe table shows up within minutes; reconnect
	// events explicitly invalidate for an immediate refresh.
	capabilities *agent.CapabilitiesCache

	// Config redactor for hiding sensitive values (e.g., stats auth passwords)
	configRedactor *ConfigRedactor

	// Traffic delta tracking state
	// HAProxy provides cumulative counters, we need to calculate deltas
	trafficDeltaMu      sync.RWMutex
	prevSourceData      map[string]*trafficSourceSnapshot  // key: "boxID:ip:backend"
	prevBackendData     map[string]*trafficBackendSnapshot // key: "boxID:backend"
}

// trafficSourceSnapshot stores previous values for delta calculation
type trafficSourceSnapshot struct {
	Requests int64
	BytesIn  int64
	BytesOut int64
	LastSeen time.Time
}

// trafficBackendSnapshot stores previous values for delta calculation
type trafficBackendSnapshot struct {
	TotalRequests int64
	BytesIn       int64
	BytesOut      int64
	Response2xx   int64
	Response3xx   int64
	Response4xx   int64
	Response5xx   int64
	LastSeen      time.Time
}

// NewHandler creates a new handler instance.
func NewHandler(
	authManager *auth.Manager,
	collectors map[string]*collector.Manager,
	servers []models.BoxConfig,
	logger *slog.Logger,
	db *database.DB,
	emailService *email.Service,
	encryptor *crypto.Encryptor,
	registry *collector.Registry,
) *Handler {
	return &Handler{
		authManager:     authManager,
		collectors:      collectors,
		servers:         servers,
		logger:          logger,
		db:              db,
		emailService:    emailService,
		encryptor:       encryptor,
		registry:        registry,
		geoipClient:     geoip.NewClient(),
		capabilities:    agent.NewCapabilitiesCache(capabilityCacheTTL, capabilitiesFetchTimeout),
		configRedactor:  NewConfigRedactor(),
		prevSourceData:  make(map[string]*trafficSourceSnapshot),
		prevBackendData: make(map[string]*trafficBackendSnapshot),
	}
}

// capabilityCacheTTL is the freshness window for cached agent probe tables.
// Short enough that a restarted agent's new probe table shows up without
// operator intervention; long enough that the dashboard doesn't fire a
// fresh agent call on every page render. Reconnect events invalidate
// explicitly via Handler.invalidateBoxCapabilities for faster refresh.
const capabilityCacheTTL = 5 * time.Minute

// CapabilitiesCache exposes the dashboard's shared probe-table cache so
// other framework components (notably the ServerAdapter used by gear
// plugins) can answer "is gear X available on box Y?" without each
// holding its own cache. Returns the same *agent.CapabilitiesCache that
// the handler uses for filterGearsByAgentCapabilities, so callers see
// consistent capability data across the dashboard.
func (h *Handler) CapabilitiesCache() *agent.CapabilitiesCache {
	return h.capabilities
}

// getBoxCapabilities returns the cached capabilities for boxID, or fetches
// fresh ones if missing/stale. Returns (nil, false) when the box isn't
// configured or doesn't use the agent API. Errors are logged at debug —
// callers should fail open (show the full UI) when capabilities are
// unknown, the same way filterGearsByAgentCapabilities does.
func (h *Handler) getBoxCapabilities(boxID string) (*agent.BoxCapabilities, bool) {
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		return nil, false
	}
	caps, err := h.capabilities.Get(boxID, serverConfig.AgentURL, serverConfig.APIKey)
	if err != nil {
		h.logger.Debug("capabilities fetch failed",
			"box_id", boxID, "error", err)
		return nil, false
	}
	return caps, caps != nil
}

// invalidateBoxCapabilities drops the cached probe table for boxID so the
// next getBoxCapabilities call refetches. Wired to server.connected events
// by handler.go's main wiring so a restarted agent's new probe table is
// reflected immediately rather than at the next TTL boundary.
func (h *Handler) invalidateBoxCapabilities(boxID string) {
	if h.capabilities != nil {
		h.capabilities.Invalidate(boxID)
	}
}

// SetRegistry sets the collector registry for dynamic management.
func (h *Handler) SetRegistry(registry *collector.Registry) {
	h.registry = registry
}

// SetEncryptor sets the encryptor for handling secrets.
func (h *Handler) SetEncryptor(encryptor *crypto.Encryptor) {
	h.encryptor = encryptor
}

// SetWebAuthnManager sets the WebAuthn manager for passkey support.
func (h *Handler) SetWebAuthnManager(mgr *auth.WebAuthnManager) {
	h.webAuthnMgr = mgr
}

// SetEventHub sets the event hub for real-time updates and starts a
// background subscriber that invalidates the per-box capabilities cache
// whenever an agent (re)connects. This gives operators an immediate
// refresh after restarting an agent — without it, a restarted agent's
// new probe table would only surface at the next TTL boundary.
func (h *Handler) SetEventHub(hub *events.Hub) {
	h.eventHub = hub
	if hub != nil {
		go h.watchAgentReconnects(hub)
	}
}

// watchAgentReconnects subscribes to server.connected events and drops
// the cached capabilities for that box. Runs for the lifetime of the
// process; exits when the hub closes the subscription channel during
// shutdown.
func (h *Handler) watchAgentReconnects(hub *events.Hub) {
	sub := hub.Subscribe("capabilities-cache-invalidator", "")
	defer hub.Unsubscribe(sub)
	for evt := range sub.Events {
		if evt.Type != events.EventTypeServerConnected {
			continue
		}
		if evt.ServerID == "" {
			continue
		}
		h.invalidateBoxCapabilities(evt.ServerID)
	}
}

// SetWebSocketManager sets the WebSocket manager for Agent connections.
func (h *Handler) SetWebSocketManager(mgr *collector.WebSocketManager) {
	h.wsManager = mgr
}

// getWebAuthnManager returns the WebAuthn manager.
func (h *Handler) getWebAuthnManager() *auth.WebAuthnManager {
	return h.webAuthnMgr
}

// getCollector retrieves a collector for a specific server from the registry.
// This ensures we always get the current state (respecting enable/disable).
func (h *Handler) getCollector(boxID string) (*collector.Manager, bool) {
	if h.registry != nil {
		return h.registry.GetCollector(boxID)
	}
	// Fallback to static map if registry not available
	collector, exists := h.collectors[boxID]
	return collector, exists
}

// getServerConfig retrieves server config by ID.
// First checks the static list, then falls back to database lookup for newly created servers.
func (h *Handler) getServerConfig(boxID string) (*models.BoxConfig, bool) {
	// Check static list first (for backwards compatibility)
	for i := range h.servers {
		if h.servers[i].ID == boxID {
			return &h.servers[i], true
		}
	}

	// Fall back to database lookup for dynamically created servers
	servers := h.getEnabledServers()
	for i := range servers {
		if servers[i].ID == boxID {
			return &servers[i], true
		}
	}
	return nil, false
}

// getEnabledServers returns the list of currently enabled servers from the database.
// This ensures pages always reflect the current enable/disable state.
func (h *Handler) getEnabledServers() []models.BoxConfig {
	dbServers, err := h.db.GetEnabledBoxes()
	if err != nil {
		h.logger.Error("failed to get enabled servers", "error", err)
		return h.servers // Fallback to static list on error
	}

	encryptor, err := h.getEncryptor()
	if err != nil {
		h.logger.Error("failed to get encryptor", "error", err)
		return h.servers // Fallback to static list on error
	}

	var servers []models.BoxConfig
	for _, dbServer := range dbServers {
		apiKey, _ := encryptor.DecryptString(dbServer.APIKeyEncrypted)
		serverConfig := dbServer.ToBoxConfig(apiKey)
		if serverConfig.UsesAgentAPI() {
			servers = append(servers, serverConfig)
		}
	}
	return servers
}

// fullBoxRoster returns every configured box — including disabled and
// partially-configured ones — without the UsesAgentAPI() filter applied by
// getEnabledServers(). Used to publish the roster for the Bx fleet view +
// switcher chrome, where StatusGray rows are meaningful and the user needs
// to *see* the misconfigured boxes in order to fix them.
//
// API keys are intentionally not decrypted here — the roster is for UI
// rendering only; agents that need authenticated calls go through the
// existing per-request agent-client path which does its own decryption.
func (h *Handler) fullBoxRoster() []models.BoxConfig {
	dbBoxes, err := h.db.GetBoxes()
	if err != nil {
		h.logger.Error("failed to load full box roster", "error", err)
		return h.getEnabledServers() // safe fallback: at least show what works
	}
	out := make([]models.BoxConfig, 0, len(dbBoxes))
	for _, b := range dbBoxes {
		out = append(out, b.ToBoxConfig(""))
	}
	return out
}

// getDefaultServerID returns the ID of the first enabled server, or empty string if none.
func (h *Handler) getDefaultServerID() string {
	servers := h.getEnabledServers()
	if len(servers) > 0 {
		return servers[0].ID
	}
	return ""
}

// resolveBoxIDFromRequest picks the box ID to operate on, in priority:
//  1. `?server=<id>` or `?box_id=<id>` query param — explicit per-link
//     override. Both names accepted as synonyms so handlers can use
//     whichever convention the surrounding code prefers; the middleware
//     in InjectIntegrationStatus uses `?box_id=` for URL-persisted
//     pill switches, while older gear-settings links use `?server=`.
//  2. `gearbox_active_box` cookie — the header pill's selection.
//  3. The first enabled server — first-login fallback.
//
// Previously the Gears settings handlers only consulted `?server=` and fell
// straight through to the first server when it was missing, which made the
// page show the first box's gears even while the header pill was on a
// different box (issue #71 item 1). Reading the cookie aligns these
// handlers with the pill that's actually visible to the user. Recognizing
// `?box_id=` as a synonym keeps the box-resolver consistent across the
// dashboard's handler / middleware / gear-plugin layers (issue #112
// Phase 4).
func (h *Handler) resolveBoxIDFromRequest(r *http.Request) string {
	if id := r.URL.Query().Get("server"); id != "" {
		return id
	}
	if id := r.URL.Query().Get("box_id"); id != "" {
		return id
	}
	if c, err := r.Cookie(activeBoxCookieName); err == nil && c.Value != "" {
		// Only honor the cookie if it still resolves to an enabled server —
		// stale cookies (deleted/disabled boxes) shouldn't dictate behavior.
		for _, s := range h.getEnabledServers() {
			if s.ID == c.Value {
				return c.Value
			}
		}
	}
	return h.getDefaultServerID()
}

// resolveActiveBox returns the full BoxConfig for the box the request is
// acting on, using resolveBoxIDFromRequest for resolution. Returns
// (nil, false) when:
//
//   - There are no servers configured at all (resolveBoxIDFromRequest's
//     getDefaultServerID fallback has nothing to return), OR
//   - The resolved ID doesn't match any entry in the static
//     h.servers list or the database — e.g. a stale link with
//     ?server=<deleted-box-id>.
//
// Note: this does NOT return (nil, false) for an "all-boxes
// dashboard context". When at least one server is configured,
// resolveBoxIDFromRequest falls back to the first enabled box, and
// getServerConfig accepts entries from the static h.servers list
// whether or not they're DB-enabled, so any time at least one
// server exists this helper resolves to one. Handlers that need to
// distinguish "no active box" from "first enabled box" should
// consult the auth-context active-box set by
// InjectIntegrationStatus, not call this helper.
//
// Handlers that need an agent client, an agent URL, or other
// BoxConfig fields should prefer this over resolveBoxIDFromRequest +
// a separate getServerConfig call: it folds the existence check
// into one call site (issue #112 Phase 4).
func (h *Handler) resolveActiveBox(r *http.Request) (*models.BoxConfig, bool) {
	boxID := h.resolveBoxIDFromRequest(r)
	if boxID == "" {
		return nil, false
	}
	return h.getServerConfig(boxID)
}

// activeBoxCookieName is the cookie key that persists the user's selected
// box across navigations. Lets gear links (e.g. /haproxy) drop the verbose
// `?box_id=` query string and still resolve the active context.
const activeBoxCookieName = "gearbox_active_box"

// acceptsHTML reports whether the client appears to be requesting an HTML
// document (vs. an XHR/fetch JSON call). Used to scope the box_id-stripping
// redirect to navigations only.
func acceptsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	// Cheap substring check is fine — quality factors don't change the answer
	// for our use case (a navigation always advertises text/html very near
	// the front of the Accept list).
	for _, want := range []string{"text/html", "application/xhtml+xml"} {
		if strings.Contains(accept, want) {
			return true
		}
	}
	return false
}

// activeBoxCookieMaxAge is one year — long enough to feel persistent.
// Cleared explicitly via clearActiveBoxCookie when the user picks "All boxes".
const activeBoxCookieMaxAge = 60 * 60 * 24 * 365

// requestIsTLS reports whether the current request appears to be served
// over HTTPS, either directly (r.TLS != nil) or via a TLS-terminating
// proxy that forwards X-Forwarded-Proto=https (the HAProxy front-end in
// this homelab does exactly that). Used to gate the Secure cookie
// attribute so cookies are HTTPS-only in production but still work on
// plain http://localhost during development.
func requestIsTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

// setActiveBoxCookie writes the active-box id to an HttpOnly cookie scoped
// to the whole site. SameSite=Lax so cross-site navigations (share-links,
// bookmarks) still pick it up; HttpOnly so JS can't read it. Secure is
// set whenever the request is itself TLS — addresses CodeQL
// js/clear-text-cookie / go-cookie-not-secure.
func setActiveBoxCookie(w http.ResponseWriter, r *http.Request, boxID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     activeBoxCookieName,
		Value:    boxID,
		Path:     "/",
		MaxAge:   activeBoxCookieMaxAge,
		HttpOnly: true,
		Secure:   requestIsTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// clearActiveBoxCookie removes the persisted box selection. Triggered by
// `?box_id=` (empty value) or by visiting /bx (the fleet picker) — both
// signal the user wants the all-boxes context.
func clearActiveBoxCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     activeBoxCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// InjectIntegrationStatus is middleware that adds integration status, the
// active-box context, the enabled-box roster, and user permissions to the
// request context. This is what drives the sidebar's scope-aware rendering
// and the header's box-switcher chip.
//
// Active-box resolution (in priority order):
//  1. `?box_id=<id>` in the URL — explicit override. Also written to the
//     active-box cookie so subsequent navigations don't need the query
//     string. An empty `?box_id=` clears the cookie (used to deselect).
//  2. The `gearbox_active_box` cookie — sticky preference from a prior
//     selection. Subject to the same enabled-box validation as the URL.
//  3. None — "All boxes" / box-agnostic context. Sidebar hides ScopeBox
//     gears; the Bx fleet view becomes the entry point.
//
// System gears (keyed by database.SystemServerID) are loaded unconditionally
// because they are install-wide.
func (h *Handler) InjectIntegrationStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Inject user permissions into context for sidebar visibility
		perms, err := h.authManager.GetUserPermissions(r)
		if err == nil && perms != nil {
			ctx = auth.SetUserPermissions(ctx, perms)
		}

		// Publish the *full* configured-box roster so the header chip,
		// switcher palette, and Bx fleet view can render disabled or
		// partially-configured boxes (the Bx page's StatusGray semantic).
		// Active-box resolution below still uses the enabled+agent-API-using
		// subset — landing on a disabled box has no gears to show.
		fullRoster := h.fullBoxRoster()
		ctx = auth.SetAllBoxes(ctx, fullRoster)

		enabled := h.getEnabledServers()

		// Resolve the active box: URL takes precedence, cookie is fallback.
		// `?box_id=` with an empty value is the explicit "deselect" signal —
		// we honor it by clearing the cookie and skipping cookie fallback.
		var activeBox *models.BoxConfig
		var requestedID string
		urlHasBoxID := r.URL.Query().Has("box_id")
		_, hasCookie := r.Cookie(activeBoxCookieName)
		hasCookieSet := hasCookie == nil
		if urlHasBoxID {
			requestedID = r.URL.Query().Get("box_id")
		} else if c, err := r.Cookie(activeBoxCookieName); err == nil {
			requestedID = c.Value
		}
		if requestedID != "" {
			for i := range enabled {
				if enabled[i].ID == requestedID {
					activeBox = &enabled[i]
					break
				}
			}
		}
		// First-login fallback: if the request has no `?box_id=`, no
		// previously-set cookie, and the user is landing on a page that
		// benefits from a box context (i.e. not /bx, which means "show
		// all"), seed the active box from the first enabled entry. This
		// stops the sidebar from looking empty for users who haven't
		// explicitly picked a box yet — the most common cause of first-
		// login confusion.
		if activeBox == nil && !urlHasBoxID && !hasCookieSet && len(enabled) > 0 &&
			r.URL.Path != "/bx" && !strings.HasPrefix(r.URL.Path, "/bx/") {
			activeBox = &enabled[0]
			setActiveBoxCookie(w, r, activeBox.ID)
		}
		// Persist / clear the cookie based on what the URL signaled, then
		// redirect to the same path with `box_id` stripped so URLs stay clean.
		// Other query params are preserved (e.g. /logs?source=foo). Only
		// HTML document GETs get the redirect — XHR/fetch (`Accept` lacks
		// text/html) keep the param transparently so existing JS callers
		// that still pass `?box_id=` don't break.
		if urlHasBoxID && r.Method == http.MethodGet && acceptsHTML(r) {
			if activeBox != nil {
				setActiveBoxCookie(w, r, activeBox.ID)
			} else {
				clearActiveBoxCookie(w, r)
			}
			q := r.URL.Query()
			q.Del("box_id")
			redir := r.URL.Path
			if encoded := q.Encode(); encoded != "" {
				redir += "?" + encoded
			}
			http.Redirect(w, r, redir, http.StatusSeeOther)
			return
		}
		// Cookie still needs writing for non-HTML callers that explicitly
		// passed `?box_id=` (e.g. an early SPA-style call) so the next
		// document GET doesn't have to re-resolve.
		if urlHasBoxID {
			if activeBox != nil {
				setActiveBoxCookie(w, r, activeBox.ID)
			} else {
				clearActiveBoxCookie(w, r)
			}
		}
		if r.URL.Path == "/bx" || r.URL.Path == "/bx/" {
			// /bx is the all-boxes view — clear any sticky selection so
			// the chip reads "All boxes" and the sidebar hides box-scoped
			// gears. Clear whenever the cookie is present, not just when
			// it resolved to an enabled box: otherwise a stale cookie
			// (referencing a deleted/disabled box) survives indefinitely
			// AND blocks the first-login auto-select branch below
			// because hasCookieSet stays true.
			if hasCookieSet {
				clearActiveBoxCookie(w, r)
			}
			activeBox = nil
		}
		if activeBox != nil {
			ctx = auth.SetSelectedBox(ctx, activeBox)
		}

		status := make(map[string]bool)
		orderedIntegrations := make([]auth.SidebarIntegration, 0)

		// System / box-agnostic gears go first so they render at the head of the nav.
		systemGears, err := h.db.GetGears(database.SystemServerID)
		if err != nil {
			h.logger.Warn("failed to load system gears for sidebar", "error", err)
		}
		for _, sg := range systemGears {
			status[sg.Name] = sg.Enabled
			orderedIntegrations = append(orderedIntegrations, auth.SidebarIntegration{
				Name:      sg.Name,
				Enabled:   sg.Enabled,
				SortOrder: sg.SortOrder,
			})
		}

		if activeBox != nil {
			integrations, err := h.db.GetGears(activeBox.ID)
			if err != nil {
				// Fail-open: a partial gear list could collapse the sidebar
				// to system-gears-only, hiding box features the user
				// actually has. Leave the gear-status/order context unset
				// so OrderedIntegrationLinks falls back to its default
				// (full) rendering branch.
				h.logger.Error("failed to get box integrations for sidebar", "error", err, "box_id", activeBox.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// Filter the per-box gear list by the agent's probe table —
			// if the agent reports a gear as unavailable, hide it from
			// the sidebar entirely so the operator doesn't click into a
			// page the box physically can't serve (issue #112). The
			// filter fails OPEN: when the agent is unreachable the full
			// list is preserved, matching the Gears-settings-page filter.
			integrations = h.filterGearsByAgentCapabilities(activeBox.ID, integrations)
			for _, i := range integrations {
				status[i.Name] = i.Enabled
				orderedIntegrations = append(orderedIntegrations, auth.SidebarIntegration{
					Name:      i.Name,
					Enabled:   i.Enabled,
					SortOrder: i.SortOrder,
				})
			}
		} else {
			// No box selected — explicitly mark box-scoped gears off so the
			// sidebar renderer hides them. ScopeBoxAgnostic gears (Bx, etc.)
			// and ScopeSystem gears (Home) are injected above as system
			// rows and remain visible.
			for _, n := range []string{"haproxy", "metrics", "logs", "services", "certificates", "traffic", "alerts", "os_updates"} {
				if _, present := status[n]; !present {
					status[n] = false
				}
			}
		}

		ctx = auth.SetGearStatus(ctx, status)
		ctx = auth.SetIntegrationOrder(ctx, orderedIntegrations)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
