package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUsageRecorderPersistsUsageAndLastUsed(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("DATABASE_URL not configured; skipping PostgreSQL usage integration test")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%012x", time.Now().UnixNano()&0xffffffffffff)
	email := "gateway-usage-" + suffix + "@example.test"
	publicModel := "nexora/usage-" + suffix
	upstreamModel := "test/usage-" + suffix
	keyPrefix := "nxa_live_" + suffix
	requestID := "req_nxa_usage_" + suffix

	var userID, projectID, modelID, apiKeyID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, display_name, status)
		VALUES ($1, 'integration-test-only', 'Gateway Usage Test', 'active')
		RETURNING id::text
	`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM usage_events WHERE request_id=$1", requestID)
		if apiKeyID != "" {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id=$1", apiKeyID)
		}
		if modelID != "" {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM models WHERE id=$1", modelID)
		}
		if projectID != "" {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM projects WHERE id=$1", projectID)
		}
		if userID != "" {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id=$1", userID)
		}
	})

	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (user_id, name, status)
		VALUES ($1, 'Usage Test Project', 'active')
		RETURNING id::text
	`, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO models (public_id, upstream_id, display_name, active, is_free)
		VALUES ($1, $2, 'Usage Test Model', true, true)
		RETURNING id::text
	`, publicModel, upstreamModel).Scan(&modelID); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO api_keys (
			project_id, name, key_prefix, key_hash, status, allow_all_free_models
		)
		VALUES ($1, 'Usage Test Key', $2, $3, 'active', true)
		RETURNING id::text
	`, projectID, keyPrefix, []byte{1, 2, 3}).Scan(&apiKeyID); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	recorder, err := NewUsageRecorder(databaseURL)
	if err != nil {
		t.Fatalf("create usage recorder: %v", err)
	}
	defer recorder.Close()

	ttft := 12
	latency := 34
	promptTokens := 7
	completionTokens := 3
	if ok := recorder.Enqueue(UsageEvent{
		RequestID:        requestID,
		APIKeyID:         apiKeyID,
		ModelPublicID:    publicModel,
		Status:           200,
		TTFTMS:           &ttft,
		LatencyMS:        &latency,
		PromptTokens:     &promptTokens,
		CompletionTokens: &completionTokens,
	}); !ok {
		t.Fatal("usage event was not queued")
	}
	if ok := recorder.TouchLastUsed(apiKeyID); !ok {
		t.Fatal("last-used update was not queued")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var status int
		var storedTTFT, storedLatency, storedPrompt, storedCompletion sql.NullInt64
		var storedModelID sql.NullString
		var lastUsed sql.NullTime

		eventErr := db.QueryRowContext(ctx, `
			SELECT status, ttft_ms, latency_ms, prompt_tokens, completion_tokens, model_id::text
			FROM usage_events
			WHERE request_id=$1
		`, requestID).Scan(
			&status,
			&storedTTFT,
			&storedLatency,
			&storedPrompt,
			&storedCompletion,
			&storedModelID,
		)
		keyErr := db.QueryRowContext(ctx,
			"SELECT last_used_at FROM api_keys WHERE id=$1",
			apiKeyID,
		).Scan(&lastUsed)

		if eventErr == nil && keyErr == nil && lastUsed.Valid {
			if status != 200 {
				t.Fatalf("expected status 200, got %d", status)
			}
			if !storedTTFT.Valid || storedTTFT.Int64 != 12 {
				t.Fatalf("unexpected ttft: %#v", storedTTFT)
			}
			if !storedLatency.Valid || storedLatency.Int64 != 34 {
				t.Fatalf("unexpected latency: %#v", storedLatency)
			}
			if !storedPrompt.Valid || storedPrompt.Int64 != 7 {
				t.Fatalf("unexpected prompt tokens: %#v", storedPrompt)
			}
			if !storedCompletion.Valid || storedCompletion.Int64 != 3 {
				t.Fatalf("unexpected completion tokens: %#v", storedCompletion)
			}
			if !storedModelID.Valid || storedModelID.String != modelID {
				t.Fatalf("unexpected model id: %#v expected %s", storedModelID, modelID)
			}
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("usage event or last_used_at was not persisted before timeout")
}
