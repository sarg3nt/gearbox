package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	apperrors "github.com/sarg3nt/webcore/core/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// RequestAccountPage serves the account request form.
func (h *Handler) RequestAccountPage(w http.ResponseWriter, r *http.Request) {
	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")

	component := pages.RequestAccount(errorMessage, csrfToken, auth.GetPasswordRequirements())
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render request account template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// RequestAccountPost handles account request submission.
func (h *Handler) RequestAccountPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/request-account?error=Invalid+request", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	firstName := r.FormValue("first_name")
	lastName := r.FormValue("last_name")
	phoneNumber := r.FormValue("phone_number")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")
	reason := r.FormValue("reason")

	// Validate field lengths
	if len(firstName) > 100 || len(lastName) > 100 || len(email) > 254 || len(phoneNumber) > 20 || len(password) > 128 || len(reason) > 500 {
		http.Redirect(w, r, "/request-account?error=One+or+more+fields+exceed+maximum+length", http.StatusSeeOther)
		return
	}

	// Validate email
	if err := auth.ValidateEmail(email); err != nil {
		http.Redirect(w, r, "/request-account?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Validate passwords match
	if password != confirmPassword {
		http.Redirect(w, r, "/request-account?error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	// Validate password policy
	if err := auth.ValidatePassword(password); err != nil {
		if pve, ok := err.(*auth.PasswordValidationError); ok {
			http.Redirect(w, r, "/request-account?error="+url.QueryEscape(pve.Errors[0]), http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/request-account?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		}
		return
	}

	// Hash password
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		h.logger.Error("Failed to hash password", "error", err)
		http.Redirect(w, r, "/request-account?error=Internal+error", http.StatusSeeOther)
		return
	}

	// Create account request
	req := &models.AccountRequest{
		Email:       email,
		FirstName:   firstName,
		LastName:    lastName,
		PhoneNumber: phoneNumber,
		Reason:      reason,
	}

	_, err = h.authManager.GetDB().CreateAccountRequest(req, passwordHash)
	if err != nil {
		h.logger.Error("Failed to create account request", "error", err)
		http.Redirect(w, r, "/request-account?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Notify admins and users with approve permission via email if configured
	if h.emailService != nil && h.emailService.IsConfigured() {
		// Get users who can approve (admins + users with approve permission)
		approvers, _ := h.authManager.GetDB().GetUsersWithPermission(models.ComponentUsers, models.PermissionApproveUsers)
		for _, approver := range approvers {
			_ = h.emailService.SendNewAccountRequestEmail(approver.Email, req)
		}
	}

	h.logger.Info("new account request submitted", "email", email)
	http.Redirect(w, r, "/request-account/success", http.StatusSeeOther)
}

// RequestAccountSuccessPage shows success message after account request.
func (h *Handler) RequestAccountSuccessPage(w http.ResponseWriter, r *http.Request) {
	component := pages.RequestAccountSuccess()
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render request account success template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ForgotPasswordPage serves the forgot password form.
func (h *Handler) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	component := pages.ForgotPassword(errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render forgot password template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ForgotPasswordPost handles password reset request.
func (h *Handler) ForgotPasswordPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/forgot-password?error=Invalid+request", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	if len(email) > 254 {
		http.Redirect(w, r, "/forgot-password?error=Email+exceeds+maximum+length", http.StatusSeeOther)
		return
	}

	// Request password reset (this won't reveal if user exists)
	token, user, err := h.authManager.RequestPasswordReset(email)
	if err != nil {
		h.logger.Error("password reset request failed", "email", email, "error", err)
	}

	// If user exists and email is configured, send reset email
	if user != nil && token != "" && h.emailService != nil && h.emailService.IsConfigured() {
		h.logger.Info("sending password reset email", "requested_email", email, "user_email", user.Email)
		if err := h.emailService.SendPasswordResetEmail(user, token); err != nil {
			h.logger.Error("failed to send password reset email", "email", user.Email, "error", err)
		} else {
			h.logger.Info("password reset email sent successfully", "email", user.Email)
		}
	} else if user == nil {
		h.logger.Info("password reset requested but no user found", "email", email)
	} else if token == "" {
		h.logger.Warn("password reset requested but no token generated", "email", email)
	} else {
		h.logger.Warn("password reset requested but email service not configured", "email", email, "is_configured", h.emailService != nil && h.emailService.IsConfigured())
	}

	// Always show success message to prevent email enumeration
	http.Redirect(w, r, "/forgot-password?success=If+an+account+exists+with+that+email,+you+will+receive+a+password+reset+link", http.StatusSeeOther)
}

// ResetPasswordPage serves the password reset form.
func (h *Handler) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, "/login?error=Invalid+reset+link", http.StatusSeeOther)
		return
	}

	// Verify token is valid
	user, err := h.authManager.GetDB().GetUserByResetToken(token)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login?error=Invalid+or+expired+reset+link", http.StatusSeeOther)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")

	component := pages.ResetPassword(token, errorMessage, csrfToken, auth.GetPasswordRequirements())
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render reset password template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ResetPasswordPost handles password reset submission.
func (h *Handler) ResetPasswordPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+request", http.StatusSeeOther)
		return
	}

	token := r.FormValue("token")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if len(password) > 128 {
		http.Redirect(w, r, "/reset-password?token="+url.QueryEscape(token)+"&error=Password+exceeds+maximum+length", http.StatusSeeOther)
		return
	}

	if password != confirmPassword {
		http.Redirect(w, r, "/reset-password?token="+url.QueryEscape(token)+"&error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	if err := h.authManager.ResetPassword(r, token, password); err != nil {
		if pve, ok := err.(*auth.PasswordValidationError); ok {
			http.Redirect(w, r, "/reset-password?token="+url.QueryEscape(token)+"&error="+url.QueryEscape(pve.Errors[0]), http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/reset-password?token="+url.QueryEscape(token)+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		}
		return
	}

	http.Redirect(w, r, "/login?success=Password+reset+successfully.+Please+log+in.", http.StatusSeeOther)
}

// CompleteAccountSetupPage serves the account setup form (for first login).
func (h *Handler) CompleteAccountSetupPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")

	component := pages.CompleteAccountSetup(user, errorMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render complete account setup template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ChangePasswordPage serves the change password form.
func (h *Handler) ChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")

	component := pages.ChangePassword(user, errorMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render change password template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// CompleteAccountSetupPost handles account setup submission on first login.
func (h *Handler) CompleteAccountSetupPost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/complete-account-setup?error=Invalid+request", http.StatusSeeOther)
		return
	}

	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")
	newEmail := strings.TrimSpace(r.FormValue("email"))
	firstName := strings.TrimSpace(r.FormValue("first_name"))
	lastName := strings.TrimSpace(r.FormValue("last_name"))
	phoneNumber := strings.TrimSpace(r.FormValue("phone_number"))

	// Validate field lengths
	if len(firstName) > 100 || len(lastName) > 100 || len(newEmail) > 254 || len(phoneNumber) > 20 || len(newPassword) > 128 {
		http.Redirect(w, r, "/settings/complete-account-setup?error=One+or+more+fields+exceed+maximum+length", http.StatusSeeOther)
		return
	}

	// Validate password match
	if newPassword != confirmPassword {
		http.Redirect(w, r, "/settings/complete-account-setup?error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	// Validate email is provided
	if newEmail == "" {
		http.Redirect(w, r, "/settings/complete-account-setup?error=Email+is+required", http.StatusSeeOther)
		return
	}

	// Check if email is already in use by another user
	existingUser, err := h.authManager.GetDB().GetUserByEmail(newEmail)
	if err != nil {
		h.logger.Error("Failed to check email uniqueness", "error", err)
		http.Redirect(w, r, "/settings/complete-account-setup?error=Failed+to+validate+email", http.StatusSeeOther)
		return
	}
	if existingUser != nil && existingUser.ID != user.ID {
		http.Redirect(w, r, "/settings/complete-account-setup?error=Email+is+already+in+use", http.StatusSeeOther)
		return
	}

	// Update profile information
	if err := h.authManager.GetDB().UpdateUserProfile(user.ID, firstName, lastName, phoneNumber); err != nil {
		h.logger.Error("Failed to update user profile", "error", err)
		http.Redirect(w, r, "/settings/complete-account-setup?error=Failed+to+update+profile", http.StatusSeeOther)
		return
	}

	// Set password and email (doesn't require current password)
	if err := h.authManager.SetPasswordAndEmail(r, user.ID, newPassword, newEmail); err != nil {
		http.Redirect(w, r, "/settings/complete-account-setup?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// SECURITY: Auto-delete admin credentials file after successful setup
	credentialsFile := "data/admin-credentials.txt" //#nosec G101 -- Not a credential; this is a file path constant
	if _, err := os.Stat(credentialsFile); err == nil {
		if err := os.Remove(credentialsFile); err != nil {
			h.logger.Warn("failed to delete admin credentials file after account setup",
				"error", err,
				"file", credentialsFile)
		} else {
			h.logger.Info("admin credentials file deleted after account setup",
				"user_id", user.ID,
				"file", credentialsFile)
		}
	}

	// Get updated user record (with new email and cleared MustChangePassword flag)
	updatedUser, err := h.authManager.GetDB().GetUserByID(user.ID)
	if err != nil {
		h.logger.Error("Failed to get updated user after account setup", "error", err)
		http.Redirect(w, r, "/settings/complete-account-setup?error=Setup+completed+but+session+error", http.StatusSeeOther)
		return
	}

	// Create a fresh session with the updated user
	if err := h.authManager.CreateSessionForUser(w, r, updatedUser); err != nil {
		h.logger.Error("failed to create session after account setup", "error", err)
		http.Redirect(w, r, "/login?message=Setup+complete.+Please+log+in.", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ChangePasswordPost handles password change submission.
func (h *Handler) ChangePasswordPost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/change-password?error=Invalid+request", http.StatusSeeOther)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if len(currentPassword) > 128 || len(newPassword) > 128 {
		http.Redirect(w, r, "/settings/change-password?error=Password+exceeds+maximum+length", http.StatusSeeOther)
		return
	}

	if newPassword != confirmPassword {
		http.Redirect(w, r, "/settings/change-password?error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	if err := h.authManager.ChangePassword(r, user.ID, currentPassword, newPassword); err != nil {
		http.Redirect(w, r, "/settings/change-password?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings?message=Password+changed+successfully", http.StatusSeeOther)
}

// ProfilePage serves the user profile page.
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	// Get user's passkeys
	passkeys, _ := h.authManager.GetDB().GetUserPasskeys(user.ID)

	component := pages.Profile(user, passkeys, errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render profile template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ProfileUpdatePost handles profile update submission.
func (h *Handler) ProfileUpdatePost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/profile?error=Invalid+request", http.StatusSeeOther)
		return
	}

	newEmail := strings.TrimSpace(r.FormValue("email"))
	oldEmail := user.Email

	// Validate field lengths
	if len(r.FormValue("first_name")) > 100 || len(r.FormValue("last_name")) > 100 || len(newEmail) > 254 || len(r.FormValue("phone_number")) > 20 {
		http.Redirect(w, r, "/settings/profile?error=One+or+more+fields+exceed+maximum+length", http.StatusSeeOther)
		return
	}

	// Validate email if it changed
	if newEmail != oldEmail {
		if newEmail == "" {
			http.Redirect(w, r, "/settings/profile?error=Email+is+required", http.StatusSeeOther)
			return
		}

		// Check if email is already in use by another user
		existingUser, err := h.authManager.GetDB().GetUserByEmail(newEmail)
		if err != nil {
			h.logger.Error("Failed to check email uniqueness", "error", err)
			http.Redirect(w, r, "/settings/profile?error=Failed+to+validate+email", http.StatusSeeOther)
			return
		}
		if existingUser != nil && existingUser.ID != user.ID {
			http.Redirect(w, r, "/settings/profile?error=Email+is+already+in+use", http.StatusSeeOther)
			return
		}

		user.Email = newEmail
	}

	user.FirstName = r.FormValue("first_name")
	user.LastName = r.FormValue("last_name")
	user.PhoneNumber = r.FormValue("phone_number")

	if err := h.authManager.GetDB().UpdateUser(user); err != nil {
		http.Redirect(w, r, "/settings/profile?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Log with details if email changed
	if newEmail != oldEmail {
		h.authManager.LogAudit(r, &user.ID, models.AuditActionProfileUpdated, fmt.Sprintf("email changed from %q to %q", oldEmail, newEmail))
	} else {
		h.authManager.LogAudit(r, &user.ID, models.AuditActionProfileUpdated, "")
	}
	http.Redirect(w, r, "/settings/profile?success=Profile+updated+successfully", http.StatusSeeOther)
}

// AdminUsersPage serves the admin user management page.
func (h *Handler) AdminUsersPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to manage users
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !user.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	// Get all users
	users, _ := h.authManager.GetDB().GetAllUsers()

	// Get pending account requests
	pendingRequests, _ := h.authManager.GetDB().GetPendingAccountRequests()

	component := pages.AdminUsers(user, users, pendingRequests, errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render admin users template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// AdminApproveUserPost handles approving an account request.
func (h *Handler) AdminApproveUserPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to approve users
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !admin.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+request", http.StatusSeeOther)
		return
	}

	requestID, err := strconv.ParseInt(r.FormValue("request_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+request+ID", http.StatusSeeOther)
		return
	}

	newUser, err := h.authManager.GetDB().ApproveAccountRequest(requestID, admin.ID)
	if err != nil {
		http.Redirect(w, r, "/settings/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.authManager.LogAudit(r, &admin.ID, models.AuditActionAccountApproved, "approved user: "+newUser.Email)

	// Send approval email
	if h.emailService != nil && h.emailService.IsConfigured() {
		_ = h.emailService.SendAccountApprovedEmail(newUser)
	}

	// Redirect to permissions page to set up user permissions
	successMsg := url.QueryEscape(fmt.Sprintf("Account approved for %s. Set up permissions below.", newUser.Email))
	http.Redirect(w, r, fmt.Sprintf("/settings/permissions/%s?success=%s", newUser.ID, successMsg), http.StatusSeeOther)
}

// AdminDenyUserPost handles denying an account request.
func (h *Handler) AdminDenyUserPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to approve users (includes denying)
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !admin.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+request", http.StatusSeeOther)
		return
	}

	requestID, err := strconv.ParseInt(r.FormValue("request_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+request+ID", http.StatusSeeOther)
		return
	}

	reason := r.FormValue("deny_reason")

	// Get request info before denying
	req, _, _ := h.authManager.GetDB().GetAccountRequest(requestID)

	if err := h.authManager.GetDB().DenyAccountRequest(requestID, admin.ID, reason); err != nil {
		http.Redirect(w, r, "/settings/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Log audit and send denial email only if request was found
	if req != nil {
		h.authManager.LogAudit(r, &admin.ID, models.AuditActionAccountDenied, "denied request: "+req.Email)

		// Send denial email
		if h.emailService != nil && h.emailService.IsConfigured() {
			_ = h.emailService.SendAccountDeniedEmail(req.Email, req.FirstName, reason)
		}
	}

	http.Redirect(w, r, "/settings/users?success=Account+request+denied", http.StatusSeeOther)
}

// AdminToggleUserPost handles enabling/disabling a user.
func (h *Handler) AdminToggleUserPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+request", http.StatusSeeOther)
		return
	}

	userID := r.FormValue("user_id")
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	action := r.FormValue("action") // "enable" or "disable"

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Prevent disabling yourself
	if targetUser.ID == admin.ID {
		http.Redirect(w, r, "/settings/users?error=Cannot+disable+your+own+account", http.StatusSeeOther)
		return
	}

	if action == "enable" {
		targetUser.Status = models.UserStatusActive
		h.authManager.LogAudit(r, &admin.ID, models.AuditActionAccountEnabled, "enabled user: "+targetUser.Email)
	} else {
		targetUser.Status = models.UserStatusDisabled
		h.authManager.LogAudit(r, &admin.ID, models.AuditActionAccountDisabled, "disabled user: "+targetUser.Email)
	}

	if err := h.authManager.GetDB().UpdateUser(targetUser); err != nil {
		http.Redirect(w, r, "/settings/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings/users?success=User+status+updated", http.StatusSeeOther)
}

// AdminChangeUserRolePost handles changing a user's role.
func (h *Handler) AdminChangeUserRolePost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+request", http.StatusSeeOther)
		return
	}

	userID := r.FormValue("user_id")
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	role := models.UserRole(r.FormValue("role"))
	if role != models.RoleAdmin && role != models.RoleReadOnly {
		http.Redirect(w, r, "/settings/users?error=Invalid+role", http.StatusSeeOther)
		return
	}

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	targetUser.Role = role

	if err := h.authManager.GetDB().UpdateUser(targetUser); err != nil {
		http.Redirect(w, r, "/settings/users?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.authManager.LogAudit(r, &admin.ID, "role_changed", "changed role for "+targetUser.Email+" to "+string(role))
	http.Redirect(w, r, "/settings/users?success=User+role+updated", http.StatusSeeOther)
}

// AdminUserDetailPage serves the user detail/edit page.
func (h *Handler) AdminUserDetailPage(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	component := pages.AdminUserDetail(admin, targetUser, errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render user detail template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// AdminUpdateUserPost handles updating a user's information.
func (h *Handler) AdminUpdateUserPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Invalid+request", userID), http.StatusSeeOther)
		return
	}

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Validate field lengths
	if len(r.FormValue("first_name")) > 100 || len(r.FormValue("last_name")) > 100 || len(r.FormValue("email")) > 254 || len(r.FormValue("phone_number")) > 20 {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=One+or+more+fields+exceed+maximum+length", userID), http.StatusSeeOther)
		return
	}

	// Update fields from form
	newEmail := strings.TrimSpace(r.FormValue("email"))
	if newEmail == "" {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Email+is+required", userID), http.StatusSeeOther)
		return
	}

	// Check if email is already in use by another user
	if newEmail != targetUser.Email {
		existingUser, err := h.authManager.GetDB().GetUserByEmail(newEmail)
		if err != nil {
			h.logger.Error("Failed to check email uniqueness", "error", err)
			http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Failed+to+validate+email", userID), http.StatusSeeOther)
			return
		}
		if existingUser != nil && existingUser.ID != targetUser.ID {
			http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Email+is+already+in+use", userID), http.StatusSeeOther)
			return
		}
	}

	// Validate role
	role := models.UserRole(r.FormValue("role"))
	if role != models.RoleAdmin && role != models.RoleReadOnly {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Invalid+role", userID), http.StatusSeeOther)
		return
	}

	// Validate status
	status := models.UserStatus(r.FormValue("status"))
	if status != models.UserStatusActive && status != models.UserStatusDisabled {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Invalid+status", userID), http.StatusSeeOther)
		return
	}

	// Build list of changes for audit log
	var changes []string
	if targetUser.Email != newEmail {
		changes = append(changes, fmt.Sprintf("email: %s -> %s", targetUser.Email, newEmail))
	}
	if targetUser.Role != role {
		changes = append(changes, fmt.Sprintf("role: %s -> %s", targetUser.Role, role))
	}
	if targetUser.Status != status {
		changes = append(changes, fmt.Sprintf("status: %s -> %s", targetUser.Status, status))
	}

	// Update user
	targetUser.Email = newEmail
	targetUser.FirstName = r.FormValue("first_name")
	targetUser.LastName = r.FormValue("last_name")
	targetUser.PhoneNumber = r.FormValue("phone_number")
	targetUser.Role = role
	targetUser.Status = status

	if err := h.authManager.GetDB().UpdateUser(targetUser); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=%s", userID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}

	// Log audit if there were significant changes
	if len(changes) > 0 {
		h.authManager.LogAudit(r, &admin.ID, "user_updated", fmt.Sprintf("updated user %s: %s", targetUser.Email, strings.Join(changes, ", ")))
	}

	http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?success=User+updated+successfully", userID), http.StatusSeeOther)
}

// AdminForcePasswordResetPost forces a user to reset their password on next login.
func (h *Handler) AdminForcePasswordResetPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Cannot force password reset on yourself
	if targetUser.ID == admin.ID {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Cannot+force+password+reset+on+yourself", userID), http.StatusSeeOther)
		return
	}

	targetUser.MustChangePassword = true
	if err := h.authManager.GetDB().UpdateUser(targetUser); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=%s", userID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}

	h.authManager.LogAudit(r, &admin.ID, "password_reset_forced", "forced password reset for "+targetUser.Email)
	http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?success=Password+reset+required+on+next+login", userID), http.StatusSeeOther)
}

// AdminDeleteUserPost handles deleting a user.
func (h *Handler) AdminDeleteUserPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if err != nil {
		http.Redirect(w, r, "/settings/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	// Cannot delete yourself
	if userID == admin.ID {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=Cannot+delete+your+own+account", userID), http.StatusSeeOther)
		return
	}

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Store email for audit log before deletion
	deletedEmail := targetUser.Email

	if err := h.authManager.GetDB().DeleteUser(userID); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/settings/users/%s?error=%s", userID, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}

	h.authManager.LogAudit(r, &admin.ID, "user_deleted", "deleted user: "+deletedEmail)
	http.Redirect(w, r, "/settings/users?success=User+deleted+successfully", http.StatusSeeOther)
}

// AdminSMTPSettingsPage serves the SMTP settings page.
func (h *Handler) AdminSMTPSettingsPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	settings, err := h.authManager.GetDB().GetSMTPSettings()
	if err != nil {
		h.logger.Error("Failed to get SMTP settings", "error", err)
		settings = &models.SMTPSettings{ID: 1}
	}
	if settings == nil {
		settings = &models.SMTPSettings{ID: 1}
	}

	component := pages.AdminSMTPSettings(user, settings, errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render SMTP settings template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// AdminSMTPSettingsPost handles SMTP settings update.
func (h *Handler) AdminSMTPSettingsPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/smtp?error=Invalid+request", http.StatusSeeOther)
		return
	}

	// Validate field lengths
	if len(r.FormValue("host")) > 253 || len(r.FormValue("username")) > 254 || len(r.FormValue("password")) > 256 || len(r.FormValue("from_address")) > 254 || len(r.FormValue("from_name")) > 100 {
		http.Redirect(w, r, "/settings/smtp?error=One+or+more+fields+exceed+maximum+length", http.StatusSeeOther)
		return
	}

	// Get existing settings to preserve password if not changed
	existingSettings, err := h.authManager.GetDB().GetSMTPSettings()
	if err != nil {
		http.Redirect(w, r, "/settings/smtp?error="+url.QueryEscape("Failed to load existing settings"), http.StatusSeeOther)
		return
	}

	// Determine encryption settings and port from dropdown selection
	encryption := r.FormValue("encryption")
	var useTLS, useStartTLS bool
	var port int
	switch encryption {
	case "tls":
		useTLS = true
		port = 465
	case "starttls":
		useStartTLS = true
		port = 587
	case "custom":
		// Use custom port value, default to STARTTLS encryption
		customPort, _ := strconv.Atoi(r.FormValue("custom_port"))
		if customPort > 0 && customPort <= 65535 {
			port = customPort
		} else {
			port = 587
		}
		useStartTLS = true
	default: // "none"
		port = 25
	}

	// Get password from form - if blank, keep existing password
	newPassword := r.FormValue("password")
	passwordToSave := newPassword
	if newPassword == "" {
		// Keep existing password if form field is blank
		passwordToSave = existingSettings.Password
	}

	settings := &models.SMTPSettings{
		Host:        r.FormValue("host"),
		Port:        port,
		Username:    r.FormValue("username"),
		Password:    passwordToSave,
		FromAddress: r.FormValue("from_address"),
		FromName:    r.FormValue("from_name"),
		UseTLS:      useTLS,
		UseStartTLS: useStartTLS,
		Enabled:     r.FormValue("enabled") == "on",
	}

	if err := h.authManager.GetDB().UpdateSMTPSettings(settings, admin.ID); err != nil {
		http.Redirect(w, r, "/settings/smtp?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.authManager.LogAudit(r, &admin.ID, models.AuditActionSMTPSettingsChanged, "")
	http.Redirect(w, r, "/settings/smtp?success=SMTP+settings+saved", http.StatusSeeOther)
}

// AdminSMTPTestPost handles sending a test email.
func (h *Handler) AdminSMTPTestPost(w http.ResponseWriter, r *http.Request) {
	admin, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !admin.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/smtp?error=Invalid+request", http.StatusSeeOther)
		return
	}

	testEmail := r.FormValue("test_email")
	if testEmail == "" {
		testEmail = admin.Email
	}

	if h.emailService == nil {
		http.Redirect(w, r, "/settings/smtp?error=Email+service+not+available", http.StatusSeeOther)
		return
	}

	if err := h.emailService.SendTestEmail(testEmail); err != nil {
		http.Redirect(w, r, "/settings/smtp?error="+url.QueryEscape("Failed to send test email: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings/smtp?success=Test+email+sent+to+"+url.QueryEscape(testEmail), http.StatusSeeOther)
}

// SettingsPage serves the main settings page.
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get user permissions for granular access control
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		h.logger.Error("Failed to get user permissions", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	component := pages.Settings(user, perms)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render settings template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// API handlers for user management

// APICurrentUser returns the current user info.
func (h *Handler) APICurrentUser(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104 -- HTTP response write error is not actionable
		"id":           user.ID,
		"email":        user.Email,
		"first_name":   user.FirstName,
		"last_name":    user.LastName,
		"display_name": user.DisplayName(),
		"initials":     user.Initials(),
		"role":         user.Role,
		"is_admin":     user.IsAdmin(),
	})
}

// APIPendingUsersCount returns the count of pending account requests.
// Only accessible by admins or users with approve_users permission.
func (h *Handler) APIPendingUsersCount(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check if user has permission to view pending requests
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !user.IsAdmin() && !perms.CanApproveUsers() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{ //#nosec G104 -- HTTP response write error is not actionable
			"count": 0,
		})
		return
	}

	// Get count of pending account requests
	requests, err := h.db.GetPendingAccountRequests()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get pending account requests", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{ //#nosec G104 -- HTTP response write error is not actionable
		"count": len(requests),
	})
}
