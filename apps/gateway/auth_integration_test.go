package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAuthenticateEnforcesActiveProjectAndModelScope(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("DATABASE_URL not configured; skipping PostgreSQL integration test")
	}
	pepper := os.Getenv("API_KEY_HASH_PEPPER")
	if strings.TrimSpace(pepper) == "" {
		t.Fatal("API_KEY_HASH_PEPPER is required for integration test")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%012x", time.Now().UnixNano()&0xffffffffffff)
	email := "gateway-auth-" + suffix + "@example.test"
	publicModel := "nexora/test-" + suffix
	upstreamModel := "test/upstream-" + suffix
	rawKey := "nxa_live_" + suffix + ".integration-secret-" + suffix
	prefix := rawKey[:strings.IndexByte(rawKey, '.')]

	var userID, projectID, modelID, keyID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, display_name, status)
		VALUES ($1, 'integration-test-only', 'Gateway Auth Test', 'active')
		RETURNING id::text
	`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if keyID != "" {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM api_key_model_scopes WHERE api_key_id=$1", keyID)
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id=$1", keyID)
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
		VALUES ($1, 'Gateway Auth Project', 'active')
		RETURNING id::text
	`, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO models (public_id, upstream_id, display_name, active, is_free)
		VALUES ($1, $2, 'Gateway Auth Model', true, true)
		RETURNING id::text
	`, publicModel, upstreamModel).Scan(&modelID); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO api_keys (
			project_id, name, key_prefix, key_hash, status,
			allow_all_free_models
		)
		VALUES ($1, 'Gateway Auth Key', $2, $3, 'active', false)
		RETURNING id::text
	`, projectID, prefix, hashAPIKey(rawKey, pepper)).Scan(&keyID); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		"INSERT INTO api_key_model_scopes (api_key_id, model_id) VALUES ($1,$2)",
		keyID, modelID,
	); err != nil {
		t.Fatalf("insert model scope: %v", err)
	}

	authenticator, err := NewAPIKeyAuthenticator(databaseURL, pepper)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	defer authenticator.Close()

	principal, err := authenticator.Authenticate(ctx, rawKey)
	if err != nil {
		t.Fatalf("authenticate active scoped key: %v", err)
	}
	if !principal.AllowsModel(publicModel) {
		t.Fatalf("expected scoped model %q to be allowed", publicModel)
	}
	if principal.AllowsModel("nexora/not-allowed") {
		t.Fatal("unexpected access to model outside API key scope")
	}

	if _, err := db.ExecContext(ctx, "UPDATE projects SET status='archived' WHERE id=$1", projectID); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	_, err = authenticator.Authenticate(ctx, rawKey)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("archived project must invalidate API key authentication; got %v", err)
	}
}
