package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryPolicyBackoffIsBounded(t *testing.T) {
	policy := RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    250 * time.Millisecond,
	}

	if got := policy.Backoff(1, ""); got != 100*time.Millisecond {
		t.Fatalf("expected first backoff 100ms, got %s", got)
	}
	if got := policy.Backoff(2, ""); got != 200*time.Millisecond {
		t.Fatalf("expected second backoff 200ms, got %s", got)
	}
	if got := policy.Backoff(3, ""); got != 250*time.Millisecond {
		t.Fatalf("expected capped backoff 250ms, got %s", got)
	}
	if got := policy.Backoff(1, "10"); got != 250*time.Millisecond {
		t.Fatalf("expected Retry-After to respect max delay, got %s", got)
	}
}

func TestSleepWithContextCancelsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepWithContext(ctx, time.Second)
	if err == nil {
		t.Fatal("expected canceled sleep")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("canceled backoff did not return promptly")
	}
}

func TestCircuitBreakerOpensHalfOpensAndCloses(t *testing.T) {
	breaker := NewCircuitBreaker(2, 30*time.Second)
	now := time.Unix(1_700_000_000, 0)
	breaker.now = func() time.Time { return now }

	first, ok := breaker.Acquire()
	if !ok {
		t.Fatal("first request should be admitted")
	}
	first.Failure()
	if breaker.State() != CircuitClosed {
		t.Fatalf("expected closed after one failure, got %s", breaker.State())
	}

	second, ok := breaker.Acquire()
	if !ok {
		t.Fatal("second request should be admitted")
	}
	second.Failure()
	if breaker.State() != CircuitOpen {
		t.Fatalf("expected open after threshold, got %s", breaker.State())
	}

	if _, ok := breaker.Acquire(); ok {
		t.Fatal("open circuit should reject requests")
	}

	now = now.Add(31 * time.Second)
	if breaker.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after cooldown, got %s", breaker.State())
	}

	probe, ok := breaker.Acquire()
	if !ok {
		t.Fatal("half-open probe should be admitted")
	}
	if _, ok := breaker.Acquire(); ok {
		t.Fatal("only one half-open probe should run")
	}

	probe.Success()
	if breaker.State() != CircuitClosed {
		t.Fatalf("successful probe should close circuit, got %s", breaker.State())
	}
}

func TestCircuitBreakerFailedProbeReopens(t *testing.T) {
	breaker := NewCircuitBreaker(1, time.Second)
	now := time.Unix(1_700_000_000, 0)
	breaker.now = func() time.Time { return now }

	permit, ok := breaker.Acquire()
	if !ok {
		t.Fatal("initial request should be admitted")
	}
	permit.Failure()

	now = now.Add(2 * time.Second)
	probe, ok := breaker.Acquire()
	if !ok {
		t.Fatal("probe should be admitted")
	}
	probe.Failure()

	if breaker.State() != CircuitOpen {
		t.Fatalf("failed probe should reopen circuit, got %s", breaker.State())
	}
}

func TestRetryableProviderStatusClassification(t *testing.T) {
	for _, status := range []int{
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if !isRetryableProviderStatus(status) {
			t.Fatalf("expected status %d to be retryable", status)
		}
	}

	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	} {
		if isRetryableProviderStatus(status) {
			t.Fatalf("expected status %d to be non-retryable", status)
		}
	}
}
