package handler

import (
	"net/http"
	"net/url"

	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

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

	// If admin user and no HAProxy servers configured, redirect to server setup
	if user.Role == models.RoleAdmin {
		count, err := h.db.CountEnabledServers()
		if err != nil {
			h.logger.Error("Failed to count HAProxy servers", "error", err)
		} else if count == 0 {
			h.logger.Info("admin user logged in but no HAProxy servers configured, redirecting to setup", "email", email)
			http.Redirect(w, r, "/settings/servers/new", http.StatusSeeOther)
			return
		}
	}

	// Redirect to return URL if provided, otherwise to home
	redirectTarget := "/"
	if returnURL != "" && returnURL != "/login" && returnURL != "/logout" {
		redirectTarget = returnURL
	}
	http.Redirect(w, r, redirectTarget, http.StatusSeeOther)
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

	// Build redirect URL with logout message
	redirectURL := "/login?message=" + url.QueryEscape("You have been logged out.")
	if returnURL != "" && returnURL != "/" && returnURL != "/login" && returnURL != "/logout" {
		redirectURL += "&return=" + url.QueryEscape(returnURL)
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
