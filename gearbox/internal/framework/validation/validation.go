// Package validation provides input validation utilities for forms and API requests.
//
// This package implements comprehensive server-side validation to protect against:
//   - Invalid input data
//   - SQL injection attempts
//   - XSS attacks
//   - Malformed email addresses, URLs, and other structured data
//
// The validation functions follow a composable pattern where multiple validators
// can be chained together for a single field.
//
// Example usage:
//
//	// Single field validation:
//	if err := validation.Email("email", userInput); err != nil {
//	    return fmt.Errorf("invalid email: %w", err)
//	}
//
//	// Multiple validators on one field:
//	if err := validation.Validate("username", username,
//	    func(v string) error { return validation.Required("username", v) },
//	    func(v string) error { return validation.MinLength("username", v, 3) },
//	    func(v string) error { return validation.Alphanumeric("username", v) },
//	); err != nil {
//	    return err
//	}
//
//	// Batch validation:
//	values := map[string]string{
//	    "email": r.FormValue("email"),
//	    "url":   r.FormValue("url"),
//	}
//	validations := map[string][]validation.Validator{
//	    "email": {
//	        func(v string) error { return validation.Required("email", v) },
//	        func(v string) error { return validation.Email("email", v) },
//	    },
//	    "url": {
//	        func(v string) error { return validation.URL("url", v) },
//	    },
//	}
//	if errs := validation.ValidateAll(validations, values); errs.HasErrors() {
//	    return errs
//	}
package validation

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Validator represents a validation rule.
type Validator func(value string) error

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors represents multiple validation errors.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// HasErrors returns true if there are any validation errors.
func (e ValidationErrors) HasErrors() bool {
	return len(e) > 0
}

