package main

import (
	"sync"
	"time"
)

type CircuitBreaker struct {
	mu             sync.Mutex
	failures       int
	threshold      int
	openDuration   time.Duration
	openedUntil    time.Time
	halfOpenActive bool
}

func NewCircuitBreaker(threshold int, openDuration time.Duration) *CircuitBreaker {
	if threshold < 1 {
		threshold = 5
	}
	if openDuration <= 0 {
		openDuration = 30 * time.Second
	}
	return &CircuitBreaker{threshold: threshold, openDuration: openDuration}
}

func (b *CircuitBreaker) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.openedUntil.IsZero() {
		return true
	}
	if now.Before(b.openedUntil) {
		return false
	}
	if b.halfOpenActive {
		return false
	}
	b.halfOpenActive = true
	return true
}

func (b *CircuitBreaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedUntil = time.Time{}
	b.halfOpenActive = false
}

func (b *CircuitBreaker) Failure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.halfOpenActive {
		b.openedUntil = now.Add(b.openDuration)
		b.halfOpenActive = false
		b.failures = b.threshold
		return
	}

	b.failures++
	if b.failures >= b.threshold {
		b.openedUntil = now.Add(b.openDuration)
	}
}
