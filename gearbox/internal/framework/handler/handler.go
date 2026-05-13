package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/collector"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/services/email"
	"github.com/sarg3nt/gearbox/internal/framework/services/geoip"
	"github.com/sarg3nt/gearbox/internal/framework/models"
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
		configRedactor:  NewConfigRedactor(),
		prevSourceData:  make(map[string]*trafficSourceSnapshot),
		prevBackendData: make(map[string]*trafficBackendSnapshot),
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

// SetEventHub sets the event hub for real-time updates.
func (h *Handler) SetEventHub(hub *events.Hub) {
	h.eventHub = hub
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
