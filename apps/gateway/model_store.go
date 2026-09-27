package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DiscoveredModel struct {
	UpstreamID    string
	DisplayName   string
	ContextLength int
	Capabilities  map[string]bool
}

type ModelCatalogStore struct {
	db *sql.DB
}

func NewModelCatalogStore(databaseURL string) (*ModelCatalogStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(getenvInt("CATALOG_DB_MAX_OPEN_CONNS", 4))
	db.SetMaxIdleConns(getenvInt("CATALOG_DB_MAX_IDLE_CONNS", 2))
	db.SetConnMaxLifetime(30 * time.Minute)

	return &ModelCatalogStore{db: db}, nil
}

func (s *ModelCatalogStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *ModelCatalogStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("model catalog store unavailable")
	}
	return s.db.PingContext(ctx)
}

func (s *ModelCatalogStore) Reconcile(
	ctx context.Context,
	providerKey string,
	discovered []DiscoveredModel,
) error {
	providerKey = strings.TrimSpace(providerKey)
	if providerKey == "" {
		return errors.New("provider key is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := ensureAutoFreeModel(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE models
		SET active=false,
		    updated_at=now()
		WHERE provider_key=$1
		  AND upstream_id<>$2
		  AND active=true
	`, providerKey, autoFreeUpstreamID); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(discovered))
	for _, candidate := range discovered {
		upstreamID := strings.TrimSpace(candidate.UpstreamID)
		if upstreamID == "" || upstreamID == autoFreeUpstreamID {
			continue
		}
		if _, exists := seen[upstreamID]; exists {
			continue
		}
		seen[upstreamID] = struct{}{}

		displayName := strings.TrimSpace(candidate.DisplayName)
		if displayName == "" {
			displayName = "Free Model"
		}
		capabilities := candidate.Capabilities
		if capabilities == nil {
			capabilities = map[string]bool{"text": true}
		}
		capabilitiesJSON, err := json.Marshal(capabilities)
		if err != nil {
			return err
		}

		var id string
		var currentPublicID string
		err = tx.QueryRowContext(ctx, `
			SELECT id::text, public_id
			FROM models
			WHERE upstream_id=$1
			FOR UPDATE
		`, upstreamID).Scan(&id, &currentPublicID)

		switch {
		case err == nil:
			publicID := currentPublicID
			if !isNexoraPublicModelID(publicID) {
				publicID = publicModelAlias(displayName, upstreamID)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE models
				SET public_id=$1,
				    display_name=$2,
				    provider_key=$3,
				    context_length=$4,
				    active=true,
				    is_free=true,
				    capabilities=$5::jsonb,
				    updated_at=now()
				WHERE id=$6::uuid
			`,
				publicID,
				displayName,
				providerKey,
				nullPositiveInt(candidate.ContextLength),
				string(capabilitiesJSON),
				id,
			); err != nil {
				return err
			}

		case errors.Is(err, sql.ErrNoRows):
			publicID := publicModelAlias(displayName, upstreamID)
			if _, err := tx.ExecContext(ctx, `
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
				VALUES ($1,$2,$3,$4,$5,true,true,$6::jsonb)
			`,
				publicID,
				upstreamID,
				displayName,
				providerKey,
				nullPositiveInt(candidate.ContextLength),
				string(capabilitiesJSON),
			); err != nil {
				return err
			}

		default:
			return err
		}
	}

	return tx.Commit()
}

func (s *ModelCatalogStore) ListActiveFree(ctx context.Context) ([]Model, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			public_id,
			upstream_id,
			display_name,
			provider_key,
			COALESCE(context_length, 0),
			capabilities
		FROM models
		WHERE active=true
		  AND is_free=true
		ORDER BY
			CASE WHEN public_id=$1 THEN 0 ELSE 1 END,
			public_id
	`, autoFreeModelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := make([]Model, 0)
	for rows.Next() {
		var model Model
		var capabilitiesJSON []byte
		if err := rows.Scan(
			&model.ID,
			&model.UpstreamID,
			&model.DisplayName,
			&model.ProviderKey,
			&model.ContextLength,
			&capabilitiesJSON,
		); err != nil {
			return nil, err
		}

		model.Object = "model"
		model.OwnedBy = nexoraOwnedBy
		model.Status = "active"
		model.Free = true
		model.Capabilities = map[string]bool{}
		if len(capabilitiesJSON) > 0 {
			if err := json.Unmarshal(capabilitiesJSON, &model.Capabilities); err != nil {
				return nil, err
			}
		}
		models = append(models, normalizeCatalogModel(model))
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return models, nil
}

func ensureAutoFreeModel(ctx context.Context, tx *sql.Tx) error {
	capabilitiesJSON, _ := json.Marshal(map[string]bool{
		"text":      true,
		"streaming": true,
	})

	var id string
	var publicID string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, public_id
		FROM models
		WHERE public_id=$1 OR upstream_id=$2
		ORDER BY CASE WHEN public_id=$1 THEN 0 ELSE 1 END
		LIMIT 1
		FOR UPDATE
	`, autoFreeModelID, autoFreeUpstreamID).Scan(&id, &publicID)

	switch {
	case err == nil:
		_, err = tx.ExecContext(ctx, `
			UPDATE models
			SET public_id=$1,
			    upstream_id=$2,
			    display_name='Auto Free',
			    provider_key=$3,
			    context_length=NULL,
			    active=true,
			    is_free=true,
			    capabilities=$4::jsonb,
			    updated_at=now()
			WHERE id=$5::uuid
		`,
			autoFreeModelID,
			autoFreeUpstreamID,
			openRouterProviderKey,
			string(capabilitiesJSON),
			id,
		)
		return err

	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
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
			VALUES ($1,$2,'Auto Free',$3,NULL,true,true,$4::jsonb)
		`,
			autoFreeModelID,
			autoFreeUpstreamID,
			openRouterProviderKey,
			string(capabilitiesJSON),
		)
		return err

	default:
		return err
	}
}

func isNexoraPublicModelID(value string) bool {
	value = strings.TrimSpace(value)
	return value == autoFreeModelID || strings.HasPrefix(value, "nexora/")
}

func nullPositiveInt(value int) any {
	if value > 0 {
		return value
	}
	return nil
}
