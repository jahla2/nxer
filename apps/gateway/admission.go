package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type AdmissionController struct {
	client *redis.Client
}

type AdmissionLease struct {
	controller *AdmissionController
	keyID      string
	projectID  string
	token      string
	released   bool
}

var admissionScript = redis.NewScript(`
local key_rpm = tonumber(ARGV[1])
local key_daily = tonumber(ARGV[2])
local key_concurrent = tonumber(ARGV[3])
local project_concurrent = tonumber(ARGV[4])
local global_rpm = tonumber(ARGV[5])
local global_daily = tonumber(ARGV[6])
local global_concurrent = tonumber(ARGV[7])
local minute_ttl = tonumber(ARGV[8])
local day_ttl = tonumber(ARGV[9])
local concurrency_ttl = tonumber(ARGV[10])
local now_ms = tonumber(ARGV[11])
local expiry_ms = tonumber(ARGV[12])
local lease_token = ARGV[13]

local function current_counter(key)
  local v = redis.call('GET', key)
  if not v then return 0 end
  return tonumber(v)
end

local function clean_concurrency(key)
  redis.call('ZREMRANGEBYSCORE', key, '-inf', now_ms)
end

clean_concurrency(KEYS[3])
clean_concurrency(KEYS[4])
clean_concurrency(KEYS[7])

if current_counter(KEYS[1]) >= key_rpm then return {0, 1} end
if current_counter(KEYS[2]) >= key_daily then return {0, 2} end
if redis.call('ZCARD', KEYS[3]) >= key_concurrent then return {0, 3} end
if redis.call('ZCARD', KEYS[4]) >= project_concurrent then return {0, 4} end
if current_counter(KEYS[5]) >= global_rpm then return {0, 5} end
if current_counter(KEYS[6]) >= global_daily then return {0, 6} end
if redis.call('ZCARD', KEYS[7]) >= global_concurrent then return {0, 7} end

local function increment_with_ttl(key, ttl)
  local v = redis.call('INCR', key)
  if v == 1 then redis.call('EXPIRE', key, ttl) end
end

local function add_lease(key)
  redis.call('ZADD', key, expiry_ms, lease_token)
  redis.call('EXPIRE', key, concurrency_ttl + 60)
end

increment_with_ttl(KEYS[1], minute_ttl)
increment_with_ttl(KEYS[2], day_ttl)
increment_with_ttl(KEYS[5], minute_ttl)
increment_with_ttl(KEYS[6], day_ttl)
add_lease(KEYS[3])
add_lease(KEYS[4])
add_lease(KEYS[7])
return {1, 0}
`)

var releaseScript = redis.NewScript(`
local lease_token = ARGV[1]
for _, key in ipairs(KEYS) do
  redis.call('ZREM', key, lease_token)
end
return 1
`)

func NewAdmissionController(redisURL string) (*AdmissionController, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis unavailable: %w", err)
	}
	return &AdmissionController{client: client}, nil
}

func (a *AdmissionController) Close() error { return a.client.Close() }
func (a *AdmissionController) RedisClient() *redis.Client { return a.client }

