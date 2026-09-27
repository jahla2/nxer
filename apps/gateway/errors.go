package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type APIError struct {
	Status int
	Type string
	Code string
	Message string
	RequestID string
}

func newAPIError(status int, typ, code, message string) *APIError {
	return &APIError{Status: status, Type: typ, Code: code, Message: message, RequestID: newRequestID()}
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_nxa_unavailable"
	}
	return "req_nxa_" + hex.EncodeToString(b[:])
}

func writeAPIError(w http.ResponseWriter, apiErr *APIError) {
	if apiErr == nil {
		apiErr = newAPIError(http.StatusInternalServerError, "server_error", "NEXORA_INTERNAL_ERROR", "An unexpected error occurred.")
	}
	writeJSON(w, apiErr.Status, map[string]any{"error": map[string]any{"type": apiErr.Type, "code": apiErr.Code, "message": apiErr.Message, "request_id": apiErr.RequestID}})
}

func writeRequestAPIError(w http.ResponseWriter, r *http.Request, apiErr *APIError) {
	if apiErr == nil {
		apiErr = newAPIError(http.StatusInternalServerError, "server_error", "NEXORA_INTERNAL_ERROR", "An unexpected error occurred.")
	}
	if r != nil {
		if requestID := requestIDFromContext(r.Context()); requestID != "" {
			apiErr.RequestID = requestID
			w.Header().Set("X-Request-ID", requestID)
		}
	}
	writeAPIError(w, apiErr)
}
