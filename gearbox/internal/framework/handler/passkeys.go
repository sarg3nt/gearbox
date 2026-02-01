package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// PasskeyRegisterBeginResponse is the JSON response for registration start.
type PasskeyRegisterBeginResponse struct {
	Options   *protocol.CredentialCreation `json:"options"`
	SessionID string                       `json:"session_id"`
}

// PasskeyLoginBeginResponse is the JSON response for login start.
type PasskeyLoginBeginResponse struct {
	Options   *protocol.CredentialAssertion `json:"options"`
	SessionID string                        `json:"session_id"`
}

// PasskeyRegisterBegin starts the passkey registration process.
func (h *Handler) PasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get WebAuthn manager from handler
	webAuthnMgr := h.getWebAuthnManager()
	if webAuthnMgr == nil {
		h.logger.Info("WebAuthn not configured")
		http.Error(w, "WebAuthn not configured", http.StatusInternalServerError)
		return
	}

	// Get user's existing passkeys
	passkeys, err := h.db.GetUserPasskeys(user.ID)
	if err != nil {
		h.logger.Error("Failed to get user passkeys", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create WebAuthn user
	webAuthnUser := &auth.WebAuthnUser{
		User:     user,
		Passkeys: passkeys,
	}

	// Begin registration
	options, session, err := webAuthnMgr.BeginRegistration(webAuthnUser)
	if err != nil {
		h.logger.Error("Failed to begin registration", "error", err)
		http.Error(w, "Failed to start registration", http.StatusInternalServerError)
		return
	}

	// Generate session ID
	sessionID, err := database.GenerateSecureToken(32)
	if err != nil {
		h.logger.Error("Failed to generate session ID", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Serialize session data
	sessionData, err := json.Marshal(session)
	if err != nil {
		h.logger.Error("Failed to serialize session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store session with 5 minute expiry
	expiresAt := time.Now().Add(5 * time.Minute)
	if err := h.db.CreateWebAuthnSession(sessionID, &user.ID, sessionData, database.WebAuthnSessionRegistration, expiresAt); err != nil {
		h.logger.Error("Failed to store WebAuthn session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return options to client
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PasskeyRegisterBeginResponse{ //#nosec G104
		Options:   options,
		SessionID: sessionID,
	})
}

// PasskeyRegisterFinish completes the passkey registration process.
func (h *Handler) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		SessionID  string `json:"session_id"`
		Name       string `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get WebAuthn manager
	webAuthnMgr := h.getWebAuthnManager()
	if webAuthnMgr == nil {
		http.Error(w, "WebAuthn not configured", http.StatusInternalServerError)
		return
	}

	// Retrieve session
	sessionData, storedUserID, err := h.db.GetWebAuthnSession(req.SessionID)
	if err != nil || sessionData == nil {
		http.Error(w, "Invalid or expired session", http.StatusBadRequest)
		return
	}

	// Verify session belongs to this user
	if storedUserID == nil || *storedUserID != user.ID {
		http.Error(w, "Session mismatch", http.StatusBadRequest)
		return
	}

	// Delete the session immediately (one-time use)
	_ = h.db.DeleteWebAuthnSession(req.SessionID)

	// Deserialize session
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		h.logger.Error("Failed to deserialize session", "error", err)
		http.Error(w, "Invalid session data", http.StatusBadRequest)
		return
	}

	// Parse the credential creation response from the JSON body
	parsedResponse, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		h.logger.Error("Failed to parse credential response", "error", err)
		http.Error(w, "Invalid credential response", http.StatusBadRequest)
		return
	}

	// Get user's existing passkeys
	passkeys, _ := h.db.GetUserPasskeys(user.ID)

	// Create WebAuthn user
	webAuthnUser := &auth.WebAuthnUser{
		User:     user,
		Passkeys: passkeys,
	}

	// Finish registration
	credential, err := webAuthnMgr.FinishRegistration(webAuthnUser, &session, parsedResponse)
	if err != nil {
		h.logger.Error("Failed to finish registration", "error", err)
		http.Error(w, "Failed to verify credential", http.StatusBadRequest)
		return
	}

	// Set default name if not provided
	passkeyName := req.Name
	if passkeyName == "" {
		passkeyName = "Passkey"
	}

	// Store the passkey
	passkey := &models.Passkey{
		UserID:         user.ID,
		CredentialID:   credential.ID,
		PublicKey:      credential.PublicKey,
		Name:           passkeyName,
		AAGUID:         credential.Authenticator.AAGUID,
		SignCount:      credential.Authenticator.SignCount,
		BackupEligible: credential.Flags.BackupEligible,
		BackupState:    credential.Flags.BackupState,
	}

	passkeyID, err := h.db.CreatePasskey(passkey)
	if err != nil {
		h.logger.Error("Failed to store passkey", "error", err)
		http.Error(w, "Failed to store passkey", http.StatusInternalServerError)
		return
	}

	// Log the action
	h.authManager.LogAudit(r, &user.ID, models.AuditActionPasskeyAdded, fmt.Sprintf("passkey '%s' (id: %d)", passkeyName, passkeyID))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
		"success": true,
		"message": "Passkey registered successfully",
	})
}

// PasskeyDelete handles passkey deletion.
func (h *Handler) PasskeyDelete(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/profile?error=Invalid+request", http.StatusSeeOther)
		return
	}

	// Validate CSRF token
	if err := h.authManager.ValidateCSRFToken(r); err != nil {
		http.Redirect(w, r, "/settings/profile?error=Invalid+CSRF+token", http.StatusSeeOther)
		return
	}

	passkeyID, err := strconv.ParseInt(r.FormValue("passkey_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/profile?error=Invalid+passkey+ID", http.StatusSeeOther)
		return
	}

	// Get passkey to log the name
	passkey, err := h.db.GetPasskeyByID(passkeyID)
	if err != nil || passkey == nil {
		http.Redirect(w, r, "/settings/profile?error=Passkey+not+found", http.StatusSeeOther)
		return
	}

	// Verify passkey belongs to user
	if passkey.UserID != user.ID {
		http.Redirect(w, r, "/settings/profile?error=Unauthorized", http.StatusSeeOther)
		return
	}

	// Delete the passkey
	if err := h.db.DeletePasskey(passkeyID, user.ID); err != nil {
		http.Redirect(w, r, "/settings/profile?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Log the action
	h.authManager.LogAudit(r, &user.ID, models.AuditActionPasskeyRemoved, fmt.Sprintf("passkey '%s' (id: %d)", passkey.Name, passkeyID))

	http.Redirect(w, r, "/settings/profile?success=Passkey+removed+successfully", http.StatusSeeOther)
}

// PasskeyLoginBegin starts the passkey authentication process.
func (h *Handler) PasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	webAuthnMgr := h.getWebAuthnManager()
	if webAuthnMgr == nil {
		http.Error(w, "WebAuthn not configured", http.StatusInternalServerError)
		return
	}

	// Use discoverable login (passwordless)
	options, session, err := webAuthnMgr.BeginDiscoverableLogin()
	if err != nil {
		h.logger.Error("Failed to begin discoverable login", "error", err)
		http.Error(w, "Failed to start authentication", http.StatusInternalServerError)
		return
	}

	// Generate session ID
	sessionID, err := database.GenerateSecureToken(32)
	if err != nil {
		h.logger.Error("Failed to generate session ID", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Serialize session data
	sessionData, err := json.Marshal(session)
	if err != nil {
		h.logger.Error("Failed to serialize session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store session with 5 minute expiry (no user ID for discoverable login)
	expiresAt := time.Now().Add(5 * time.Minute)
	if err := h.db.CreateWebAuthnSession(sessionID, nil, sessionData, database.WebAuthnSessionLogin, expiresAt); err != nil {
		h.logger.Error("Failed to store WebAuthn session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PasskeyLoginBeginResponse{ //#nosec G104
		Options:   options,
		SessionID: sessionID,
	})
}

// PasskeyLoginFinish completes the passkey authentication process.
func (h *Handler) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	webAuthnMgr := h.getWebAuthnManager()
	if webAuthnMgr == nil {
		http.Error(w, "WebAuthn not configured", http.StatusInternalServerError)
		return
	}

	// Parse request body
	var req struct {
		SessionID  string          `json:"session_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Retrieve session
	sessionData, _, err := h.db.GetWebAuthnSession(req.SessionID)
	if err != nil || sessionData == nil {
		http.Error(w, "Invalid or expired session", http.StatusBadRequest)
		return
	}

	// Delete the session immediately (one-time use)
	_ = h.db.DeleteWebAuthnSession(req.SessionID)

	// Deserialize session
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		h.logger.Error("Failed to deserialize session", "error", err)
		http.Error(w, "Invalid session data", http.StatusBadRequest)
		return
	}

	// Parse the credential assertion response from the JSON body
	parsedResponse, err := protocol.ParseCredentialRequestResponseBytes(req.Credential)
	if err != nil {
		h.logger.Error("Failed to parse credential response", "error", err)
		http.Error(w, "Invalid credential response", http.StatusBadRequest)
		return
	}

	// Variables to capture user info from the handler callback
	var foundUser *models.User
	var foundPasskey *models.Passkey

	// Create a user handler callback for discoverable login
	// This is called by the WebAuthn library to look up the user
	userHandler := func(rawID, userHandle []byte) (webauthn.User, error) {
		// Look up user by credential ID (rawID)
		user, passkey, err := h.db.GetUserByPasskeyCredentialID(rawID)
		if err != nil {
			return nil, fmt.Errorf("database error: %w", err)
		}
		if user == nil || passkey == nil {
			return nil, fmt.Errorf("no user found for credential")
		}

		// Check if user can login
		if !user.CanLogin() {
			return nil, fmt.Errorf("account is disabled or locked")
		}

		// Store for later use
		foundUser = user
		foundPasskey = passkey

		// Get all passkeys for the user
		passkeys, _ := h.db.GetUserPasskeys(user.ID)

		return &auth.WebAuthnUser{
			User:     user,
			Passkeys: passkeys,
		}, nil
	}

	// Validate the assertion using discoverable login
	credential, err := webAuthnMgr.FinishDiscoverableLogin(&session, parsedResponse, userHandler)
	if err != nil {
		h.logger.Error("Failed to validate login", "error", err)
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	// Use the user found during validation
	user := foundUser
	passkey := foundPasskey

	if user == nil {
		h.logger.Info("User not found after validation")
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	// Update sign count and backup flags after successful authentication
	if err := h.db.UpdatePasskeyAfterLogin(passkey.ID, credential.Authenticator.SignCount, credential.Flags.BackupEligible, credential.Flags.BackupState); err != nil {
		h.logger.Error("Failed to update passkey after login", "error", err)
		// Don't fail the login, just log the error
	}

	// Create session for the user
	if err := h.authManager.CreateSessionForUser(w, r, user); err != nil {
		h.logger.Error("Failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Record successful login
	if err := h.db.RecordLoginAttempt(user.ID, true); err != nil {
		h.logger.Error("Failed to record login", "error", err)
	}

	// Log the login
	h.authManager.LogAudit(r, &user.ID, models.AuditActionLogin, "via passkey")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
		"success":  true,
		"redirect": "/",
	})
}
