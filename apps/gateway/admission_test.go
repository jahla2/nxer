package main

import (
	"database/sql"
	"testing"
)

func TestPrincipalLimitUsesKeyOverride(t *testing.T) {
	got := principalLimit(sql.NullInt64{Int64: 7, Valid: true}, "DEFAULT_KEY_RPM", 5)
	if got != 7 { t.Fatalf("expected 7, got %d", got) }
}

func TestPrincipalLimitFallsBackToDefault(t *testing.T) {
	t.Setenv("DEFAULT_KEY_RPM", "6")
	got := principalLimit(sql.NullInt64{}, "DEFAULT_KEY_RPM", 5)
	if got != 6 { t.Fatalf("expected 6, got %d", got) }
}

func TestAdmissionLimitErrorMapsDailyQuota(t *testing.T) {
	err := admissionLimitError(2)
	if err.Status != 429 || err.Code != "NEXORA_KEY_DAILY_LIMIT" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestAdmissionLimitErrorMapsGlobalConcurrency(t *testing.T) {
	err := admissionLimitError(7)
	if err.Status != 429 || err.Code != "NEXORA_GLOBAL_CONCURRENCY_LIMIT" {
		t.Fatalf("unexpected error: %#v", err)
	}
}
