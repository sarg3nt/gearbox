package handler

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// isSafeReturnURL reports whether a `?return=` (or `?next=`) query value is
// safe to redirect the browser to after login/logout. Only same-origin
// relative paths are allowed — anything that could escape to an external host
// is rejected so the login page can't be used as a phishing redirector.
//
// The accepted shape is exactly: starts with `/`, does NOT start with `//`
// (protocol-relative), does NOT start with `/\` (some browsers normalize this
// to `//` and follow it as protocol-relative), and does not contain a
// scheme-like prefix elsewhere. Backslashes anywhere in the value are
// rejected because some browsers normalize them to forward slashes during
// URL parsing, opening sneak paths like `/\/evil.example.com`.
//
// 2026-05 audit P0-4.
func isSafeReturnURL(s string) bool {
	if s == "" {
		return false
	}
	if len(s) < 1 || s[0] != '/' {
		return false
	}
	if len(s) >= 2 && (s[1] == '/' || s[1] == '\\') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' {
			return false
		}
		// Reject ASCII control characters (including \r, \n, \t, NUL,
		// DEL). Paths containing these are not valid URLs and have been
		// used in header-injection / open-redirect sneak paths. 2026-05
		// audit P0-4 follow-up (Copilot review on PR #39).
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// resolvePostLoginPath returns the path the user should land on after login
// or after visiting "/" without a destination. Cascade:
//
//  1. The user's default_landing_path (if set).
//  2. The home gear's system_default_landing_path config (if set and the home gear is enabled).
//  3. "/" (caller decides what that means).
//
// Empty userID skips step 1. Errors are logged by the caller via the returned target;
// this function is intentionally fail-safe — it returns "/" rather than erroring.
func (h *Handler) resolvePostLoginPath(userID string) string {
	if userID != "" {
		if path, err := h.db.GetUserDefaultLandingPath(userID); err == nil && path != "" {
			return path
		}
	}

	homeGear, err := h.db.GetGear(database.SystemServerID, database.GearHome)
	if err != nil || homeGear == nil || !homeGear.Enabled {
		return "/"
	}

	var cfg database.HomeConfig
	if err := json.Unmarshal(homeGear.Config, &cfg); err != nil {
		return "/"
	}
	if cfg.SystemDefaultLandingPath != "" {
		return cfg.SystemDefaultLandingPath
	}
	return "/"
}

// LoginPage serves the login page.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// Check if already authenticated
	if h.authManager.IsAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Get CSRF token for the form (empty for now, will be validated in POST)
	csrfToken, _ := h.authManager.GetCSRFToken(r)

	// Get error message from query params
	errorMessage := r.URL.Query().Get("error")

	// Get success message (e.g., after password reset)
	successMessage := r.URL.Query().Get("success")

	// Also check for generic 'message' param (used for session timeout, etc.)
	if successMessage == "" {
		successMessage = r.URL.Query().Get("message")
	}

	// Get return URL from query params
	returnURL := r.URL.Query().Get("return")

	// Render login template
	component := pages.Login(errorMessage, successMessage, csrfToken, returnURL)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render login template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// LoginPost handles login form submission.
func (h *Handler) LoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+request", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	returnURL := r.FormValue("return")

	// Validate field lengths
	if len(email) > 254 || len(password) > 128 {
		http.Redirect(w, r, "/login?error=Invalid+credentials", http.StatusSeeOther)
		return
	}

	// Attempt login
	user, err := h.authManager.Login(w, r, email, password)
	if err != nil {
		h.logger.Warn("login failed for user", "email", email, "error", err)
		redirectURL := "/login?error=" + url.QueryEscape(err.Error())
		if returnURL != "" {
			redirectURL += "&return=" + url.QueryEscape(returnURL)
		}
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}

	h.logger.Info("user logged in successfully", "email", email)

	// Check if user must change password
	if user.MustChangePassword {
		http.Redirect(w, r, "/settings/change-password?forced=true", http.StatusSeeOther)
		return
	}

	// First-run onboarding is no longer forced at login time. If nothing is
	// configured, the user's resolved landing path will be "/", which
	// RootRedirect routes to /welcome. Issue #49.

	// Redirect to return URL if provided, otherwise honor the per-user
	// default-landing-path with system fallback. The return URL must be a
	// same-origin relative path — anything else (an absolute URL, a
	// protocol-relative URL, a `javascript:` URI) is rejected to prevent
	// the login page from being used as a phishing redirector
	// (2026-05 audit P0-4).
	redirectTarget := h.resolvePostLoginPath(user.ID)
	if isSafeReturnURL(returnURL) && returnURL != "/login" && returnURL != "/logout" {
		redirectTarget = returnURL
	}
	http.Redirect(w, r, redirectTarget, http.StatusSeeOther)
}

// RootRedirect handles GET /. Redirects authenticated users to their
// default-landing-path (per-user → system → fallback). When nothing is
// configured the fallback chain is:
//
//  1. Any box enabled            → /haproxy
//  2. Any system gear enabled    → /home (today the only system gear)
//  3. Otherwise                  → /welcome (first-run onboarding, issue #49)
func (h *Handler) RootRedirect(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if u, err := h.authManager.GetUser(r); err == nil && u != nil {
		userID = u.ID
	}

	target := h.resolvePostLoginPath(userID)
	if target != "/" {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}

	if count, err := h.db.CountEnabledBoxes(); err == nil && count > 0 {
		http.Redirect(w, r, "/haproxy", http.StatusSeeOther)
		return
	}
	if homeGear, err := h.db.GetGear(database.SystemServerID, database.GearHome); err == nil && homeGear != nil && homeGear.Enabled {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/welcome", http.StatusSeeOther)
}

// Logout handles user logout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.authManager.Logout(w, r); err != nil {
		h.logger.Error("Logout error", "error", err)
	}

	// Get the return URL from the request (if provided via query or referer)
	returnURL := r.URL.Query().Get("return")
	if returnURL == "" {
		// Try to use the referer header as the return URL
		referer := r.Header.Get("Referer")
		if referer != "" {
			// Parse the referer to extract just the path
			if refererURL, err := url.Parse(referer); err == nil {
				returnURL = refererURL.Path
				if refererURL.RawQuery != "" {
					returnURL += "?" + refererURL.RawQuery
				}
			}
		}
	}

	// Build redirect URL with logout message. The return URL must pass the
	// same safety check as the post-login redirect (2026-05 audit P0-4) so
	// the logout page can't be used to inject an attacker-controlled
	// `?return=...` into the login page's URL.
	redirectURL := "/login?message=" + url.QueryEscape("You have been logged out.")
	if isSafeReturnURL(returnURL) && returnURL != "/" && returnURL != "/login" && returnURL != "/logout" {
		redirectURL += "&return=" + url.QueryEscape(returnURL)
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
