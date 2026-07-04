package auth

import (
	webcoreauth "github.com/sarg3nt/webcore/core/auth"
)

// Password policy and helpers now live in webcore/core/auth; these re-exports
// keep gearbox's historical auth.* call sites (handlers, templates, main.go)
// compiling unchanged. The policy values are identical to the pre-webcore
// implementation (50-bit entropy floor, 8..128 length, bcrypt cost 12).
const (
	MinEntropyBits       = webcoreauth.MinEntropyBits
	MinPasswordLength    = webcoreauth.MinPasswordLength
	MaxPasswordLength    = webcoreauth.MaxPasswordLength
	BcryptCost           = webcoreauth.BcryptCost
	GeneratedPasswordLen = webcoreauth.GeneratedPasswordLen
	MinTokenBytes        = webcoreauth.MinTokenBytes
)

// PasswordValidationError aggregates password policy violations.
type PasswordValidationError = webcoreauth.PasswordValidationError

var (
	ValidatePassword         = webcoreauth.ValidatePassword
	GetPasswordEntropy       = webcoreauth.GetPasswordEntropy
	ValidatePasswordStrength = webcoreauth.ValidatePasswordStrength
	HashPassword             = webcoreauth.HashPassword
	CheckPassword            = webcoreauth.CheckPassword
	GenerateRandomPassword   = webcoreauth.GenerateRandomPassword
	GetPasswordRequirements  = webcoreauth.GetPasswordRequirements
)
