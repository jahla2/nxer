package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestIdempotencyLifecycleReplayConflictAndRetry(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if strings.TrimSpace(redisURL) == "" {
		t.Skip("REDIS_URL not configured; skipping Redis idempotency integration test")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	guard := NewIdempotencyGuard(client)
	principal := &APIKeyPrincipal{
		ID:        "idem-key-" + time.Now().UTC().Format("150405.000000000"),
		ProjectID: "idem-project",
	}
	req := &ChatCompletionRequest{
		Model: "auto-free",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpReq.Header.Set("Idempotency-Key", "idem-replay-test")

	cacheKey := idempotencyKeyPrefix + principal.ID + ":idem-replay-test"
	t.Cleanup(func() {
		_ = client.Del(context.Background(), cacheKey).Err()
	})

	first, apiErr := guard.Begin(httpReq, principal, req)
	if apiErr != nil {
		t.Fatalf("begin first request: %#v", apiErr)
	}
	if first.Reservation == nil || first.Replay != nil {
		t.Fatalf("expected active reservation, got %#v", first)
	}

	duplicate, apiErr := guard.Begin(httpReq, principal, req)
	if duplicate != nil {
		t.Fatalf("processing duplicate should not return a decision: %#v", duplicate)
	}
	if apiErr == nil || apiErr.Code != "NEXORA_DUPLICATE_REQUEST" {
		t.Fatalf("expected duplicate processing error, got %#v", apiErr)
	}

	responseBody := []byte(`{"id":"chatcmpl_test","choices":[{"index":0}]}`)
	if finalizeErr := first.Reservation.Complete(
		context.Background(),
		http.StatusOK,
		"application/json",
		responseBody,
	); finalizeErr != nil {
		t.Fatalf("complete response: %#v", finalizeErr)
	}

	replayed, apiErr := guard.Begin(httpReq, principal, req)
	if apiErr != nil {
		t.Fatalf("replay completed request: %#v", apiErr)
	}
	if replayed.Replay == nil {
		t.Fatalf("expected cached replay, got %#v", replayed)
	}
	if replayed.Replay.Status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", replayed.Replay.Status)
	}
	if string(replayed.Replay.Body) != string(responseBody) {
		t.Fatalf("unexpected replay body: %s", replayed.Replay.Body)
	}

	conflicting := &ChatCompletionRequest{
		Model: "auto-free",
		Messages: []ChatMessage{
			{Role: "user", Content: "different"},
		},
	}
	_, apiErr = guard.Begin(httpReq, principal, conflicting)
	if apiErr == nil || apiErr.Code != "NEXORA_IDEMPOTENCY_CONFLICT" {
		t.Fatalf("expected body conflict, got %#v", apiErr)
	}
}

func TestFailedIdempotentRequestCanRetryImmediately(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if strings.TrimSpace(redisURL) == "" {
		t.Skip("REDIS_URL not configured; skipping Redis idempotency integration test")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	guard := NewIdempotencyGuard(client)
	principal := &APIKeyPrincipal{
		ID:        "idem-failed-" + time.Now().UTC().Format("150405.000000000"),
		ProjectID: "idem-project",
	}
	req := &ChatCompletionRequest{
		Model: "auto-free",
		Messages: []ChatMessage{
			{Role: "user", Content: "retry me"},
		},
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpReq.Header.Set("Idempotency-Key", "idem-failed-test")
	cacheKey := idempotencyKeyPrefix + principal.ID + ":idem-failed-test"
	t.Cleanup(func() {
		_ = client.Del(context.Background(), cacheKey).Err()
	})

	first, apiErr := guard.Begin(httpReq, principal, req)
	if apiErr != nil || first.Reservation == nil {
		t.Fatalf("expected first reservation, decision=%#v error=%#v", first, apiErr)
	}
	first.Reservation.Fail(context.Background())

	retry, apiErr := guard.Begin(httpReq, principal, req)
	if apiErr != nil {
		t.Fatalf("failed request should be retryable immediately: %#v", apiErr)
	}
	if retry.Reservation == nil {
		t.Fatalf("expected new processing reservation after failed state, got %#v", retry)
	}
	retry.Reservation.Fail(context.Background())
}

func TestCompletedStreamingRequestCannotReplay(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if strings.TrimSpace(redisURL) == "" {
		t.Skip("REDIS_URL not configured; skipping Redis idempotency integration test")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	guard := NewIdempotencyGuard(client)
	principal := &APIKeyPrincipal{
		ID:        "idem-stream-" + time.Now().UTC().Format("150405.000000000"),
		ProjectID: "idem-project",
	}
	req := &ChatCompletionRequest{
		Model:  "auto-free",
		Stream: true,
		Messages: []ChatMessage{
			{Role: "user", Content: "stream me"},
		},
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpReq.Header.Set("Idempotency-Key", "idem-stream-test")
	cacheKey := idempotencyKeyPrefix + principal.ID + ":idem-stream-test"
	t.Cleanup(func() {
		_ = client.Del(context.Background(), cacheKey).Err()
	})

	first, apiErr := guard.Begin(httpReq, principal, req)
	if apiErr != nil || first.Reservation == nil {
		t.Fatalf("expected stream reservation, decision=%#v error=%#v", first, apiErr)
	}
	if finalizeErr := first.Reservation.CompleteStream(context.Background()); finalizeErr != nil {
		t.Fatalf("complete stream: %#v", finalizeErr)
	}

	_, apiErr = guard.Begin(httpReq, principal, req)
	if apiErr == nil || apiErr.Code != "NEXORA_DUPLICATE_STREAM" {
		t.Fatalf("expected completed stream replay rejection, got %#v", apiErr)
	}
}

func TestBufferedResponseWriterFlushesCapturedResponse(t *testing.T) {
	buffered := NewBufferedResponseWriter()
	buffered.Header().Set("Content-Type", "application/json")
	buffered.WriteHeader(http.StatusCreated)
	_, _ = buffered.Write([]byte(`{"ok":true}`))

	target := httptest.NewRecorder()
	buffered.FlushTo(target)

	if target.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", target.Code)
	}
	if target.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected content type: %q", target.Header().Get("Content-Type"))
	}
	if target.Body.String() != `{"ok":true}` {
		t.Fatalf("unexpected body: %q", target.Body.String())
	}
}
