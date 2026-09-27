package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type UsageEvent struct {
	RequestID        string
	APIKeyID         string
	PublicModelID    string
	Status           int
	TTFTMS           *int
	LatencyMS        int
	PromptTokens     *int
	CompletionTokens *int
	ErrorClass       string
}

type UsageRecorder struct {
	db     *sql.DB
	queue  chan UsageEvent
	wg     sync.WaitGroup
	closed sync.Once
}

func NewUsageRecorder(databaseURL string, queueSize int) (*UsageRecorder, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if queueSize < 1 {
		queueSize = 256
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	recorder := &UsageRecorder{db: db, queue: make(chan UsageEvent, queueSize)}
	recorder.wg.Add(1)
	go recorder.loop()
	return recorder, nil
}

func (r *UsageRecorder) Record(event UsageEvent) {
	if r == nil {
		return
	}
	select {
	case r.queue <- event:
	default:
		log.Printf("{\"event\":\"usage_event_dropped\",\"request_id\":%q}", event.RequestID)
	}
}

func (r *UsageRecorder) loop() {
	defer r.wg.Done()
	for event := range r.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := r.db.ExecContext(
			ctx,
			`
			WITH inserted AS (
				INSERT INTO usage_events (
					request_id, api_key_id, public_model_id, status, ttft_ms,
					latency_ms, prompt_tokens, completion_tokens, error_class
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''))
				ON CONFLICT (request_id) DO NOTHING
				RETURNING api_key_id
			)
			UPDATE api_keys
			SET last_used_at=now(), updated_at=now()
			WHERE id IN (SELECT api_key_id FROM inserted)
			`,
			event.RequestID,
			event.APIKeyID,
			event.PublicModelID,
			event.Status,
			event.TTFTMS,
			event.LatencyMS,
			event.PromptTokens,
			event.CompletionTokens,
			event.ErrorClass,
		)
		cancel()
		if err != nil {
			log.Printf("{\"event\":\"usage_event_persist_failed\",\"request_id\":%q}", event.RequestID)
		}
	}
}

func (r *UsageRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.closed.Do(func() {
		close(r.queue)
		r.wg.Wait()
	})
	return r.db.Close()
}
