package auth

import (
	webcoreauth "github.com/sarg3nt/webcore/core/auth"
)

// Token, UUID, and email helpers now live in webcore/core/auth; re-exported
// so gearbox call sites compile unchanged.
var (
	GenerateUUID         = webcoreauth.GenerateUUID
	GenerateSessionToken = webcoreauth.GenerateSessionToken
	GenerateCSRFToken    = webcoreauth.GenerateCSRFToken
	GenerateSecureToken  = webcoreauth.GenerateSecureToken
	ValidateEmail        = webcoreauth.ValidateEmail
)
