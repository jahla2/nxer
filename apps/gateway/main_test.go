package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	rr := httptest.NewRecorder()
	healthHandler(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK { t.Fatalf("expected 200, got %d", rr.Code) }
}

func TestModelsRejectsWrongMethod(t *testing.T) {
	rr := httptest.NewRecorder()
	modelsHandler(rr, httptest.NewRequest(http.MethodPost, "/v1/models", nil))
	if rr.Code != http.StatusMethodNotAllowed { t.Fatalf("expected 405, got %d", rr.Code) }
}
