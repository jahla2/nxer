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
	UserID             string
	DefaultModelID     sql.NullString
	AllowAllFreeModels bool
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

func parseAPIKeyPrefix(rawKey string) (string, bool) {
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return "", false
	}
	dot := strings.IndexByte(rawKey, '.')
	if dot < 0 {
		return "", false
	}
	publicPart := rawKey[len(apiKeyPrefix):dot]
	secretPart := rawKey[dot+1:]
	if len(publicPart) != 12 || len(secretPart) < 43 {
		return "", false
	}
	if _, err := hex.DecodeString(publicPart); err != nil {
		return "", false
	}
	for _, ch := range secretPart {
		if !(ch >= 'a' && ch <= 'z') &&
			!(ch >= 'A' && ch <= 'Z') &&
			!(ch >= '0' && ch <= '9') &&
			ch != '-' && ch != '_' {
			return "", false
		}
	}
	return rawKey[:dot], true
}

func (a *APIKeyAuthenticator) Authenticate(ctx context.Context, rawKey string) (*APIKeyPrincipal, error) {
	prefix, ok := parseAPIKeyPrefix(rawKey)
	if !ok {
		return nil, sql.ErrNoRows
	}

	var principal APIKeyPrincipal
	var storedHash []byte
	err := a.db.QueryRowContext(ctx, `
		SELECT
			k.id::text,
			k.project_id::text,
			p.user_id::text,
			k.key_hash,
			k.default_model_id::text,
			k.allow_all_free_models,
			k.requests_per_minute,
			k.requests_per_day,
			k.max_concurrent
		FROM api_keys k
		JOIN projects p ON p.id = k.project_id
		JOIN users u ON u.id = p.user_id
		WHERE k.key_prefix = $1
		  AND k.status = 'active'
		  AND k.revoked_at IS NULL
		  AND (k.expires_at IS NULL OR k.expires_at > now())
		  AND p.status = 'active'
		  AND u.status = 'active'
		LIMIT 1
	`, prefix).Scan(
		&principal.ID,
		&principal.ProjectID,
		&principal.UserID,
		&storedHash,
		&principal.DefaultModelID,
		&principal.AllowAllFreeModels,
		&principal.RequestsPerMinute,
		&principal.RequestsPerDay,
		&principal.MaxConcurrent,
	)
	if err != nil {
		return nil, err
	}
	actual := hashAPIKey(rawKey, a.pepper)
	if !hmac.Equal(actual, storedHash) {
		return nil, sql.ErrNoRows
	}
	return &principal, nil
}

func (a *APIKeyAuthenticator) AuthorizeModel(ctx context.Context, principal *APIKeyPrincipal, publicModelID string) error {
	if principal == nil {
		return sql.ErrNoRows
	}
	if principal.AllowAllFreeModels {
		return nil
	}
	var allowed bool
	err := a.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM api_key_model_scopes s
			JOIN models m ON m.id = s.model_id
			WHERE s.api_key_id = $1
			  AND m.public_id = $2
			  AND m.active = true
			  AND m.is_free = true
		)
	`, principal.ID, publicModelID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return sql.ErrNoRows
	}
	return nil
}

func (a *APIKeyAuthenticator) AuthorizedModelIDs(ctx context.Context, principal *APIKeyPrincipal) (map[string]struct{}, error) {
	if principal == nil || principal.AllowAllFreeModels {
		return nil, nil
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT m.public_id
		FROM api_key_model_scopes s
		JOIN models m ON m.id = s.model_id
		WHERE s.api_key_id = $1
		  AND m.active = true
		  AND m.is_free = true
	`, principal.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]struct{}{}
	for rows.Next() {
		var publicID string
		if err := rows.Scan(&publicID); err != nil {
			return nil, err
		}
		result[publicID] = struct{}{}
	}
	return result, rows.Err()
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
			writeAPIError(w, newAPIError(
				http.StatusUnauthorized,
				"authentication_error",
				"NEXORA_INVALID_API_KEY",
				"A valid Nexora API key is required.",
			))
			return
		}

		principal, err := authenticator.Authenticate(r.Context(), rawKey)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeAPIError(w, newAPIError(
					http.StatusUnauthorized,
					"authentication_error",
					"NEXORA_INVALID_API_KEY",
					"A valid Nexora API key is required.",
				))
				return
			}
			writeAPIError(w, newAPIError(
				http.StatusServiceUnavailable,
				"service_unavailable",
				"NEXORA_AUTH_UNAVAILABLE",
				"API key authentication is temporarily unavailable.",
			))
			return
		}

		if telemetry := telemetryFromContext(r.Context()); telemetry != nil {
			telemetry.KeyID = principal.ID
			telemetry.ProjectID = principal.ProjectID
			telemetry.UserID = principal.UserID
		}

		ctx := context.WithValue(r.Context(), apiKeyContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func apiKeyFingerprint(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:6])
}
