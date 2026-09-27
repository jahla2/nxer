package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestListFreeModelsFiltersPaidAndNonTextModels(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/free-text","name":"Free Text","context_length":8192,"pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["text"]}},
			{"id":"vendor/paid-text","name":"Paid","pricing":{"prompt":"0.1","completion":"0"},"architecture":{"output_modalities":["text"]}},
			{"id":"vendor/free-image","name":"Image","pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["image"]}}
		]}`))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
	}
	models, err := provider.ListFreeModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected one eligible model, got %#v", models)
	}
	if models[0].UpstreamID != "vendor/free-text" {
		t.Fatalf("unexpected upstream model: %#v", models[0])
	}
	if !models[0].Capabilities["text"] || !models[0].Capabilities["streaming"] {
		t.Fatalf("expected public capabilities, got %#v", models[0].Capabilities)
	}
}

func TestChatRoutesAliasAndSanitizesJSONResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing provider authorization")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"model":"vendor/free-text"`) {
			t.Fatalf("expected internal route in upstream body, got %s", body)
		}
		if strings.Contains(string(body), "nexora/free-text-public") {
			t.Fatalf("public alias must not be sent upstream: %s", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"upstream-secret-id",
			"model":"vendor/free-text",
			"provider":"OpenRouter",
			"object":"chat.completion",
			"choices":[],
			"usage":{
				"prompt_tokens":7,
				"completion_tokens":3,
				"total_tokens":10,
				"cost":0,
				"is_byok":false
			}
		}`))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
	}
	route := Model{
		ID:          "nexora/free-text-public",
		UpstreamID:  "vendor/free-text",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	rec := httptest.NewRecorder()

	metrics, apiErr := provider.Chat(context.Background(), req, route, rec)
	if apiErr != nil {
		t.Fatalf("unexpected error %#v", apiErr)
	}
	if metrics == nil || !metrics.Completed {
		t.Fatalf("expected completed metrics, got %#v", metrics)
	}
	if metrics.PromptTokens == nil || *metrics.PromptTokens != 7 {
		t.Fatalf("unexpected prompt tokens %#v", metrics.PromptTokens)
	}
	if metrics.CompletionTokens == nil || *metrics.CompletionTokens != 3 {
		t.Fatalf("unexpected completion tokens %#v", metrics.CompletionTokens)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["model"] != route.ID {
		t.Fatalf("expected public model alias, got %#v", payload["model"])
	}
	id, _ := payload["id"].(string)
	if !strings.HasPrefix(id, "chatcmpl_nxa_") {
		t.Fatalf("expected Nexora completion id, got %q", id)
	}
	if _, exists := payload["provider"]; exists {
		t.Fatal("provider metadata leaked")
	}
	if strings.Contains(rec.Body.String(), "upstream-secret-id") ||
		strings.Contains(rec.Body.String(), "vendor/free-text") ||
		strings.Contains(strings.ToLower(rec.Body.String()), "openrouter") {
		t.Fatalf("upstream identity leaked: %s", rec.Body.String())
	}
	usage, _ := payload["usage"].(map[string]any)
	if _, exists := usage["cost"]; exists {
		t.Fatal("provider-specific cost metadata leaked")
	}
	if _, exists := usage["is_byok"]; exists {
		t.Fatal("provider-specific BYOK metadata leaked")
	}
}

func TestChatMapsUpstreamRateLimitWithoutLeakingBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("provider-secret-error-detail"))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
	}
	route := Model{
		ID:          "nexora/free-text-public",
		UpstreamID:  "vendor/free-text",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	rec := httptest.NewRecorder()

	metrics, apiErr := provider.Chat(context.Background(), req, route, rec)
	if metrics == nil || metrics.Status != http.StatusServiceUnavailable {
		t.Fatalf("unexpected metrics %#v", metrics)
	}
	if apiErr == nil ||
		apiErr.Status != http.StatusServiceUnavailable ||
		apiErr.Code != "NEXORA_UPSTREAM_CAPACITY" {
		t.Fatalf("unexpected error %#v", apiErr)
	}
	if strings.Contains(apiErr.Message, "provider-secret") {
		t.Fatal("provider detail leaked")
	}
}

func TestChatStreamingSanitizesProviderIdentityAndCapturesUsage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}

		_, _ = w.Write([]byte(": OPENROUTER PROCESSING\n"))
		_, _ = w.Write([]byte(`data: {"id":"upstream-stream-id","model":"vendor/free-text","provider":"OpenRouter","choices":[{"delta":{"content":"hello"}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"id":"upstream-stream-id","model":"vendor/free-text","provider_name":"OpenRouter","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":4,"cost":0}}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
	}
	route := Model{
		ID:          "nexora/free-text-public",
		UpstreamID:  "vendor/free-text",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Stream:   true,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	rec := httptest.NewRecorder()

	metrics, apiErr := provider.Chat(context.Background(), req, route, rec)
	if apiErr != nil {
		t.Fatalf("unexpected error %#v", apiErr)
	}
	if metrics == nil || !metrics.Completed || metrics.Status != http.StatusOK {
		t.Fatalf("unexpected metrics %#v", metrics)
	}
	if metrics.TTFTMS == nil || metrics.LatencyMS == nil {
		t.Fatalf("missing stream timings %#v", metrics)
	}
	if metrics.PromptTokens == nil || *metrics.PromptTokens != 11 {
		t.Fatalf("unexpected prompt tokens %#v", metrics.PromptTokens)
	}
	if metrics.CompletionTokens == nil || *metrics.CompletionTokens != 4 {
		t.Fatalf("unexpected completion tokens %#v", metrics.CompletionTokens)
	}

	body := rec.Body.String()
	if !strings.Contains(body, route.ID) {
		t.Fatalf("public model alias missing from stream: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream body missing done marker: %s", body)
	}
	for _, leaked := range []string{
		"OPENROUTER",
		"OpenRouter",
		"vendor/free-text",
		"upstream-stream-id",
		"\"cost\"",
		"\"provider\"",
		"\"provider_name\"",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("stream leaked %q: %s", leaked, body)
		}
	}
	if strings.Count(body, "chatcmpl_nxa_") < 2 {
		t.Fatalf("expected stable Nexora completion id in stream chunks: %s", body)
	}
}

