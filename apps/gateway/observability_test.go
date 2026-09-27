package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestObservabilityCorrelatesResponseAndErrorRequestID(t *testing.T) {
	handler := requestObservabilityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRequestAPIError(
			w,
			r,
			newAPIError(
				http.StatusBadRequest,
				"invalid_request_error",
				"NEXORA_TEST_ERROR",
				"test error",
			),
		)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("expected X-Request-ID response header")
	}

	var payload struct {
		Error struct {
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Error.RequestID != requestID {
		t.Fatalf("request ID mismatch: body=%q header=%q", payload.Error.RequestID, requestID)
	}
}

func TestObservedResponseWriterCapturesStatusAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	observed := &ObservedResponseWriter{ResponseWriter: rec}
	observed.Header().Set("Content-Type", "text/plain")
	observed.WriteHeader(http.StatusCreated)
	_, _ = observed.Write([]byte("hello"))

	if observed.Status() != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", observed.Status())
	}
	if observed.bytes != 5 {
		t.Fatalf("expected 5 response bytes, got %d", observed.bytes)
	}
}
