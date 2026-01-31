// Package errors provides centralized error types and constants for the gearbox-agent framework.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Common errors used across the application.
var (
	ErrNotFound             = errors.New("resource not found")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrForbidden            = errors.New("forbidden")
	ErrBadRequest           = errors.New("bad request")
	ErrInternalServer       = errors.New("internal server error")
	ErrServiceUnavailable   = errors.New("service unavailable")
	ErrMetadataNotAvailable = errors.New("metadata not available")
	ErrSyncNotConfigured    = errors.New("sync service not configured")
	ErrSyncPending          = errors.New("sync pending")
	ErrInvalidAPIKey        = errors.New("invalid API key")
	ErrInvalidWebhookSecret = errors.New("invalid webhook secret")
	ErrInvalidSignature     = errors.New("invalid signature")
	ErrRateLimitExceeded    = errors.New("rate limit exceeded")
)

// HTTPError represents an error with an associated HTTP status code.
type HTTPError struct {
	Code    int
	Message string
	Err     error
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap implements error unwrapping.
func (e *HTTPError) Unwrap() error {
	return e.Err
}

// NewHTTPError creates a new HTTPError.
func NewHTTPError(code int, message string, err error) *HTTPError {
	return &HTTPError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// Common HTTP error constructors.
func NotFound(message string) *HTTPError {
	return &HTTPError{Code: http.StatusNotFound, Message: message, Err: ErrNotFound}
}

func Unauthorized(message string) *HTTPError {
	return &HTTPError{Code: http.StatusUnauthorized, Message: message, Err: ErrUnauthorized}
}

func Forbidden(message string) *HTTPError {
	return &HTTPError{Code: http.StatusForbidden, Message: message, Err: ErrForbidden}
}

func BadRequest(message string) *HTTPError {
	return &HTTPError{Code: http.StatusBadRequest, Message: message, Err: ErrBadRequest}
}

func InternalServer(message string, err error) *HTTPError {
	return &HTTPError{Code: http.StatusInternalServerError, Message: message, Err: err}
}

func ServiceUnavailable(message string) *HTTPError {
	return &HTTPError{Code: http.StatusServiceUnavailable, Message: message, Err: ErrServiceUnavailable}
}
