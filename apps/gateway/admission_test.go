package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestPrincipalLimitUsesKeyOverride(t *testing.T) {
	got := principalLimit(sql.NullInt64{Int64: 7, Valid: true}, "DEFAULT_KEY_RPM", 5)
	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestPrincipalLimitFallsBackToDefault(t *testing.T) {
	t.Setenv("DEFAULT_KEY_RPM", "6")
	got := principalLimit(sql.NullInt64{}, "DEFAULT_KEY_RPM", 5)
	if got != 6 {
		t.Fatalf("expected 6, got %d", got)
	}
}

func TestAdmissionLimitErrorMapsKeyDailyQuota(t *testing.T) {
	err := admissionLimitError(4)
	if err.Status != 429 || err.Code != "NEXORA_KEY_DAILY_LIMIT" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestAdmissionLimitErrorMapsGlobalConcurrency(t *testing.T) {
	err := admissionLimitError(10)
	if err.Status != 429 || err.Code != "NEXORA_GLOBAL_CONCURRENCY_LIMIT" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestAdmissionAtomicRateAndConcurrencyLimits(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not configured")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}

	controller, err := NewAdmissionController(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()

	for key, value := range map[string]string{
		"GLOBAL_DAILY_LIMIT":             "1000",
		"DEFAULT_USER_DAILY":             "1000",
		"DEFAULT_PROJECT_DAILY":          "1000",
		"GLOBAL_RPM_LIMIT":               "1000",
		"DEFAULT_USER_RPM":               "1000",
		"DEFAULT_PROJECT_RPM":            "1000",
		"DEFAULT_IP_RPM":                 "1000",
		"GLOBAL_MAX_CONCURRENT":          "1000",
		"DEFAULT_PROJECT_MAX_CONCURRENT": "1000",
	} {
		t.Setenv(key, value)
	}

	principal := &APIKeyPrincipal{
		ID:                "key-rate",
		ProjectID:         "project-rate",
		UserID:            "user-rate",
		RequestsPerMinute: sql.NullInt64{Int64: 1, Valid: true},
		RequestsPerDay:    sql.NullInt64{Int64: 1000, Valid: true},
		MaxConcurrent:     sql.NullInt64{Int64: 10, Valid: true},
	}
	first, apiErr := controller.Admit(context.Background(), principal, "127.0.0.1")
	if apiErr != nil {
		t.Fatalf("first request unexpectedly denied: %#v", apiErr)
	}
	first.Release()

	_, apiErr = controller.Admit(context.Background(), principal, "127.0.0.1")
	if apiErr == nil || apiErr.Code != "NEXORA_KEY_RPM_LIMIT" {
		t.Fatalf("expected key RPM rejection, got %#v", apiErr)
	}

	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	principal.ID = "key-concurrency"
	principal.ProjectID = "project-concurrency"
	principal.UserID = "user-concurrency"
	principal.RequestsPerMinute = sql.NullInt64{Int64: 1000, Valid: true}
	principal.MaxConcurrent = sql.NullInt64{Int64: 1, Valid: true}

	lease, apiErr := controller.Admit(context.Background(), principal, "127.0.0.2")
	if apiErr != nil {
		t.Fatalf("first concurrent request unexpectedly denied: %#v", apiErr)
	}
	_, apiErr = controller.Admit(context.Background(), principal, "127.0.0.2")
	if apiErr == nil || apiErr.Code != "NEXORA_KEY_CONCURRENCY_LIMIT" {
		t.Fatalf("expected concurrency rejection, got %#v", apiErr)
	}
	lease.Release()
	lease.Release()

	retry, apiErr := controller.Admit(context.Background(), principal, "127.0.0.2")
	if apiErr != nil {
		t.Fatalf("expected released slot to be reusable: %#v", apiErr)
	}
	retry.Release()
}
