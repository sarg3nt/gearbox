// Package errors provides error sanitization and structured error handling
// for the HAProxy monitoring application.
//
// This package implements a centralized error handling system that:
//   - Sanitizes internal errors before exposing them to users
//   - Provides structured logging for error diagnosis
//   - Maintains consistent HTTP status codes across the application
//   - Separates user-facing messages from internal error details
//
// Example usage:
//
//	// In an HTTP handler:
//	user, err := db.GetUser(id)
//	if err != nil {
//	    errors.WriteHTTPError(w, logger, errors.Internal("fetch user", err))
//	    return
//	}
//
//	// Or using specific error constructors:
//	if !authorized {
//	    errors.WriteHTTPError(w, logger, errors.Forbidden("Access denied", nil))
//	    return
//	}
package errors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// AppError represents a structured application error with user-safe message
// and internal details for logging.
type AppError struct {
	// Code is the HTTP status code
	Code int
	// UserMessage is safe to display to end users
	UserMessage string
	// InternalError contains the actual error (logged but not shown to user)
	InternalError error
	// LogContext contains additional context for structured logging
	LogContext map[string]any
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.InternalError != nil {
		return fmt.Sprintf("%s: %v", e.UserMessage, e.InternalError)
	}
	return e.UserMessage
}

// Unwrap implements error unwrapping for errors.Is and errors.As.
func (e *AppError) Unwrap() error {
	return e.InternalError
}

// ErrorType represents common error categories.
type ErrorType string

const (
	ErrorTypeNotFound      ErrorType = "not_found"
	ErrorTypeUnauthorized  ErrorType = "unauthorized"
	ErrorTypeForbidden     ErrorType = "forbidden"
	ErrorTypeBadRequest    ErrorType = "bad_request"
	ErrorTypeInternal      ErrorType = "internal_error"
	ErrorTypeServiceUnavailable ErrorType = "service_unavailable"
	ErrorTypeConflict      ErrorType = "conflict"
)

// New creates a new AppError with the given parameters.
func New(code int, userMsg string, internalErr error) *AppError {
	return &AppError{
		Code:          code,
		UserMessage:   userMsg,
		InternalError: internalErr,
		LogContext:    make(map[string]any),
	}
}

// WithContext adds structured logging context to the error.
func (e *AppError) WithContext(key string, value any) *AppError {
	e.LogContext[key] = value
	return e
}

// Common error constructors

// NotFound creates a 404 error.
func NotFound(resource string, internalErr error) *AppError {
	return New(
		http.StatusNotFound,
		fmt.Sprintf("%s not found", resource),
		internalErr,
	).WithContext("resource", resource)
}

// Unauthorized creates a 401 error.
func Unauthorized(message string, internalErr error) *AppError {
	return New(http.StatusUnauthorized, message, internalErr)
}

// Forbidden creates a 403 error.
func Forbidden(message string, internalErr error) *AppError {
	return New(http.StatusForbidden, message, internalErr)
}

// BadRequest creates a 400 error.
func BadRequest(message string, internalErr error) *AppError {
	return New(http.StatusBadRequest, message, internalErr)
}

// Internal creates a 500 error with a generic user message.
func Internal(operation string, internalErr error) *AppError {
	return New(
		http.StatusInternalServerError,
		fmt.Sprintf("Failed to %s. Please try again later.", operation),
		internalErr,
	).WithContext("operation", operation)
}

// ServiceUnavailable creates a 503 error.
func ServiceUnavailable(service string, internalErr error) *AppError {
	return New(
		http.StatusServiceUnavailable,
		fmt.Sprintf("%s is currently unavailable", service),
		internalErr,
	).WithContext("service", service)
}

// Conflict creates a 409 error.
func Conflict(message string, internalErr error) *AppError {
	return New(http.StatusConflict, message, internalErr)
}

// WrapDatabaseError converts database errors to appropriate AppErrors.
// Use this for database operations where ErrNoRows is unexpected.
func WrapDatabaseError(err error, operation string) *AppError {
	if err == nil {
		return nil
	}

	// Handle specific database errors
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("Resource", err)
	}

	// Generic database error - don't expose internal details
	return Internal(operation, err)
}

// IsNotFound checks if an error is sql.ErrNoRows.
// Use this helper when ErrNoRows is expected behavior (e.g., optional lookups).
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// IgnoreNotFound returns nil if the error is sql.ErrNoRows, otherwise returns the error.
// Use this when a record not existing is acceptable/expected behavior.
func IgnoreNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// WriteHTTPError writes an AppError to an HTTP response and logs it.
func WriteHTTPError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var appErr *AppError

	// Convert to AppError if not already
	if !errors.As(err, &appErr) {
		// Unknown error - create generic internal error
		appErr = Internal("process request", err)
	}

	// Log the error with full details
	logAttrs := []slog.Attr{
		slog.Int("status_code", appErr.Code),
		slog.String("user_message", appErr.UserMessage),
	}

	if appErr.InternalError != nil {
		logAttrs = append(logAttrs, slog.String("internal_error", appErr.InternalError.Error()))
	}

	// Add context fields
	for key, value := range appErr.LogContext {
		logAttrs = append(logAttrs, slog.Any(key, value))
	}

	logger.LogAttrs(context.Background(), slog.LevelError, "HTTP error", logAttrs...)

	// Write sanitized error to client
	http.Error(w, appErr.UserMessage, appErr.Code)
}

// SanitizeError converts any error to a user-safe message.
// This is a fallback for cases where structured errors aren't used.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}

	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.UserMessage
	}

	// Database errors
	if errors.Is(err, sql.ErrNoRows) {
		return "Resource not found"
	}

	// Generic fallback - never expose internal error details
	return "An error occurred. Please try again later."
}
