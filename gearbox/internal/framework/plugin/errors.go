package plugin

import "fmt"

// PluginError represents an error that occurred in a plugin.
type PluginError struct {
	// Plugin is the name of the plugin that produced the error.
	Plugin string

	// Code is a machine-readable error code.
	Code string

	// Message is a human-readable error message.
	Message string

	// Cause is the underlying error, if any.
	Cause error
}

// Error implements the error interface.
func (e *PluginError) Error() string {
	if e.Plugin != "" {
		if e.Code != "" {
			return fmt.Sprintf("[%s] %s: %s", e.Plugin, e.Code, e.Message)
		}
		return fmt.Sprintf("[%s] %s", e.Plugin, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// Unwrap returns the underlying error.
func (e *PluginError) Unwrap() error {
	return e.Cause
}

// NewPluginError creates a new PluginError.
func NewPluginError(plugin, code, message string) *PluginError {
	return &PluginError{
		Plugin:  plugin,
		Code:    code,
		Message: message,
	}
}

// WrapError wraps an existing error with plugin context.
func WrapError(plugin string, err error) *PluginError {
	return &PluginError{
		Plugin:  plugin,
		Message: err.Error(),
		Cause:   err,
	}
}

// Common error codes
const (
	// ErrCodeNotInitialized indicates the plugin has not been initialized.
	ErrCodeNotInitialized = "not_initialized"

	// ErrCodeAlreadyInitialized indicates the plugin has already been initialized.
	ErrCodeAlreadyInitialized = "already_initialized"

	// ErrCodeNotStarted indicates the plugin has not been started.
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