func TestChatRejectsUnsupportedInternalProviderRoute(t *testing.T) {
	provider := &OpenRouterProvider{}
	req := &ChatCompletionRequest{
		Model:    "nexora/test",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	route := Model{
		ID:          "nexora/test",
		UpstreamID:  "internal/test",
		ProviderKey: "other-provider",
	}

	metrics, apiErr := provider.Chat(context.Background(), req, route, httptest.NewRecorder())
	if metrics == nil || metrics.Status != http.StatusServiceUnavailable {
		t.Fatalf("unexpected metrics %#v", metrics)
	}
	if apiErr == nil || apiErr.Code != "NEXORA_ROUTE_UNAVAILABLE" {
		t.Fatalf("unexpected error %#v", apiErr)
	}
}


func TestChatStreamingRequiresDoneMarker(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"upstream-stream-id","model":"vendor/free-text","choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
	}
	route := Model{
		ID:          "nexora/free-text-public",
		UpstreamID:  "vendor/free-text",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Stream:   true,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}

	metrics, apiErr := provider.Chat(context.Background(), req, route, httptest.NewRecorder())
	if apiErr != nil {
		t.Fatalf("unexpected API error after stream headers: %#v", apiErr)
	}
	if metrics == nil || metrics.Completed {
		t.Fatalf("truncated stream must not be completed: %#v", metrics)
	}
	if metrics.Status != http.StatusBadGateway {
		t.Fatalf("expected bad gateway telemetry for truncated stream, got %#v", metrics)
	}
}


func TestChatRetriesTransientStatusBeforeSuccess(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"upstream-id",
			"model":"vendor/free-text",
			"choices":[],
			"usage":{"prompt_tokens":2,"completion_tokens":1}
		}`))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
		retryPolicy: RetryPolicy{
			MaxAttempts: 2,
			BaseDelay:   0,
			MaxDelay:    0,
		},
		breaker:        NewCircuitBreaker(3, time.Second),
		requestTimeout: 2 * time.Second,
	}
	route := Model{
		ID:          "nexora/free-text-test",
		UpstreamID:  "vendor/free-text",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	rec := httptest.NewRecorder()

	metrics, apiErr := provider.Chat(context.Background(), req, route, rec)
	if apiErr != nil {
		t.Fatalf("unexpected error after retry: %#v", apiErr)
	}
	if metrics == nil || !metrics.Completed {
		t.Fatalf("expected completed response, got %#v", metrics)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected two attempts, got %d", attempts.Load())
	}
	if provider.breaker.State() != CircuitClosed {
		t.Fatalf("successful retry should keep circuit closed, got %s", provider.breaker.State())
	}
}

func TestChatCircuitOpensAfterConsecutiveProviderFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
		retryPolicy: RetryPolicy{
			MaxAttempts: 1,
		},
		breaker:        NewCircuitBreaker(2, time.Minute),
		requestTimeout: 2 * time.Second,
	}
	route := Model{
		ID:          "nexora/circuit-test",
		UpstreamID:  "vendor/circuit-test",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}

	for i := 0; i < 2; i++ {
		_, apiErr := provider.Chat(context.Background(), req, route, httptest.NewRecorder())
		if apiErr == nil {
			t.Fatal("expected transient provider error")
		}
	}
	if provider.breaker.State() != CircuitOpen {
		t.Fatalf("expected open circuit, got %s", provider.breaker.State())
	}

	_, apiErr := provider.Chat(context.Background(), req, route, httptest.NewRecorder())
	if apiErr == nil || apiErr.Code != "NEXORA_PROVIDER_CIRCUIT_OPEN" {
		t.Fatalf("expected circuit-open error, got %#v", apiErr)
	}
	if attempts.Load() != 2 {
		t.Fatalf("open circuit should block network call; attempts=%d", attempts.Load())
	}
}

func TestChatProviderDeadlineMapsToGatewayTimeout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"late","model":"vendor/slow","choices":[]}`))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
		retryPolicy: RetryPolicy{
			MaxAttempts: 1,
		},
		breaker:        NewCircuitBreaker(5, time.Second),
		requestTimeout: 40 * time.Millisecond,
	}
	route := Model{
		ID:          "nexora/slow-test",
		UpstreamID:  "vendor/slow",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}

	metrics, apiErr := provider.Chat(context.Background(), req, route, httptest.NewRecorder())
	if apiErr == nil || apiErr.Code != "NEXORA_UPSTREAM_TIMEOUT" || apiErr.Status != http.StatusGatewayTimeout {
		t.Fatalf("expected gateway timeout, got metrics=%#v error=%#v", metrics, apiErr)
	}
	if metrics == nil || metrics.Status != http.StatusGatewayTimeout {
		t.Fatalf("unexpected timeout metrics %#v", metrics)
	}
}

