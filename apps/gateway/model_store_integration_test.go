package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModelCatalogStoreReconcilesProviderNeutralAliases(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("DATABASE_URL not configured; skipping PostgreSQL model catalog integration test")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%012x", time.Now().UnixNano()&0xffffffffffff)
	providerKey := "test-" + suffix[:8]
	upstreamID := "private-provider/model-" + suffix
	legacyPublicID := "private-provider/model-" + suffix
	displayName := "Premium Free Text " + suffix
	capabilities, _ := json.Marshal(map[string]bool{"text": true})

	var originalID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO models (
			public_id,
			upstream_id,
			display_name,
			provider_key,
			context_length,
			active,
			is_free,
			capabilities
		)
		VALUES ($1,$2,$3,$4,4096,true,true,$5::jsonb)
		RETURNING id::text
	`,
		legacyPublicID,
		upstreamID,
		displayName,
		providerKey,
		string(capabilities),
	).Scan(&originalID); err != nil {
		t.Fatalf("insert legacy model: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM models WHERE upstream_id=$1", upstreamID)
	})

	store, err := NewModelCatalogStore(databaseURL)
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}
	defer store.Close()

	if err := store.Reconcile(ctx, providerKey, []DiscoveredModel{
		{
			UpstreamID:    upstreamID,
			DisplayName:   displayName,
			ContextLength: 8192,
			Capabilities: map[string]bool{
				"text":      true,
				"streaming": true,
			},
		},
	}); err != nil {
		t.Fatalf("reconcile catalog: %v", err)
	}

	var storedID, storedPublicID, storedProvider string
	var storedContext int
	if err := db.QueryRowContext(ctx, `
		SELECT id::text, public_id, provider_key, context_length
		FROM models
		WHERE upstream_id=$1
	`, upstreamID).Scan(
		&storedID,
		&storedPublicID,
		&storedProvider,
		&storedContext,
	); err != nil {
		t.Fatalf("load reconciled model: %v", err)
	}

	if storedID != originalID {
		t.Fatalf("reconcile must preserve model UUID for API-key scopes: original=%s stored=%s", originalID, storedID)
	}
	if !strings.HasPrefix(storedPublicID, "nexora/") {
		t.Fatalf("legacy upstream-facing public id was not replaced: %q", storedPublicID)
	}
	if strings.Contains(strings.ToLower(storedPublicID), "private-provider") ||
		strings.Contains(strings.ToLower(storedPublicID), "openrouter") {
		t.Fatalf("provider identity leaked through public id: %q", storedPublicID)
	}
	if storedProvider != providerKey {
		t.Fatalf("unexpected provider key %q", storedProvider)
	}
	if storedContext != 8192 {
		t.Fatalf("expected refreshed context length 8192, got %d", storedContext)
	}

	models, err := store.ListActiveFree(ctx)
	if err != nil {
		t.Fatalf("list active free models: %v", err)
	}

	var found *Model
	for i := range models {
		if models[i].UpstreamID == upstreamID {
			found = &models[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("reconciled model missing from active catalog: %#v", models)
	}
	if found.ID != storedPublicID ||
		found.ProviderKey != providerKey ||
		found.UpstreamID != upstreamID ||
		found.OwnedBy != nexoraOwnedBy {
		t.Fatalf("catalog routing/public metadata mismatch: %#v", found)
	}

	if err := store.Reconcile(ctx, providerKey, nil); err != nil {
		t.Fatalf("reconcile stale catalog: %v", err)
	}
	var active bool
	if err := db.QueryRowContext(ctx,
		"SELECT active FROM models WHERE upstream_id=$1",
		upstreamID,
	).Scan(&active); err != nil {
		t.Fatalf("load stale model state: %v", err)
	}
	if active {
		t.Fatal("stale provider model should be marked inactive")
	}
}


func TestGatewayReloadsCatalogFromDatabaseWithoutProviderSync(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("DATABASE_URL not configured; skipping PostgreSQL catalog reload test")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%012x", time.Now().UnixNano()&0xffffffffffff)
	publicID := "nexora/reload-" + suffix
	upstreamID := "provider/reload-" + suffix

	if _, err := db.ExecContext(ctx, `
		INSERT INTO models (
			public_id, upstream_id, display_name, provider_key,
			context_length, active, is_free, capabilities
		)
		VALUES ($1,$2,'Reload Test','openrouter',4096,true,true,'{"text":true}'::jsonb)
	`, publicID, upstreamID); err != nil {
		t.Fatalf("insert reload model: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM models WHERE upstream_id=$1", upstreamID)
	})

	store, err := NewModelCatalogStore(databaseURL)
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}
	defer store.Close()

	catalog := NewModelCatalog()
	if _, ok := catalog.Get(publicID); ok {
		t.Fatalf("test model unexpectedly existed before reload")
	}

	if err := loadModelCatalogFromStore(ctx, store, catalog); err != nil {
		t.Fatalf("reload catalog from database: %v", err)
	}

	reloaded, ok := catalog.Get(publicID)
	if !ok {
		t.Fatalf("database model was not loaded into gateway catalog")
	}
	if reloaded.UpstreamID != upstreamID || reloaded.ID != publicID {
		t.Fatalf("unexpected reloaded route: %#v", reloaded)
	}
}
