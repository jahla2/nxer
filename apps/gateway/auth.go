package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

const (
	apiKeyPrefix        = "nxa_live_"
	apiKeyAuthCacheBase = "nxa:auth:key:"
)

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
	ExpiresAt          sql.NullTime
}

type cachedAPIKeyPrincipal struct {
	ID                 string   `json:"id"`
	ProjectID          string   `json:"project_id"`
	KeyHashHex         string   `json:"key_hash"`
	DefaultModelID     *string  `json:"default_model_id,omitempty"`
	AllowAllFreeModels bool     `json:"allow_all_free_models"`
	AllowedModels      []string `json:"allowed_models,omitempty"`
	RequestsPerMinute  *int64   `json:"requests_per_minute,omitempty"`
	RequestsPerDay     *int64   `json:"requests_per_day,omitempty"`
	MaxConcurrent      *int64   `json:"max_concurrent,omitempty"`
	ExpiresAtUnix      *int64   `json:"expires_at_unix,omitempty"`
}

type APIKeyAuthenticator struct {
	db       *sql.DB
	redis    *redis.Client
	pepper   string
	cacheTTL time.Duration
}

func NewAPIKeyAuthenticator(databaseURL, redisURL, pepper string) (*APIKeyAuthenticator, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if strings.TrimSpace(redisURL) == "" {
		return nil, errors.New("REDIS_URL is required")
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

	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &APIKeyAuthenticator{
		db:       db,
		redis:    redis.NewClient(redisOptions),
		pepper:   pepper,
		cacheTTL: time.Duration(getenvInt("API_KEY_AUTH_CACHE_TTL_SECONDS", 300)) * time.Second,
	}, nil
}

func (a *APIKeyAuthenticator) Close() error {
	dbErr := a.db.Close()
	redisErr := a.redis.Close()
	if dbErr != nil {
		return dbErr
	}
	return redisErr
}

func (a *APIKeyAuthenticator) Ping(ctx context.Context) error { return a.db.PingContext(ctx) }

func (a *APIKeyAuthenticator) Authenticate(ctx context.Context, rawKey string) (*APIKeyPrincipal, error) {
	prefix, ok := parseAPIKeyPrefix(rawKey)
	if !ok {
		return nil, sql.ErrNoRows
	}

	if principal, storedHash, cacheHit := a.readCache(ctx, prefix); cacheHit {
		if principal.ExpiresAt.Valid && !principal.ExpiresAt.Time.After(time.Now().UTC()) {
			_ = a.InvalidatePrefix(context.WithoutCancel(ctx), prefix)
			return nil, sql.ErrNoRows
		}
		if !hmac.Equal(hashAPIKey(rawKey, a.pepper), storedHash) {
			return nil, sql.ErrNoRows
		}
		return principal, nil
	}

	principal, storedHash, err := a.loadFromDatabase(ctx, prefix)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(hashAPIKey(rawKey, a.pepper), storedHash) {
		return nil, sql.ErrNoRows
	}

	a.writeCache(context.WithoutCancel(ctx), prefix, principal, storedHash)
	return principal, nil
}

func parseAPIKeyPrefix(rawKey string) (string, bool) {
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return "", false
	}
	dot := strings.IndexByte(rawKey, '.')
	if dot <= len(apiKeyPrefix) || dot == len(rawKey)-1 {
		return "", false
	}
	return rawKey[:dot], true
}