type failingStreamWriter struct {
	header http.Header
	writes int
}

func (w *failingStreamWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingStreamWriter) WriteHeader(_ int) {}

func (w *failingStreamWriter) Write(body []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, fmt.Errorf("client disconnected")
	}
	return len(body), nil
}

func (w *failingStreamWriter) Flush() {}

func TestChatStreamingClientDisconnectIsIncompleteAndDoesNotTripCircuit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"id":"upstream-1","model":"vendor/stream","choices":[{"delta":{"content":"one"}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"id":"upstream-1","model":"vendor/stream","choices":[{"delta":{"content":"two"}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
		retryPolicy: RetryPolicy{
			MaxAttempts: 1,
		},
		breaker:       NewCircuitBreaker(1, time.Second),
		streamTimeout: 2 * time.Second,
	}
	route := Model{
		ID:          "nexora/stream-disconnect-test",
		UpstreamID:  "vendor/stream",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Stream:   true,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	writer := &failingStreamWriter{}

	metrics, apiErr := provider.Chat(context.Background(), req, route, writer)
	if apiErr != nil {
		t.Fatalf("disconnect after stream start should not write a second API error: %#v", apiErr)
	}
	if metrics == nil || metrics.Completed || metrics.Status != 499 {
		t.Fatalf("disconnect should be incomplete 499 telemetry, got %#v", metrics)
	}
	if provider.breaker.State() != CircuitClosed {
		t.Fatalf("client disconnect must not trip provider circuit, got %s", provider.breaker.State())
	}
}

func TestChatHandlesConcurrentStreamingLoad(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"id":"upstream-load","model":"vendor/load","choices":[{"delta":{"content":"ok"}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
		retryPolicy: RetryPolicy{
			MaxAttempts: 1,
		},
		breaker:       NewCircuitBreaker(20, time.Second),
		streamTimeout: 3 * time.Second,
	}
	route := Model{
		ID:          "nexora/load-test",
		UpstreamID:  "vendor/load",
		ProviderKey: openRouterProviderKey,
	}
	req := &ChatCompletionRequest{
		Model:    route.ID,
		Stream:   true,
		Messages: []ChatMessage{{Role: "user", Content: "load"}},
	}

	const clients = 64
	var wg sync.WaitGroup
	errs := make(chan error, clients)

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			metrics, apiErr := provider.Chat(context.Background(), req, route, rec)
			if apiErr != nil {
				errs <- fmt.Errorf("api error: %s", apiErr.Code)
				return
			}
			if metrics == nil || !metrics.Completed || metrics.Status != http.StatusOK {
				errs <- fmt.Errorf("unexpected metrics: %#v", metrics)
				return
			}
			if !strings.Contains(rec.Body.String(), "data: [DONE]") {
				errs <- fmt.Errorf("missing DONE marker")
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if provider.breaker.State() != CircuitClosed {
		t.Fatalf("healthy concurrent load should leave circuit closed, got %s", provider.breaker.State())
	}
}


func TestListFreeModelsRetriesTransientCatalogFailure(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/free-text","name":"Free Text","context_length":8192,"pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["text"]}}
		]}`))
	}))
	defer server.Close()

	provider := &OpenRouterProvider{
		baseURL: server.URL,
		apiKey:  "secret",
		client:  server.Client(),
		retryPolicy: RetryPolicy{
			MaxAttempts: 2,
			BaseDelay:   0,
			MaxDelay:    0,
		},
		catalogTimeout: 2 * time.Second,
	}

	models, err := provider.ListFreeModels(context.Background())
	if err != nil {
		t.Fatalf("catalog retry failed: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected two catalog attempts, got %d", attempts.Load())
	}
	if len(models) != 1 || models[0].UpstreamID != "vendor/free-text" {
		t.Fatalf("unexpected catalog %#v", models)
	}
}