func (a *AdmissionController) Admit(ctx context.Context, p *APIKeyPrincipal) (*AdmissionLease, *APIError) {
	if p == nil {
		return nil, newAPIError(
			401,
			"authentication_error",
			"NEXORA_INVALID_API_KEY",
			"A valid Nexora API key is required.",
		)
	}

	token, err := newLeaseToken()
	if err != nil {
		return nil, newAPIError(
			503,
			"service_unavailable",
			"NEXORA_ADMISSION_UNAVAILABLE",
			"Unable to create request lease.",
		)
	}

	now := time.Now().UTC()
	minute := now.Format("200601021504")
	day := now.Format("20060102")
	keyRPM := principalLimit(p.RequestsPerMinute, "DEFAULT_KEY_RPM", 5)
	keyDaily := principalLimit(p.RequestsPerDay, "DEFAULT_KEY_DAILY", 10)
	keyConcurrent := principalLimit(p.MaxConcurrent, "DEFAULT_KEY_MAX_CONCURRENT", 2)
	projectConcurrent := getenvInt("DEFAULT_PROJECT_MAX_CONCURRENT", 4)
	globalRPM := getenvInt("GLOBAL_RPM_LIMIT", 18)
	globalDaily := getenvInt("GLOBAL_DAILY_LIMIT", 45)
	globalConcurrent := getenvInt("GLOBAL_MAX_CONCURRENT", 8)
	concurrencyTTL := getenvInt("CONCURRENCY_LEASE_TTL_SECONDS", 300)

	keys := []string{
		"nxa:rate:key:" + p.ID + ":" + minute,
		"nxa:daily:key:" + p.ID + ":" + day,
		"nxa:concurrent:v2:key:" + p.ID,
		"nxa:concurrent:v2:project:" + p.ProjectID,
		"nxa:rate:global:" + minute,
		"nxa:daily:global:" + day,
		"nxa:concurrent:v2:global",
	}
	nowMS := now.UnixMilli()
	expiryMS := now.Add(time.Duration(concurrencyTTL) * time.Second).UnixMilli()
	args := []any{
		keyRPM,
		keyDaily,
		keyConcurrent,
		projectConcurrent,
		globalRPM,
		globalDaily,
		globalConcurrent,
		120,
		172800,
		concurrencyTTL,
		nowMS,
		expiryMS,
		token,
	}

	result, err := admissionScript.Run(ctx, a.client, keys, args...).Slice()
	if err != nil {
		return nil, newAPIError(
			503,
			"service_unavailable",
			"NEXORA_ADMISSION_UNAVAILABLE",
			"Request admission service is unavailable.",
		)
	}
	if len(result) != 2 {
		return nil, newAPIError(
			503,
			"service_unavailable",
			"NEXORA_ADMISSION_UNAVAILABLE",
			"Request admission service returned an invalid result.",
		)
	}

	allowed, ok := result[0].(int64)
	if !ok {
		return nil, newAPIError(
			503,
			"service_unavailable",
			"NEXORA_ADMISSION_UNAVAILABLE",
			"Request admission service returned an invalid result.",
		)
	}
	if allowed != 1 {
		code := int64(0)
		if len(result) > 1 {
			code, _ = result[1].(int64)
		}
		return nil, admissionLimitError(code)
	}

	return &AdmissionLease{
		controller: a,
		keyID:      p.ID,
		projectID:  p.ProjectID,
		token:      token,
	}, nil
}

func (l *AdmissionLease) Release(ctx context.Context) {
	if l == nil || l.released {
		return
	}
	l.released = true
	keys := []string{
		"nxa:concurrent:v2:key:" + l.keyID,
		"nxa:concurrent:v2:project:" + l.projectID,
		"nxa:concurrent:v2:global",
	}
	_, _ = releaseScript.Run(ctx, l.controller.client, keys, l.token).Result()
}

func newLeaseToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func principalLimit(v sql.NullInt64, env string, fallback int) int {
	if v.Valid && v.Int64 > 0 {
		return int(v.Int64)
	}
	return getenvInt(env, fallback)
}

func admissionLimitError(code int64) *APIError {
	switch code {
	case 1:
		return newAPIError(429, "rate_limit_error", "NEXORA_KEY_RPM_LIMIT", "API key requests-per-minute limit exceeded.")
	case 2:
		return newAPIError(429, "rate_limit_error", "NEXORA_KEY_DAILY_LIMIT", "API key daily request limit exceeded.")
	case 3:
		return newAPIError(429, "rate_limit_error", "NEXORA_KEY_CONCURRENCY_LIMIT", "API key concurrent request limit exceeded.")
	case 4:
		return newAPIError(429, "rate_limit_error", "NEXORA_PROJECT_CONCURRENCY_LIMIT", "Project concurrent request limit exceeded.")
	case 5:
		return newAPIError(429, "rate_limit_error", "NEXORA_GLOBAL_RPM_LIMIT", "Gateway requests-per-minute safety limit exceeded.")
	case 6:
		return newAPIError(429, "rate_limit_error", "NEXORA_GLOBAL_DAILY_LIMIT", "Gateway daily safety limit exceeded.")
	case 7:
		return newAPIError(429, "rate_limit_error", "NEXORA_GLOBAL_CONCURRENCY_LIMIT", "Gateway concurrent request safety limit exceeded.")
	default:
		return newAPIError(429, "rate_limit_error", "NEXORA_RATE_LIMIT", "Request limit exceeded.")
	}
}
