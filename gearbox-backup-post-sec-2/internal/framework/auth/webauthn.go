package auth

import (
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// WebAuthnConfig holds configuration for WebAuthn.
type WebAuthnConfig struct {
	RPDisplayName string // Display name for the relying party (e.g., "Gearbox")
	RPID          string // Relying Party ID (e.g., "gearbox.example.com")
	RPOrigins     []string // Allowed origins (e.g., ["https://gearbox.example.com"])
}

// WebAuthnManager handles WebAuthn operations.
type WebAuthnManager struct {
	webAuthn *webauthn.WebAuthn
}

// NewWebAuthnManager creates a new WebAuthn manager.
func NewWebAuthnManager(cfg *WebAuthnConfig) (*WebAuthnManager, error) {
	wconfig := &webauthn.Config{
		RPDisplayName: cfg.RPDisplayName,
		RPID:          cfg.RPID,
		RPOrigins:     cfg.RPOrigins,
	}

	webAuthn, err := webauthn.New(wconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create webauthn: %w", err)
	}

	return &WebAuthnManager{webAuthn: webAuthn}, nil
}

// WebAuthnUser is an adapter that makes models.User compatible with webauthn.User interface.
type WebAuthnUser struct {
	User     *models.User
	Passkeys []*models.Passkey
}

// WebAuthnID returns the user ID as bytes.
func (u *WebAuthnUser) WebAuthnID() []byte {
	return []byte(fmt.Sprintf("%d", u.User.ID))
}

// WebAuthnName returns the user's email as the username.
func (u *WebAuthnUser) WebAuthnName() string {
	return u.User.Email
}

// WebAuthnDisplayName returns the user's display name.
func (u *WebAuthnUser) WebAuthnDisplayName() string {
	return u.User.DisplayName()
}

// WebAuthnCredentials returns the user's registered credentials.
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, 0, len(u.Passkeys))
	for _, pk := range u.Passkeys {
		cred := webauthn.Credential{
			ID:              pk.CredentialID,
			PublicKey:       pk.PublicKey,
			AttestationType: "", // We don't store this
			Flags: webauthn.CredentialFlags{
				BackupEligible: pk.BackupEligible,
				BackupState:    pk.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    pk.AAGUID,
				SignCount: pk.SignCount,
			},
		}
		credentials = append(credentials, cred)
	}
	return credentials
}

// BeginRegistration starts the WebAuthn registration process for a user.
func (m *WebAuthnManager) BeginRegistration(user *WebAuthnUser) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	// Exclude existing credentials to prevent re-registration
	excludeList := make([]protocol.CredentialDescriptor, 0, len(user.Passkeys))
	for _, pk := range user.Passkeys {
		excludeList = append(excludeList, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: pk.CredentialID,
		})
	}

	options, session, err := m.webAuthn.BeginRegistration(
		user,
		webauthn.WithExclusions(excludeList),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			AuthenticatorAttachment: protocol.Platform,
			UserVerification:        protocol.VerificationPreferred,
			ResidentKey:             protocol.ResidentKeyRequirementPreferred,
		}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin registration: %w", err)
	}

	return options, session, nil
}

// FinishRegistration completes the WebAuthn registration process.
func (m *WebAuthnManager) FinishRegistration(user *WebAuthnUser, session *webauthn.SessionData, response *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	credential, err := m.webAuthn.CreateCredential(user, *session, response)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	return credential, nil
}

// BeginLogin starts the WebAuthn authentication process.
func (m *WebAuthnManager) BeginLogin(user *WebAuthnUser) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	options, session, err := m.webAuthn.BeginLogin(user)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin login: %w", err)
	}

	return options, session, nil
}

// BeginDiscoverableLogin starts a discoverable (passwordless) login.
func (m *WebAuthnManager) BeginDiscoverableLogin() (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	options, session, err := m.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin discoverable login: %w", err)
	}

	return options, session, nil
}

// FinishLogin completes the WebAuthn authentication process.
func (m *WebAuthnManager) FinishLogin(user *WebAuthnUser, session *webauthn.SessionData, response *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	credential, err := m.webAuthn.ValidateLogin(user, *session, response)
	if err != nil {
		return nil, fmt.Errorf("failed to validate login: %w", err)
	}

	return credential, nil
}

// FinishDiscoverableLogin completes a discoverable (passwordless) login using a user handler callback.
func (m *WebAuthnManager) FinishDiscoverableLogin(session *webauthn.SessionData, response *protocol.ParsedCredentialAssertionData, userHandler webauthn.DiscoverableUserHandler) (*webauthn.Credential, error) {
	credential, err := m.webAuthn.ValidateDiscoverableLogin(userHandler, *session, response)
	if err != nil {
		return nil, fmt.Errorf("failed to validate discoverable login: %w", err)
	}

	return credential, nil
}
