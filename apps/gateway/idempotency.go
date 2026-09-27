package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const idempotencyKeyPrefix = "nxa:idempotency:"

type IdempotencyGuard struct {
	client      *redis.Client
	ttl         time.Duration
	failedTTL   time.Duration
	maxResponse int
}

type IdempotencyDecision struct {
	Reservation *IdempotencyReservation
	Replay      *CachedIdempotentResponse
}

type IdempotencyReservation struct {
	guard  *IdempotencyGuard
	key    string
	digest string
	stream bool
	active bool
}

type CachedIdempotentResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

type BufferedResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

var beginIdempotencyScript = redis.NewScript(`
local key = KEYS[1]
local digest = ARGV[1]
local stream = ARGV[2]
local ttl = tonumber(ARGV[3])

if redis.call('EXISTS', key) == 0 then
  redis.call('HSET', key,
    'digest', digest,
    'state', 'processing',
    'stream', stream
  )
  redis.call('EXPIRE', key, ttl)
  return {1}
end

local existing_digest = redis.call('HGET', key, 'digest')
if existing_digest ~= digest then
  return {2}
end

local state = redis.call('HGET', key, 'state')
if state == 'failed' then
  redis.call('HSET', key,
    'state', 'processing',
    'stream', stream
  )
  redis.call('HDEL', key, 'status', 'content_type', 'body')
  redis.call('EXPIRE', key, ttl)
  return {1}
end

if state == 'processing' then
  return {3}
end

if state == 'completed' then
  local existing_stream = redis.call('HGET', key, 'stream')
  if existing_stream == '1' then
    return {4}
  end
  return {
    5,
    redis.call('HGET', key, 'status') or '200',
    redis.call('HGET', key, 'content_type') or 'application/json',
    redis.call('HGET', key, 'body') or ''
  }
end

return {6}
`)

var completeIdempotencyScript = redis.NewScript(`
local key = KEYS[1]
local digest = ARGV[1]
local status = ARGV[2]
local content_type = ARGV[3]
local body = ARGV[4]
local stream = ARGV[5]
local ttl = tonumber(ARGV[6])

if redis.call('HGET', key, 'digest') ~= digest then
  return 0
end
if redis.call('HGET', key, 'state') ~= 'processing' then
  return 0
end

redis.call('HSET', key,
  'state', 'completed',
  'status', status,
  'content_type', content_type,
  'body', body,
  'stream', stream
)
redis.call('EXPIRE', key, ttl)
return 1
`)

var failIdempotencyScript = redis.NewScript(`
local key = KEYS[1]
local digest = ARGV[1]
local ttl = tonumber(ARGV[2])

if redis.call('HGET', key, 'digest') ~= digest then
  return 0
end
if redis.call('HGET', key, 'state') ~= 'processing' then
  return 0
end

redis.call('HSET', key, 'state', 'failed')
redis.call('HDEL', key, 'status', 'content_type', 'body')
redis.call('EXPIRE', key, ttl)
return 1
`)

func NewIdempotencyGuard(client *redis.Client) *IdempotencyGuard {
	ttl := time.Duration(getenvInt("IDEMPOTENCY_TTL_HOURS", 24)) * time.Hour
	failedTTL := 5 * time.Minute
	if ttl < failedTTL {
		failedTTL = ttl
	}
	return &IdempotencyGuard{
		client:      client,
		ttl:         ttl,
		failedTTL:   failedTTL,
		maxResponse: getenvInt("IDEMPOTENCY_MAX_RESPONSE_BYTES", 8<<20),
	}
}

