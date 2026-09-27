package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
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
	once       sync.Once
}

var admissionScript = redis.NewScript(`
local global_daily = tonumber(ARGV[1])
local user_daily = tonumber(ARGV[2])
local project_daily = tonumber(ARGV[3])
local key_daily = tonumber(ARGV[4])
local global_rpm = tonumber(ARGV[5])
local user_rpm = tonumber(ARGV[6])
local project_rpm = tonumber(ARGV[7])
local key_rpm = tonumber(ARGV[8])
local ip_rpm = tonumber(ARGV[9])
local global_concurrent = tonumber(ARGV[10])
local project_concurrent = tonumber(ARGV[11])
local key_concurrent = tonumber(ARGV[12])
local minute_ttl = tonumber(ARGV[13])
local day_ttl = tonumber(ARGV[14])
local concurrency_ttl = tonumber(ARGV[15])

local function current(key)
  local v = redis.call('GET', key)
  if not v then return 0 end
  return tonumber(v)
end

if current(KEYS[1]) >= global_daily then return {0, 1} end
if current(KEYS[2]) >= user_daily then return {0, 2} end
if current(KEYS[3]) >= project_daily then return {0, 3} end
if current(KEYS[4]) >= key_daily then return {0, 4} end

if current(KEYS[5]) >= global_rpm then return {0, 5} end
if current(KEYS[6]) >= user_rpm then return {0, 6} end
if current(KEYS[7]) >= project_rpm then return {0, 7} end
if current(KEYS[8]) >= key_rpm then return {0, 8} end
if current(KEYS[9]) >= ip_rpm then return {0, 9} end

if current(KEYS[10]) >= global_concurrent then return {0, 10} end
if current(KEYS[11]) >= project_concurrent then return {0, 11} end
if current(KEYS[12]) >= key_concurrent then return {0, 12} end

local function increment_with_ttl(key, ttl)
  local v = redis.call('INCR', key)
  if v == 1 then redis.call('EXPIRE', key, ttl) end
end

for i = 1, 4 do
  increment_with_ttl(KEYS[i], day_ttl)
end
for i = 5, 9 do
  increment_with_ttl(KEYS[i], minute_ttl)
end
for i = 10, 12 do
  increment_with_ttl(KEYS[i], concurrency_ttl)
end

return {1, 0}
`)

