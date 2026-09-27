package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExpiredLeaseCannotReleaseNewerLease(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if strings.TrimSpace(redisURL) == "" {
		t.Skip("REDIS_URL not configured; skipping Redis admission integration test")
	}

	t.Setenv("DEFAULT_KEY_RPM", "1000")
	t.Setenv("DEFAULT_KEY_DAILY", "1000")
	t.Setenv("DEFAULT_KEY_MAX_CONCURRENT", "1")
	t.Setenv("DEFAULT_PROJECT_MAX_CONCURRENT", "10")
	t.Setenv("GLOBAL_RPM_LIMIT", "1000")
	t.Setenv("GLOBAL_DAILY_LIMIT", "1000")
	t.Setenv("GLOBAL_MAX_CONCURRENT", "100")
	t.Setenv("CONCURRENCY_LEASE_TTL_SECONDS", "1")

	controller, err := NewAdmissionController(redisURL)
	if err != nil {
		t.Fatalf("create admission controller: %v", err)
	}
	defer controller.Close()

	suffix := time.Now().UTC().Format("150405.000000000")
	principal := &APIKeyPrincipal{
		ID:        "lease-key-" + suffix,
		ProjectID: "lease-project-" + suffix,
	}

	first, apiErr := controller.Admit(context.Background(), principal)
	if apiErr != nil {
		t.Fatalf("first admission failed: %#v", apiErr)
	}

	time.Sleep(1200 * time.Millisecond)

	second, apiErr := controller.Admit(context.Background(), principal)
	if apiErr != nil {
		t.Fatalf("second admission after expiry failed: %#v", apiErr)
	}

	first.Release(context.Background())

	third, apiErr := controller.Admit(context.Background(), principal)
	if third != nil {
		third.Release(context.Background())
		t.Fatal("old lease release incorrectly freed the newer lease")
	}
	if apiErr == nil || apiErr.Code != "NEXORA_KEY_CONCURRENCY_LIMIT" {
		t.Fatalf("expected key concurrency limit while second lease is active, got %#v", apiErr)
	}

	second.Release(context.Background())

	fourth, apiErr := controller.Admit(context.Background(), principal)
	if apiErr != nil {
		t.Fatalf("admission should succeed after active lease release: %#v", apiErr)
	}
	fourth.Release(context.Background())
}