func requestDigest(req *ChatCompletionRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func (g *IdempotencyGuard) Begin(
	r *http.Request,
	p *APIKeyPrincipal,
	req *ChatCompletionRequest,
) (*IdempotencyDecision, *APIError) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return &IdempotencyDecision{}, nil
	}
	if len(key) > 128 {
		return nil, newAPIError(
			http.StatusUnprocessableEntity,
			"invalid_request_error",
			"NEXORA_INVALID_IDEMPOTENCY_KEY",
			"Idempotency-Key is too long.",
		)
	}

	digest, err := requestDigest(req)
	if err != nil {
		return nil, newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_INTERNAL_ERROR",
			"Unable to fingerprint request.",
		)
	}

	redisKey := idempotencyKeyPrefix + p.ID + ":" + key
	streamFlag := "0"
	if req.Stream {
		streamFlag = "1"
	}

	result, err := beginIdempotencyScript.Run(
		r.Context(),
		g.client,
		[]string{redisKey},
		digest,
		streamFlag,
		int64(g.ttl/time.Second),
	).Slice()
	if err != nil {
		return nil, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Idempotency protection is unavailable.",
		)
	}
	if len(result) == 0 {
		return nil, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Idempotency protection returned an invalid result.",
		)
	}

	code := redisInt(result[0])
	switch code {
	case 1:
		return &IdempotencyDecision{
			Reservation: &IdempotencyReservation{
				guard:  g,
				key:    redisKey,
				digest: digest,
				stream: req.Stream,
				active: true,
			},
		}, nil
	case 2:
		return nil, newAPIError(
			http.StatusConflict,
			"conflict_error",
			"NEXORA_IDEMPOTENCY_CONFLICT",
			"Idempotency-Key was already used with a different request.",
		)
	case 3:
		return nil, newAPIError(
			http.StatusConflict,
			"conflict_error",
			"NEXORA_DUPLICATE_REQUEST",
			"An identical request with this Idempotency-Key is still processing.",
		)
	case 4:
		return nil, newAPIError(
			http.StatusConflict,
			"conflict_error",
			"NEXORA_DUPLICATE_STREAM",
			"Streaming responses cannot be replayed. Use a new Idempotency-Key.",
		)
	case 5:
		if len(result) < 4 {
			return nil, newAPIError(
				http.StatusServiceUnavailable,
				"service_unavailable",
				"NEXORA_IDEMPOTENCY_UNAVAILABLE",
				"Cached idempotent response is incomplete.",
			)
		}
		statusCode, parseErr := strconv.Atoi(redisString(result[1]))
		if parseErr != nil || statusCode < 100 || statusCode > 599 {
			return nil, newAPIError(
				http.StatusServiceUnavailable,
				"service_unavailable",
				"NEXORA_IDEMPOTENCY_UNAVAILABLE",
				"Cached idempotent response is invalid.",
			)
		}
		return &IdempotencyDecision{
			Replay: &CachedIdempotentResponse{
				Status:      statusCode,
				ContentType: redisString(result[2]),
				Body:        []byte(redisString(result[3])),
			},
		}, nil
	default:
		return nil, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Idempotency protection returned an invalid state.",
		)
	}
}

func (r *IdempotencyReservation) Complete(
	ctx context.Context,
	status int,
	contentType string,
	body []byte,
) *APIError {
	if r == nil || !r.active {
		return nil
	}
	if r.stream {
		return newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_INTERNAL_ERROR",
			"Streaming reservations must use CompleteStream.",
		)
	}
	if len(body) > r.guard.maxResponse {
		return newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_IDEMPOTENCY_RESPONSE_TOO_LARGE",
			"Response is too large for idempotent replay.",
		)
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}

	ok, err := completeIdempotencyScript.Run(
		ctx,
		r.guard.client,
		[]string{r.key},
		r.digest,
		strconv.Itoa(status),
		contentType,
		body,
		"0",
		int64(r.guard.ttl/time.Second),
	).Int64()
	if err != nil || ok != 1 {
		return newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Unable to finalize idempotent response.",
		)
	}
	r.active = false
	return nil
}

func (r *IdempotencyReservation) CompleteStream(ctx context.Context) *APIError {
	if r == nil || !r.active {
		return nil
	}
	ok, err := completeIdempotencyScript.Run(
		ctx,
		r.guard.client,
		[]string{r.key},
		r.digest,
		"200",
		"text/event-stream",
		"",
		"1",
		int64(r.guard.ttl/time.Second),
	).Int64()
	if err != nil || ok != 1 {
		return newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Unable to finalize streaming idempotency state.",
		)
	}
	r.active = false
	return nil
}

func (r *IdempotencyReservation) Fail(ctx context.Context) {
	if r == nil || !r.active {
		return
	}
	_, _ = failIdempotencyScript.Run(
		ctx,
		r.guard.client,
		[]string{r.key},
		r.digest,
		int64(r.guard.failedTTL/time.Second),
	).Result()
	r.active = false
}

func NewBufferedResponseWriter() *BufferedResponseWriter {
	return &BufferedResponseWriter{header: make(http.Header)}
}

func (w *BufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *BufferedResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
}

func (w *BufferedResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *BufferedResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *BufferedResponseWriter) Body() []byte {
	return w.body.Bytes()
}

func (w *BufferedResponseWriter) FlushTo(dst http.ResponseWriter) {
	for key, values := range w.header {
		for _, value := range values {
			dst.Header().Add(key, value)
		}
	}
	dst.WriteHeader(w.Status())
	_, _ = dst.Write(w.body.Bytes())
}

func writeCachedIdempotentResponse(w http.ResponseWriter, cached *CachedIdempotentResponse) {
	if cached == nil {
		return
	}
	if cached.ContentType != "" {
		w.Header().Set("Content-Type", cached.ContentType)
	}
	w.Header().Set("Idempotency-Replayed", "true")
	w.WriteHeader(cached.Status)
	_, _ = w.Write(cached.Body)
}

func redisInt(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case []byte:
		n, _ := strconv.ParseInt(string(v), 10, 64)
		return n
	default:
		return 0
	}
}

func redisString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}