var releaseScript = redis.NewScript(`
for _, key in ipairs(KEYS) do
  local v = redis.call('GET', key)
  if v and tonumber(v) > 0 then
    redis.call('DECR', key)
  end
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

func (a *AdmissionController) Admit(ctx context.Context, p *APIKeyPrincipal, clientIP string) (*AdmissionLease, *APIError) {
	if p == nil {
		return nil, newAPIError(
			401,
			"authentication_error",
			"NEXORA_INVALID_API_KEY",
			"A valid Nexora API key is required.",
		)
	}

	now := time.Now().UTC()
	minute := now.Format("200601021504")
	day := now.Format("20060102")

	globalDaily := getenvInt("GLOBAL_DAILY_LIMIT", 45)
	userDaily := getenvInt("DEFAULT_USER_DAILY", 20)
	projectDaily := getenvInt("DEFAULT_PROJECT_DAILY", 15)
	keyDaily := principalLimit(p.RequestsPerDay, "DEFAULT_KEY_DAILY", 10)

	globalRPM := getenvInt("GLOBAL_RPM_LIMIT", 18)
	userRPM := getenvInt("DEFAULT_USER_RPM", 10)
	projectRPM := getenvInt("DEFAULT_PROJECT_RPM", 8)
	keyRPM := principalLimit(p.RequestsPerMinute, "DEFAULT_KEY_RPM", 5)
	ipRPM := getenvInt("DEFAULT_IP_RPM", 20)

	globalConcurrent := getenvInt("GLOBAL_MAX_CONCURRENT", 8)
	projectConcurrent := getenvInt("DEFAULT_PROJECT_MAX_CONCURRENT", 4)
	keyConcurrent := principalLimit(p.MaxConcurrent, "DEFAULT_KEY_MAX_CONCURRENT", 2)

	keys := []string{
		"nxa:daily:global:" + day,
		"nxa:daily:user:" + p.UserID + ":" + day,
		"nxa:daily:project:" + p.ProjectID + ":" + day,
		"nxa:daily:key:" + p.ID + ":" + day,
		"nxa:rate:global:" + minute,
		"nxa:rate:user:" + p.UserID + ":" + minute,
		"nxa:rate:project:" + p.ProjectID + ":" + minute,
		"nxa:rate:key:" + p.ID + ":" + minute,
		"nxa:rate:ip:" + ipBucketID(clientIP) + ":" + minute,
		"nxa:concurrent:global",
		"nxa:concurrent:project:" + p.ProjectID,
		"nxa:concurrent:key:" + p.ID,
	}
	args := []any{
		globalDaily, userDaily, projectDaily, keyDaily,
		globalRPM, userRPM, projectRPM, keyRPM, ipRPM,
		globalConcurrent, projectConcurrent, keyConcurrent,
		120, 172800, getenvInt("CONCURRENCY_LEASE_TTL_SECONDS", 300),
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
	return &AdmissionLease{controller: a, keyID: p.ID, projectID: p.ProjectID}, nil
}

func (l *AdmissionLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		keys := []string{
			"nxa:concurrent:global",
			"nxa:concurrent:project:" + l.projectID,
			"nxa:concurrent:key:" + l.keyID,
		}
		_, _ = releaseScript.Run(ctx, l.controller.client, keys).Result()
	})
}

func principalLimit(v sql.NullInt64, env string, fallback int) int {
	if v.Valid && v.Int64 > 0 {
		return int(v.Int64)
	}
	return getenvInt(env, fallback)
}

func ipBucketID(clientIP string) string {
	sum := sha256.Sum256([]byte(clientIP))
	return hex.EncodeToString(sum[:8])
}

func admissionLimitError(code int64) *APIError {
	switch code {
	case 1:
		return newAPIError(429, "rate_limit_error", "NEXORA_GLOBAL_DAILY_LIMIT", "Gateway daily safety limit exceeded.")
	case 2:
		return newAPIError(429, "rate_limit_error", "NEXORA_USER_DAILY_LIMIT", "User daily request limit exceeded.")
	case 3:
		return newAPIError(429, "rate_limit_error", "NEXORA_PROJECT_DAILY_LIMIT", "Project daily request limit exceeded.")
	case 4:
		return newAPIError(429, "rate_limit_error", "NEXORA_KEY_DAILY_LIMIT", "API key daily request limit exceeded.")
	case 5:
		return newAPIError(429, "rate_limit_error", "NEXORA_GLOBAL_RPM_LIMIT", "Gateway requests-per-minute safety limit exceeded.")
	case 6:
		return newAPIError(429, "rate_limit_error", "NEXORA_USER_RPM_LIMIT", "User requests-per-minute limit exceeded.")
	case 7:
		return newAPIError(429, "rate_limit_error", "NEXORA_PROJECT_RPM_LIMIT", "Project requests-per-minute limit exceeded.")
	case 8:
		return newAPIError(429, "rate_limit_error", "NEXORA_KEY_RPM_LIMIT", "API key requests-per-minute limit exceeded.")
	case 9:
		return newAPIError(429, "rate_limit_error", "NEXORA_IP_RPM_LIMIT", "Client requests-per-minute limit exceeded.")
	case 10:
		return newAPIError(429, "rate_limit_error", "NEXORA_GLOBAL_CONCURRENCY_LIMIT", "Gateway concurrent request safety limit exceeded.")
	case 11:
		return newAPIError(429, "rate_limit_error", "NEXORA_PROJECT_CONCURRENCY_LIMIT", "Project concurrent request limit exceeded.")
	case 12:
		return newAPIError(429, "rate_limit_error", "NEXORA_KEY_CONCURRENCY_LIMIT", "API key concurrent request limit exceeded.")
	default:
		return newAPIError(429, "rate_limit_error", "NEXORA_RATE_LIMIT", "Request limit exceeded.")
	}
}
