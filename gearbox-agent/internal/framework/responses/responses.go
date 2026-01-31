// Package responses provides standardized HTTP response helpers for the gearbox-agent framework.
package responses

import (
	"encoding/json"
	"net/http"

	"github.com/sarg3nt/gearbox-agent/internal/framework/errors"
)

// ErrorResponse represents a standard error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// RespondJSON writes a JSON response with the given status code.
func RespondJSON(w http.ResponseWriter, status int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

// RespondError writes an error response.
func RespondError(w http.ResponseWriter, status int, message string) {
	resp := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// RespondHTTPError writes an HTTPError response.
func RespondHTTPError(w http.ResponseWriter, err *errors.HTTPError) {
	RespondError(w, err.Code, err.Message)
}

// RespondOK writes a 200 OK response with optional data.
func RespondOK(w http.ResponseWriter, data any) error {
	return RespondJSON(w, http.StatusOK, data)
}

// RespondCreated writes a 201 Created response with optional data.
func RespondCreated(w http.ResponseWriter, data any) error {
	return RespondJSON(w, http.StatusCreated, data)
}

// RespondNoContent writes a 204 No Content response.
func RespondNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// RespondBadRequest writes a 400 Bad Request response.
func RespondBadRequest(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusBadRequest, message)
}

// RespondUnauthorized writes a 401 Unauthorized response.
func RespondUnauthorized(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusUnauthorized, message)
}

// RespondForbidden writes a 403 Forbidden response.
func RespondForbidden(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusForbidden, message)
}

// RespondNotFound writes a 404 Not Found response.
func RespondNotFound(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusNotFound, message)
}

// RespondInternalError writes a 500 Internal Server Error response.
func RespondInternalError(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusInternalServerError, message)
}

// RespondServiceUnavailable writes a 503 Service Unavailable response.
func RespondServiceUnavailable(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusServiceUnavailable, message)
}
