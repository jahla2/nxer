package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRequestDigestStable(t *testing.T) {
	req := &ChatCompletionRequest{Model: "auto-free", Messages: []ChatMessage{{Role: "user", Content: "hello"}}}
	a, err := requestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := requestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || a != b {
		t.Fatalf("digest not stable: %q %q", a, b)
	}
}

func TestRequestDigestChangesWithBody(t *testing.T) {
	a, _ := requestDigest(&ChatCompletionRequest{Model: "auto-free", Messages: []ChatMessage{{Role: "user", Content: "one"}}})
	b, _ := requestDigest(&ChatCompletionRequest{Model: "auto-free", Messages: []ChatMessage{{Role: "user", Content: "two"}}})
	if a == b {
		t.Fatal("different request bodies must not share digest")
	}
}

func TestIdempotencyReplayConflictAbortAndHashedRedisKey(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not configured")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}

	guard := NewIdempotencyGuard(client)
	principal := &APIKeyPrincipal{ID: "key-idem"}
	req := &ChatCompletionRequest{
		Model:    "auto-free",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	clientKey := "customer-visible-idempotency-key"

	handle, cached, apiErr := guard.Begin(context.Background(), principal, req, clientKey)
	if apiErr != nil || handle == nil || cached != nil {
		t.Fatalf("unexpected begin result: handle=%v cached=%v err=%#v", handle, cached, apiErr)
	}

	_, _, apiErr = guard.Begin(context.Background(), principal, req, clientKey)
	if apiErr == nil || apiErr.Code != "NEXORA_DUPLICATE_REQUEST" {
		t.Fatalf("expected in-progress duplicate rejection, got %#v", apiErr)
	}

	response := CachedResponse{
		Status:      200,
		ContentType: "application/json",
		Body:        []byte(`{"id":"cached","model":"auto-free"}`),
	}
	if err := handle.Complete(context.Background(), response); err != nil {
		t.Fatal(err)
	}

	_, replay, apiErr := guard.Begin(context.Background(), principal, req, clientKey)
	if apiErr != nil || replay == nil {
		t.Fatalf("expected cached replay, got cached=%v err=%#v", replay, apiErr)
	}
	if replay.Status != 200 || string(replay.Body) != string(response.Body) {
		t.Fatalf("unexpected replay %#v", replay)
	}

	changed := &ChatCompletionRequest{
		Model:    "auto-free",
		Messages: []ChatMessage{{Role: "user", Content: "different"}},
	}
	_, _, apiErr = guard.Begin(context.Background(), principal, changed, clientKey)
	if apiErr == nil || apiErr.Code != "NEXORA_IDEMPOTENCY_CONFLICT" {
		t.Fatalf("expected body conflict, got %#v", apiErr)
	}

	keys, err := client.Keys(context.Background(), "nxa:idempotency:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if strings.Contains(key, clientKey) {
			t.Fatalf("raw idempotency key leaked into Redis key name: %s", key)
		}
	}

	abortHandle, _, apiErr := guard.Begin(context.Background(), principal, req, "abortable")
	if apiErr != nil || abortHandle == nil {
		t.Fatalf("unexpected abortable begin: %#v", apiErr)
	}
	abortHandle.Abort()
	retryHandle, _, apiErr := guard.Begin(context.Background(), principal, req, "abortable")
	if apiErr != nil || retryHandle == nil {
		t.Fatalf("expected retry after abort, got %#v", apiErr)
	}
	retryHandle.Abort()
}

func TestStreamingIdempotencyOnlyPreventsConcurrentDuplicate(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not configured")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}

	guard := NewIdempotencyGuard(client)
	principal := &APIKeyPrincipal{ID: "key-stream"}
	req := &ChatCompletionRequest{
		Model:    "auto-free",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
		Stream:   true,
	}

	handle, _, apiErr := guard.Begin(context.Background(), principal, req, "stream-key")
	if apiErr != nil || handle == nil {
		t.Fatalf("unexpected stream begin: %#v", apiErr)
	}
	_, _, apiErr = guard.Begin(context.Background(), principal, req, "stream-key")
	if apiErr == nil || apiErr.Code != "NEXORA_DUPLICATE_REQUEST" {
		t.Fatalf("expected concurrent stream duplicate rejection, got %#v", apiErr)
	}
	handle.FinishStream()

	next, _, apiErr := guard.Begin(context.Background(), principal, req, "stream-key")
	if apiErr != nil || next == nil {
		t.Fatalf("expected same idempotency key after stream completion to be reusable: %#v", apiErr)
	}
	next.FinishStream()
}

func TestInvalidIdempotencyKeyRejected(t *testing.T) {
	if validIdempotencyKey("contains space") {
		t.Fatal("spaces must not be accepted")
	}
	if validIdempotencyKey(strings.Repeat("a", 129)) {
		t.Fatal("keys over 128 chars must not be accepted")
	}
}
