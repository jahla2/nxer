package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type IdempotencyGuard struct {
	client     *redis.Client
	ttl        time.Duration
	pendingTTL time.Duration
}

type idempotencyState struct {
	Digest      string `json:"digest"`
	State       string `json:"state"`
	Status      int    `json:"status,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

type CachedResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

type IdempotencyHandle struct {
	guard        *IdempotencyGuard
	redisKey     string
	pendingValue string
	stream       bool
}

var idemCompareDeleteScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

var idemCompareSetScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
  return 1
end
return 0
`)

func NewIdempotencyGuard(client *redis.Client) *IdempotencyGuard {
	return &IdempotencyGuard{
		client:     client,
		ttl:        time.Duration(getenvInt("IDEMPOTENCY_TTL_HOURS", 24)) * time.Hour,
		pendingTTL: time.Duration(getenvInt("IDEMPOTENCY_PENDING_TTL_SECONDS", 300)) * time.Second,
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

func idempotencyRedisKey(apiKeyID, clientKey string) string {
	sum := sha256.Sum256([]byte(clientKey))
	return "nxa:idempotency:" + apiKeyID + ":" + hex.EncodeToString(sum[:])
}

func validIdempotencyKey(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

func (g *IdempotencyGuard) Begin(
	ctx context.Context,
	p *APIKeyPrincipal,
	req *ChatCompletionRequest,
	clientKey string,
) (*IdempotencyHandle, *CachedResponse, *APIError) {
	clientKey = strings.TrimSpace(clientKey)
	if clientKey == "" {
		return nil, nil, nil
	}
	if !validIdempotencyKey(clientKey) {
		return nil, nil, newAPIError(
			http.StatusUnprocessableEntity,
			"invalid_request_error",
			"NEXORA_INVALID_IDEMPOTENCY_KEY",
			"Idempotency-Key must be 1-128 visible ASCII characters.",
		)
	}

	digest, err := requestDigest(req)
	if err != nil {
		return nil, nil, newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_INTERNAL_ERROR",
			"Unable to fingerprint request.",
		)
	}

	pending := idempotencyState{Digest: digest, State: "pending"}
	pendingBytes, err := json.Marshal(pending)
	if err != nil {
		return nil, nil, newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_INTERNAL_ERROR",
			"Unable to initialize idempotency state.",
		)
	}
	redisKey := idempotencyRedisKey(p.ID, clientKey)
	created, err := g.client.SetNX(ctx, redisKey, pendingBytes, g.pendingTTL).Result()
	if err != nil {
		return nil, nil, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Idempotency protection is unavailable.",
		)
	}

	handle := &IdempotencyHandle{
		guard:        g,
		redisKey:     redisKey,
		pendingValue: string(pendingBytes),
		stream:       req.Stream,
	}
	if created {
		return handle, nil, nil
	}

	existingBytes, err := g.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		return nil, nil, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Idempotency protection is unavailable.",
		)
	}
	var existing idempotencyState
	if err := json.Unmarshal(existingBytes, &existing); err != nil {
		return nil, nil, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_IDEMPOTENCY_UNAVAILABLE",
			"Idempotency state is invalid.",
		)
	}
	if existing.Digest != digest {
		return nil, nil, newAPIError(
			http.StatusConflict,
			"conflict_error",
			"NEXORA_IDEMPOTENCY_CONFLICT",
			"Idempotency-Key was already used with a different request.",
		)
	}
	if existing.State == "completed" && !req.Stream {
		return nil, &CachedResponse{
			Status:      existing.Status,
			ContentType: existing.ContentType,
			Body:        existing.Body,
		}, nil
	}
	if existing.State == "pending" {
		return nil, nil, newAPIError(
			http.StatusConflict,
			"conflict_error",
			"NEXORA_DUPLICATE_REQUEST",
			"This request is already in progress.",
		)
	}
	return nil, nil, newAPIError(
		http.StatusServiceUnavailable,
		"service_unavailable",
		"NEXORA_IDEMPOTENCY_UNAVAILABLE",
		"Idempotency state is unavailable.",
	)
}

func (h *IdempotencyHandle) Complete(ctx context.Context, response CachedResponse) error {
	if h == nil || h.stream {
		return nil
	}
	state := idempotencyState{
		Digest:      strings.TrimPrefix("", ""),
		State:       "completed",
		Status:      response.Status,
		ContentType: response.ContentType,
		Body:        response.Body,
	}
	var pending idempotencyState
	if err := json.Unmarshal([]byte(h.pendingValue), &pending); err != nil {
		return err
	}
	state.Digest = pending.Digest
	completed, err := json.Marshal(state)
	if err != nil {
		return err
	}
	result, err := idemCompareSetScript.Run(
		ctx,
		h.guard.client,
		[]string{h.redisKey},
		h.pendingValue,
		completed,
		int(h.guard.ttl.Seconds()),
	).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return errors.New("idempotency state changed before completion")
	}
	return nil
}

func (h *IdempotencyHandle) Abort() {
	if h == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = idemCompareDeleteScript.Run(
		ctx,
		h.guard.client,
		[]string{h.redisKey},
		h.pendingValue,
	).Result()
}

func (h *IdempotencyHandle) FinishStream() {
	h.Abort()
}

type bufferedResponseWriter struct {
	parent http.ResponseWriter
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponseWriter(parent http.ResponseWriter) *bufferedResponseWriter {
	return &bufferedResponseWriter{
		parent: parent,
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (w *bufferedResponseWriter) Header() http.Header { return w.header }

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.body.Len() > 0 {
		return
	}
	w.status = status
}

func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	return w.body.Write(p)
}

func (w *bufferedResponseWriter) RequestID() string {
	if writer, ok := w.parent.(requestIDWriter); ok {
		return writer.RequestID()
	}
	return ""
}

func (w *bufferedResponseWriter) SetErrorClass(value string) {
	if writer, ok := w.parent.(errorClassWriter); ok {
		writer.SetErrorClass(value)
	}
}

func (w *bufferedResponseWriter) SetUsage(promptTokens, completionTokens *int) {
	if writer, ok := w.parent.(interface{ SetUsage(*int, *int) }); ok {
		writer.SetUsage(promptTokens, completionTokens)
	}
}

func (w *bufferedResponseWriter) CachedResponse() CachedResponse {
	return CachedResponse{
		Status:      w.status,
		ContentType: w.header.Get("Content-Type"),
		Body:        append([]byte(nil), w.body.Bytes()...),
	}
}

func (w *bufferedResponseWriter) FlushTo(target http.ResponseWriter) {
	for key, values := range w.header {
		for _, value := range values {
			target.Header().Add(key, value)
		}
	}
	target.WriteHeader(w.status)
	_, _ = target.Write(w.body.Bytes())
}

func writeCachedResponse(w http.ResponseWriter, cached *CachedResponse) {
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