func (a *APIKeyAuthenticator) loadFromDatabase(ctx context.Context, prefix string) (*APIKeyPrincipal, []byte, error) {
	var principal APIKeyPrincipal
	var storedHash []byte

	err := a.db.QueryRowContext(ctx, `
		SELECT k.id::text, k.project_id::text, k.key_hash, k.default_model_id::text,
		       k.allow_all_free_models, k.requests_per_minute, k.requests_per_day,
		       k.max_concurrent, k.expires_at
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
		&principal.MaxConcurrent, &principal.ExpiresAt,
	)
	if err != nil {
		return nil, nil, err
	}

	principal.AllowedModels = make(map[string]struct{})
	if principal.AllowAllFreeModels {
		return &principal, storedHash, nil
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
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var publicID string
		if err := rows.Scan(&publicID); err != nil {
			return nil, nil, err
		}
		publicID = strings.TrimSpace(publicID)
		if publicID != "" {
			principal.AllowedModels[publicID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return &principal, storedHash, nil
}

func authCacheKey(prefix string) string {
	return apiKeyAuthCacheBase + prefix
}

func (a *APIKeyAuthenticator) readCache(ctx context.Context, prefix string) (*APIKeyPrincipal, []byte, bool) {
	payload, err := a.redis.Get(ctx, authCacheKey(prefix)).Bytes()
	if err != nil {
		return nil, nil, false
	}

	var cached cachedAPIKeyPrincipal
	if err := json.Unmarshal(payload, &cached); err != nil {
		_ = a.redis.Del(context.WithoutCancel(ctx), authCacheKey(prefix)).Err()
		return nil, nil, false
	}

	storedHash, err := hex.DecodeString(cached.KeyHashHex)
	if err != nil || len(storedHash) == 0 {
		_ = a.redis.Del(context.WithoutCancel(ctx), authCacheKey(prefix)).Err()
		return nil, nil, false
	}

	principal := cached.toPrincipal()
	return principal, storedHash, true
}

func (a *APIKeyAuthenticator) writeCache(ctx context.Context, prefix string, principal *APIKeyPrincipal, storedHash []byte) {
	if principal == nil || len(storedHash) == 0 {
		return
	}

	ttl := a.cacheTTL
	if principal.ExpiresAt.Valid {
		untilExpiry := time.Until(principal.ExpiresAt.Time)
		if untilExpiry <= 0 {
			return
		}
		if untilExpiry < ttl {
			ttl = untilExpiry
		}
	}
	if ttl <= 0 {
		return
	}

	cached := cacheRecordFromPrincipal(principal, storedHash)
	payload, err := json.Marshal(cached)
	if err != nil {
		return
	}
	_ = a.redis.Set(ctx, authCacheKey(prefix), payload, ttl).Err()
}

func (a *APIKeyAuthenticator) InvalidatePrefix(ctx context.Context, prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	return a.redis.Del(ctx, authCacheKey(prefix)).Err()
}

func cacheRecordFromPrincipal(principal *APIKeyPrincipal, storedHash []byte) cachedAPIKeyPrincipal {
	record := cachedAPIKeyPrincipal{
		ID:                 principal.ID,
		ProjectID:          principal.ProjectID,
		KeyHashHex:         hex.EncodeToString(storedHash),
		AllowAllFreeModels: principal.AllowAllFreeModels,
		AllowedModels:      make([]string, 0, len(principal.AllowedModels)),
	}
	if principal.DefaultModelID.Valid {
		value := principal.DefaultModelID.String
		record.DefaultModelID = &value
	}
	if principal.RequestsPerMinute.Valid {
		value := principal.RequestsPerMinute.Int64
		record.RequestsPerMinute = &value
	}
	if principal.RequestsPerDay.Valid {
		value := principal.RequestsPerDay.Int64
		record.RequestsPerDay = &value
	}
	if principal.MaxConcurrent.Valid {
		value := principal.MaxConcurrent.Int64
		record.MaxConcurrent = &value
	}
	if principal.ExpiresAt.Valid {
		value := principal.ExpiresAt.Time.Unix()
		record.ExpiresAtUnix = &value
	}
	for modelID := range principal.AllowedModels {
		record.AllowedModels = append(record.AllowedModels, modelID)
	}
	return record
}

func (cached cachedAPIKeyPrincipal) toPrincipal() *APIKeyPrincipal {
	principal := &APIKeyPrincipal{
		ID:                 cached.ID,
		ProjectID:          cached.ProjectID,
		AllowAllFreeModels: cached.AllowAllFreeModels,
		AllowedModels:      make(map[string]struct{}, len(cached.AllowedModels)),
	}
	if cached.DefaultModelID != nil {
		principal.DefaultModelID = sql.NullString{String: *cached.DefaultModelID, Valid: true}
	}
	if cached.RequestsPerMinute != nil {
		principal.RequestsPerMinute = sql.NullInt64{Int64: *cached.RequestsPerMinute, Valid: true}
	}
	if cached.RequestsPerDay != nil {
		principal.RequestsPerDay = sql.NullInt64{Int64: *cached.RequestsPerDay, Valid: true}
	}
	if cached.MaxConcurrent != nil {
		principal.MaxConcurrent = sql.NullInt64{Int64: *cached.MaxConcurrent, Valid: true}
	}
	if cached.ExpiresAtUnix != nil {
		principal.ExpiresAt = sql.NullTime{Time: time.Unix(*cached.ExpiresAtUnix, 0).UTC(), Valid: true}
	}
	for _, modelID := range cached.AllowedModels {
		if modelID = strings.TrimSpace(modelID); modelID != "" {
			principal.AllowedModels[modelID] = struct{}{}
		}
	}
	return principal
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
			writeRequestAPIError(w, r, newAPIError(http.StatusUnauthorized, "authentication_error", "NEXORA_INVALID_API_KEY", "A valid Nexora API key is required."))
			return
		}
		principal, err := authenticator.Authenticate(r.Context(), rawKey)
		if err != nil {
			writeRequestAPIError(w, r, newAPIError(http.StatusUnauthorized, "authentication_error", "NEXORA_INVALID_API_KEY", "A valid Nexora API key is required."))
			return
		}
		if trace := requestTraceFromContext(r.Context()); trace != nil {
			trace.APIKeyID = principal.ID
			trace.ProjectID = principal.ProjectID
		}
		if usageRecorder != nil && !usageRecorder.TouchLastUsed(principal.ID) {
			gatewayLogger.Warn(
				"last-used queue full",
				"event", "last_used_enqueue_dropped",
				"api_key_id", principal.ID,
			)
		}
		ctx := context.WithValue(r.Context(), apiKeyContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func apiKeyFingerprint(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:6])
}
