package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const apiKeyPrefix = "nxa_live_"

type apiKeyContextKey struct{}

type APIKeyPrincipal struct {
	ID                 string
	ProjectID          string
	DefaultModelID     sql.NullString
	AllowAllFreeModels bool
	AllowedModels      map[string]struct{}
	RequestsPerMinute  sql.NullInt64
	RequestsPerDay     sql.NullInt64
	MaxConcurrent      sql.NullInt64
}

type APIKeyAuthenticator struct {
	db     *sql.DB
	pepper string
}

func NewAPIKeyAuthenticator(databaseURL, pepper string) (*APIKeyAuthenticator, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if strings.TrimSpace(pepper) == "" {
		return nil, errors.New("API_KEY_HASH_PEPPER is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(getenvInt("DB_MAX_OPEN_CONNS", 20))
	db.SetMaxIdleConns(getenvInt("DB_MAX_IDLE_CONNS", 5))
	db.SetConnMaxLifetime(30 * time.Minute)
	return &APIKeyAuthenticator{db: db, pepper: pepper}, nil
}

func (a *APIKeyAuthenticator) Close() error { return a.db.Close() }
func (a *APIKeyAuthenticator) Ping(ctx context.Context) error { return a.db.PingContext(ctx) }

func (a *APIKeyAuthenticator) Authenticate(ctx context.Context, rawKey string) (*APIKeyPrincipal, error) {
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return nil, sql.ErrNoRows
	}
	dot := strings.IndexByte(rawKey, '.')
	if dot <= len(apiKeyPrefix) || dot == len(rawKey)-1 {
		return nil, sql.ErrNoRows
	}
	prefix := rawKey[:dot]
	var principal APIKeyPrincipal
	var storedHash []byte

	err := a.db.QueryRowContext(ctx, `
		SELECT k.id::text, k.project_id::text, k.key_hash, k.default_model_id::text,
		       k.allow_all_free_models, k.requests_per_minute, k.requests_per_day, k.max_concurrent
		FROM api_keys k
		JOIN projects p ON p.id = k.project_id
		WHERE k.key_prefix = $1
		  AND k.status = 'active'
		  AND k.revoked_at IS NULL
		  AND (k.expires_at IS NULL OR k.expires_at > now())
		  AND p.status = 'active'
		LIMIT 1
	`, prefix).Scan(
		&principal.ID, &principal.ProjectID, &storedHash, &principal.DefaultModelID,
		&principal.AllowAllFreeModels, &principal.RequestsPerMinute, &principal.RequestsPerDay,
		&principal.MaxConcurrent,
	)
	if err != nil {
		return nil, err
	}

	actual := hashAPIKey(rawKey, a.pepper)
	if !hmac.Equal(actual, storedHash) {
		return nil, sql.ErrNoRows
	}

	principal.AllowedModels = make(map[string]struct{})
	if principal.AllowAllFreeModels {
		return &principal, nil
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT m.public_id
		FROM api_key_model_scopes s
		JOIN models m ON m.id = s.model_id
		WHERE s.api_key_id = $1
		  AND m.active = true
		  AND m.is_free = true

		UNION

		SELECT m.public_id
		FROM models m
		WHERE $2::uuid IS NOT NULL
		  AND m.id = $2::uuid
		  AND m.active = true
		  AND m.is_free = true
	`, principal.ID, principal.DefaultModelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var publicID string
		if err := rows.Scan(&publicID); err != nil {
			return nil, err
		}
		publicID = strings.TrimSpace(publicID)
		if publicID != "" {
			principal.AllowedModels[publicID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &principal, nil
}

func (p *APIKeyPrincipal) AllowsModel(modelID string) bool {
	if p == nil {
		return false
	}
	if p.AllowAllFreeModels {
		return true
	}
	_, ok := p.AllowedModels[strings.TrimSpace(modelID)]
	return ok
}

func hashAPIKey(rawKey, pepper string) []byte {
	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(rawKey))
	return mac.Sum(nil)
}

func extractBearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func authMiddleware(authenticator *APIKeyAuthenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawKey := extractBearerToken(r)
		if rawKey == "" {
			writeAPIError(w, newAPIError(http.StatusUnauthorized, "authentication_error", "NEXORA_INVALID_API_KEY", "A valid Nexora API key is required."))
			return
		}
		principal, err := authenticator.Authenticate(r.Context(), rawKey)
		if err != nil {
			writeAPIError(w, newAPIError(http.StatusUnauthorized, "authentication_error", "NEXORA_INVALID_API_KEY", "A valid Nexora API key is required."))
			return
		}
		ctx := context.WithValue(r.Context(), apiKeyContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func apiKeyFingerprint(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:6])
}