// Common validation patterns
var (
	// AlphanumericPattern matches alphanumeric characters and hyphens/underscores
	AlphanumericPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	// IPPattern matches IPv4 addresses
	IPPattern = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`)
	// PortPattern matches port numbers
	PortPattern = regexp.MustCompile(`^[0-9]{1,5}$`)
	// HostnamePattern matches hostnames
	HostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
)

// Required validates that a field is not empty.
func Required(field, value string) *ValidationError {
	if strings.TrimSpace(value) == "" {
		return &ValidationError{Field: field, Message: "is required"}
	}
	return nil
}

// MinLength validates minimum string length.
func MinLength(field, value string, min int) *ValidationError {
	if len(value) < min {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be at least %d characters", min),
		}
	}
	return nil
}

// MaxLength validates maximum string length.
func MaxLength(field, value string, max int) *ValidationError {
	if len(value) > max {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be at most %d characters", max),
		}
	}
	return nil
}

// Email validates email format.
func Email(field, value string) *ValidationError {
	if value == "" {
		return nil // Use Required() separately if field is required
	}
	_, err := mail.ParseAddress(value)
	if err != nil {
		return &ValidationError{Field: field, Message: "must be a valid email address"}
	}
	return nil
}

// URL validates URL format.
func URL(field, value string) *ValidationError {
	if value == "" {
		return nil // Use Required() separately if field is required
	}
	_, err := url.ParseRequestURI(value)
	if err != nil {
		return &ValidationError{Field: field, Message: "must be a valid URL"}
	}
	return nil
}

// Pattern validates against a regex pattern.
func Pattern(field, value string, pattern *regexp.Regexp, message string) *ValidationError {
	if value == "" {
		return nil // Use Required() separately if field is required
	}
	if !pattern.MatchString(value) {
		return &ValidationError{Field: field, Message: message}
	}
	return nil
}

// Alphanumeric validates alphanumeric characters with hyphens and underscores.
func Alphanumeric(field, value string) *ValidationError {
	return Pattern(field, value, AlphanumericPattern, "must contain only letters, numbers, hyphens, and underscores")
}

// IP validates IPv4 address format (basic check).
func IP(field, value string) *ValidationError {
	if value == "" {
		return nil
	}
	if !IPPattern.MatchString(value) {
		return &ValidationError{Field: field, Message: "must be a valid IP address"}
	}
	// Additional validation for octets
	parts := strings.Split(value, ".")
	for _, part := range parts {
		var octet int
		if _, err := fmt.Sscanf(part, "%d", &octet); err != nil || octet < 0 || octet > 255 {
			return &ValidationError{Field: field, Message: "must be a valid IP address"}
		}
	}
	return nil
}

// Port validates port number (1-65535).
func Port(field, value string) *ValidationError {
	if value == "" {
		return nil
	}
	if !PortPattern.MatchString(value) {
		return &ValidationError{Field: field, Message: "must be a valid port number"}
	}
	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil || port < 1 || port > 65535 {
		return &ValidationError{Field: field, Message: "must be between 1 and 65535"}
	}
	return nil
}

// Hostname validates hostname format.
func Hostname(field, value string) *ValidationError {
	return Pattern(field, value, HostnamePattern, "must be a valid hostname")
}

// InRange validates that an integer is within a range.
func InRange(field string, value, min, max int) *ValidationError {
	if value < min || value > max {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be between %d and %d", min, max),
		}
	}
	return nil
}

// OneOf validates that value is one of the allowed values.
func OneOf(field, value string, allowed []string) *ValidationError {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return &ValidationError{
		Field:   field,
		Message: fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")),
	}
}

// PasswordStrength validates password complexity.
// Requirements: min 8 chars, at least one uppercase, one lowercase, one number.
func PasswordStrength(field, value string) *ValidationError {
	if len(value) < 8 {
		return &ValidationError{Field: field, Message: "must be at least 8 characters"}
	}

	var hasUpper, hasLower, hasNumber bool
	for _, c := range value {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsNumber(c):
			hasNumber = true
		}
	}

	if !hasUpper || !hasLower || !hasNumber {
		return &ValidationError{
			Field:   field,
			Message: "must contain at least one uppercase letter, one lowercase letter, and one number",
		}
	}

	return nil
}

// NoSQLInjection performs basic SQL injection detection.
func NoSQLInjection(field, value string) *ValidationError {
	// Basic blacklist of common SQL injection patterns
	dangerous := []string{
		"--",
		";",
		"/*",
		"*/",
		"xp_",
		"sp_",
		"DROP ",
		"DELETE ",
		"INSERT ",
		"UPDATE ",
		"EXEC ",
		"EXECUTE ",
		"UNION ",
		"SELECT ",
	}

	valueLower := strings.ToLower(value)
	for _, pattern := range dangerous {
		if strings.Contains(valueLower, strings.ToLower(pattern)) {
			return &ValidationError{Field: field, Message: "contains potentially dangerous characters"}
		}
	}

	return nil
}

// NoXSS performs basic XSS detection.
func NoXSS(field, value string) *ValidationError {
	// Basic blacklist of common XSS patterns
	dangerous := []string{
		"<script",
		"</script",
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
		"<iframe",
		"<object",
		"<embed",
	}

	valueLower := strings.ToLower(value)
	for _, pattern := range dangerous {
		if strings.Contains(valueLower, pattern) {
			return &ValidationError{Field: field, Message: "contains potentially dangerous content"}
		}
	}

	return nil
}

// Validate runs multiple validators on a field and returns the first error.
func Validate(field, value string, validators ...Validator) *ValidationError {
	for _, validator := range validators {
		if err := validator(value); err != nil {
			// Wrap as ValidationError if not already
			if verr, ok := err.(*ValidationError); ok {
				return verr
			}
			return &ValidationError{Field: field, Message: err.Error()}
		}
	}
	return nil
}

// ValidateAll runs validators and collects all errors.
func ValidateAll(validations map[string][]Validator, values map[string]string) ValidationErrors {
	var errors ValidationErrors

	for field, validators := range validations {
		value := values[field]
		for _, validator := range validators {
			if err := validator(value); err != nil {
				if verr, ok := err.(*ValidationError); ok {
					errors = append(errors, *verr)
				} else {
					errors = append(errors, ValidationError{Field: field, Message: err.Error()})
				}
			}
		}
	}

	return errors
}
