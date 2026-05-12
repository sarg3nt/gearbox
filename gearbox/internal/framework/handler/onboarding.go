package handler

import (
	"net/http"
	"net/url"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// WelcomePage renders the first-run onboarding screen. Issue #49.
//
// Shown only while nothing has been configured — no enabled boxes and no
// enabled system gears. Once the admin makes any choice, the welcome screen
// is no longer the landing fallback and direct visits redirect away.
func (h *Handler) WelcomePage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if h.onboardingComplete() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	systemGears, err := h.db.GetGears(database.SystemServerID)
	if err != nil {
		h.logger.Warn("failed to load system gears for welcome page", "error", err)
		systemGears = nil
	}
	// Only offer gears the admin hasn't already enabled. Onboarding is
	// one-shot; if a gear is enabled the page wouldn't be rendered at all
	// (see onboardingComplete above), so this filter is mostly defensive.
	disabled := make([]database.Gear, 0, len(systemGears))
	for _, g := range systemGears {
		if !g.Enabled {
			disabled = append(disabled, g)
		}
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")

	component := pages.WelcomePage(user, disabled, csrfToken, errorMessage)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render welcome template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// OnboardingPost handles the welcome form. Two submit buttons share one form:
//
//   - action=add-box  → enable any checked system gears, then redirect to
//     /settings/boxes/new
//   - action=skip     → enable any checked system gears, then redirect to /
//     and let RootRedirect's fallback chain pick the landing page
//
// If nothing is checked and the admin clicked "skip", the redirect lands back
// on /welcome via RootRedirect (no decision made, no-op).
func (h *Handler) OnboardingPost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := h.authManager.ValidateCSRFToken(r); err != nil {
		http.Redirect(w, r, "/welcome?error="+url.QueryEscape("Invalid CSRF token"), http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/welcome?error="+url.QueryEscape("Invalid form submission"), http.StatusSeeOther)
		return
	}

	action := r.FormValue("action")
	checked := r.Form["gears"]

	// Only enable gears that are actually system-scoped — defence in depth
	// against form tampering.
	for _, name := range checked {
		if !database.IsSystemGear(name) {
			h.logger.Warn("onboarding ignored non-system gear", "name", name, "user", user.ID)
			continue
		}
		if err := h.db.SetGearEnabled(database.SystemServerID, name, true, &user.ID); err != nil {
			h.logger.Error("failed to enable system gear during onboarding",
				"name", name, "error", err, "user", user.ID)
			http.Redirect(w, r, "/welcome?error="+url.QueryEscape("Failed to enable selected tools"), http.StatusSeeOther)
			return
		}
		h.logger.Info("onboarding enabled system gear", "name", name, "user", user.ID)
	}

	if action == "add-box" {
		http.Redirect(w, r, "/settings/boxes/new", http.StatusSeeOther)
		return
	}
	// action == "skip" (or unknown) — let RootRedirect's chain decide.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// onboardingComplete reports whether the admin has finished onboarding —
// at least one box is enabled, or at least one system-scoped gear is
// enabled. Used as the one-shot guard for the welcome screen.
func (h *Handler) onboardingComplete() bool {
	if count, err := h.db.CountEnabledBoxes(); err == nil && count > 0 {
		return true
	}
	if anyEnabled, err := h.anySystemGearEnabled(); err == nil && anyEnabled {
		return true
	}
	return false
}

// anySystemGearEnabled reports whether any box-agnostic gear is enabled on
// the system row.
func (h *Handler) anySystemGearEnabled() (bool, error) {
	gears, err := h.db.GetGears(database.SystemServerID)
	if err != nil {
		return false, err
	}
	for _, g := range gears {
		if g.Enabled {
			return true, nil
		}
	}
	return false, nil
}
