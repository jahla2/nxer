package main

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func NewRetryPolicyFromEnv() RetryPolicy {
	maxAttempts := getenvInt("PROVIDER_MAX_ATTEMPTS", 2)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if maxAttempts > 4 {
		maxAttempts = 4
	}

	baseDelay := time.Duration(getenvInt("PROVIDER_RETRY_BASE_MS", 200)) * time.Millisecond
	if baseDelay < 0 {
		baseDelay = 0
	}

	maxDelay := time.Duration(getenvInt("PROVIDER_RETRY_MAX_MS", 1500)) * time.Millisecond
	if maxDelay < baseDelay {
		maxDelay = baseDelay
	}

	return RetryPolicy{
		MaxAttempts: maxAttempts,
		BaseDelay:   baseDelay,
		MaxDelay:    maxDelay,
	}
}

func (p RetryPolicy) Backoff(attempt int, retryAfter string) time.Duration {
	if delay, ok := parseRetryAfter(retryAfter); ok {
		if delay > p.MaxDelay {
			return p.MaxDelay
		}
		if delay < 0 {
			return 0
		}
		return delay
	}

	if attempt < 1 || p.BaseDelay <= 0 {
		return 0
	}

	exponent := float64(attempt - 1)
	delay := time.Duration(float64(p.BaseDelay) * math.Pow(2, exponent))
	if delay > p.MaxDelay {
		return p.MaxDelay
	}
	return delay
}

func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			seconds = 0
		}
		return time.Duration(seconds) * time.Second, true
	}

	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}

	return 0, false
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type CircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	consecutiveFails int
	failureThreshold int
	openDuration     time.Duration
	openedAt         time.Time
	halfOpenInFlight bool
	now              func() time.Time
}

type CircuitPermit struct {
	breaker  *CircuitBreaker
	halfOpen bool
	once     sync.Once
}

func NewCircuitBreaker(failureThreshold int, openDuration time.Duration) *CircuitBreaker {
	if failureThreshold < 1 {
		failureThreshold = 1
	}
	if openDuration <= 0 {
		openDuration = 30 * time.Second
	}
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		openDuration:     openDuration,
		now:              time.Now,
	}
}

func NewCircuitBreakerFromEnv() *CircuitBreaker {
	return NewCircuitBreaker(
		getenvInt("PROVIDER_CIRCUIT_FAILURE_THRESHOLD", 5),
		time.Duration(getenvInt("PROVIDER_CIRCUIT_OPEN_SECONDS", 30))*time.Second,
	)
}

func (b *CircuitBreaker) Acquire() (*CircuitPermit, bool) {
	if b == nil {
		return &CircuitPermit{}, true
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	switch b.state {
	case CircuitOpen:
		if now.Sub(b.openedAt) < b.openDuration {
			return nil, false
		}
		b.state = CircuitHalfOpen
		b.halfOpenInFlight = false
		fallthrough

	case CircuitHalfOpen:
		if b.halfOpenInFlight {
			return nil, false
		}
		b.halfOpenInFlight = true
		return &CircuitPermit{breaker: b, halfOpen: true}, true

	default:
		return &CircuitPermit{breaker: b}, true
	}
}

func (p *CircuitPermit) Success() {
	if p == nil || p.breaker == nil {
		return
	}
	p.once.Do(func() {
		b := p.breaker
		b.mu.Lock()
		defer b.mu.Unlock()

		b.state = CircuitClosed
		b.consecutiveFails = 0
		b.halfOpenInFlight = false
	})
}

func (p *CircuitPermit) Failure() {
	if p == nil || p.breaker == nil {
		return
	}
	p.once.Do(func() {
		b := p.breaker
		b.mu.Lock()
		defer b.mu.Unlock()

		if p.halfOpen {
			b.state = CircuitOpen
			b.openedAt = b.now()
			b.consecutiveFails = b.failureThreshold
			b.halfOpenInFlight = false
			return
		}

		b.consecutiveFails++
		if b.consecutiveFails >= b.failureThreshold {
			b.state = CircuitOpen
			b.openedAt = b.now()
		}
	})
}

func (p *CircuitPermit) Neutral() {
	if p == nil || p.breaker == nil {
		return
	}
	p.once.Do(func() {
		b := p.breaker
		b.mu.Lock()
		defer b.mu.Unlock()

		if p.halfOpen {
			b.state = CircuitOpen
			b.openedAt = b.now()
		}
		b.halfOpenInFlight = false
	})
}

func (b *CircuitBreaker) State() CircuitState {
	if b == nil {
		return CircuitClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == CircuitOpen && b.now().Sub(b.openedAt) >= b.openDuration {
		return CircuitHalfOpen
	}
	return b.state
}

func isRetryableProviderStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
