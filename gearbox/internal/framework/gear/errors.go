package gear

import "fmt"

// GearError represents an error that occurred in a gear.
type GearError struct {
	// Plugin is the name of the plugin that produced the error.
	Gear string

	// Code is a machine-readable error code.
	Code string

	// Message is a human-readable error message.
	Message string

	// Cause is the underlying error, if any.
	Cause error
}

// Error implements the error interface.
func (e *GearError) Error() string {
	if e.Gear != "" {
		if e.Code != "" {
			return fmt.Sprintf("[%s] %s: %s", e.Gear, e.Code, e.Message)
		}
		return fmt.Sprintf("[%s] %s", e.Gear, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// Unwrap returns the underlying error.
func (e *GearError) Unwrap() error {
	return e.Cause
}

// NewGearError creates a new GearError.
func NewGearError(plugin, code, message string) *GearError {
	return &GearError{
		Gear:    plugin,
		Code:    code,
		Message: message,
	}
}

// WrapError wraps an existing error with gear context.
func WrapError(plugin string, err error) *GearError {
	return &GearError{
		Gear:    plugin,
		Message: err.Error(),
		Cause:   err,
	}
}

// Common error codes
const (
	// ErrCodeNotInitialized indicates the gear has not been initialized.
	ErrCodeNotInitialized = "not_initialized"

	// ErrCodeAlreadyInitialized indicates the plugin has already been initialized.
	ErrCodeAlreadyInitialized = "already_initialized"

	// ErrCodeNotStarted indicates the gear has not been started.
	ErrCodeNotStarted = "not_started"

	// ErrCodeAlreadyStopped indicates the plugin has already been stopped.
	ErrCodeAlreadyStopped = "already_stopped"

	// ErrCodeConfigInvalid indicates the plugin configuration is invalid.
	ErrCodeConfigInvalid = "config_invalid"

	// ErrCodeDependencyMissing indicates a required dependency is missing.
	ErrCodeDependencyMissing = "dependency_missing"

	// ErrCodePermissionDenied indicates insufficient permissions.
	ErrCodePermissionDenied = "permission_denied"

	// ErrCodeResourceNotFound indicates a requested resource was not found.
	ErrCodeResourceNotFound = "resource_not_found"

	// ErrCodeOperationFailed indicates an operation failed.
	ErrCodeOperationFailed = "operation_failed"
)
