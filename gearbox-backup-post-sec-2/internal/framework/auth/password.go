package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	passwordvalidator "github.com/wagslane/go-password-validator"
	"golang.org/x/crypto/bcrypt"
)

// Password policy constants
const (
	// MinEntropyBits is the minimum entropy required for a password.
	// 50 bits is a reasonable balance between security and usability.
	// - A 4-word passphrase like "correct horse battery staple" scores ~66 bits
	// - A 12-char complex password like "P@ssw0rd123!" scores ~50 bits
	// - A weak password like "password123" scores ~28 bits (fails)
	MinEntropyBits = 50

	// MinPasswordLength is the absolute minimum length (NIST recommends 8)
	MinPasswordLength = 8

	// MaxPasswordLength prevents DoS via bcrypt
	MaxPasswordLength = 128

	// BcryptCost is the bcrypt hashing cost
	BcryptCost = 12

	// GeneratedPasswordLen is the length of auto-generated passwords
	GeneratedPasswordLen = 24
)

// Common weak passwords to reject regardless of entropy
// (these might score okay due to length but are still bad choices)
var commonPasswords = map[string]bool{
	"password":        true,
	"password123":     true,
	"password1234":    true,
	"123456789012":    true,
	"qwertyuiop":      true,
	"qwerty123456":    true,
	"admin123456":     true,
	"letmein12345":    true,
	"welcome12345":    true,
	"changeme1234":    true,
	"iloveyou1234":    true,
	"trustno1234":     true,
}

// PasswordValidationError represents a password validation failure.
type PasswordValidationError struct {
	Errors []string
}

func (e *PasswordValidationError) Error() string {
	return "password validation failed: " + strings.Join(e.Errors, "; ")
}

// ValidatePassword checks if a password meets security requirements using entropy.
// This approach supports both traditional passwords AND passphrases.
// Returns nil if valid, or a PasswordValidationError with all violations.
func ValidatePassword(password string) error {
	var errs []string

	// Check minimum length
	if len(password) < MinPasswordLength {
		errs = append(errs, fmt.Sprintf("password must be at least %d characters long", MinPasswordLength))
	}

	// Check maximum length (prevent bcrypt DoS)
	if len(password) > MaxPasswordLength {
		errs = append(errs, fmt.Sprintf("password must be no more than %d characters long", MaxPasswordLength))
	}

	// Check for common passwords (case-insensitive)
	lower := strings.ToLower(password)
	if commonPasswords[lower] {
		errs = append(errs, "password is too common")
	}

	// Use entropy-based validation
	// This naturally supports passphrases - longer passwords = more entropy
	err := passwordvalidator.Validate(password, MinEntropyBits)
	if err != nil {
		// The library returns a helpful message about what's wrong
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return &PasswordValidationError{Errors: errs}
	}

	return nil
}

// GetPasswordEntropy returns the entropy score for a password.
// Higher is better. 50+ bits is considered secure.
func GetPasswordEntropy(password string) float64 {
	return passwordvalidator.GetEntropy(password)
}

// ValidatePasswordStrength returns a simple strength score (0-100) based on entropy.
func ValidatePasswordStrength(password string) int {
	entropy := passwordvalidator.GetEntropy(password)

	// Map entropy to 0-100 score
	// 0-30 bits: 0-30 score (weak)
	// 30-50 bits: 30-60 score (moderate)
	// 50-70 bits: 60-80 score (good)
	// 70+ bits: 80-100 score (excellent)
	var score int
	switch {
	case entropy < 30:
		score = int(entropy)
	case entropy < 50:
		score = 30 + int((entropy-30)*1.5)
	case entropy < 70:
		score = 60 + int(entropy-50)
	default:
		score = 80 + int((entropy-70)*0.5)
	}

	// Penalty for common passwords
	if commonPasswords[strings.ToLower(password)] {
		score -= 50
	}

	// Clamp to 0-100
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// HashPassword creates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a password against a hash.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateRandomPassword generates a secure random password.
func GenerateRandomPassword() (string, error) {
	// Use a character set that's easy to type but secure
	const charset = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%^&*"

	b := make([]byte, GeneratedPasswordLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}

	// Verify it passes validation (it should with this length and charset)
	password := string(b)
	if err := ValidatePassword(password); err != nil {
		// If somehow it doesn't pass, try again (very unlikely)
		return GenerateRandomPassword()
	}

	return password, nil
}

// GenerateSecureToken generates a cryptographically secure random token.
func GenerateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// ValidateEmail checks if an email address is valid.
func ValidateEmail(email string) error {
	if email == "" {
		return errors.New("email is required")
	}

	// Allow "admin" as a special case for the admin user
	if email == "admin" {
		return nil
	}

	// SECURITY: Use net/mail for robust email validation instead of regex
	// This handles RFC 5322 compliant email addresses and catches edge cases
	// that simple regex patterns might miss (quoted strings, comments, etc.)
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return errors.New("invalid email format")
	}

	// Ensure the parsed address matches the input (no display name)
	if addr.Address != email {
		return errors.New("invalid email format: display name not allowed")
	}

	// Check for reasonable length (RFC 5321 limit is 254 for the path)
	if len(email) > 254 {
		return errors.New("email is too long")
	}

	return nil
}

// GetPasswordRequirements returns a human-readable list of password requirements.
func GetPasswordRequirements() []string {
	return []string{
		"At least 8 characters long",
		"Strong enough to resist guessing attacks (use length or variety)",
		"Cannot be a commonly used password",
		"Passphrases like \"correct horse battery staple\" are encouraged",
	}
}
