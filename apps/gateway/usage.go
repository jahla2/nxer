package main

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type UsageEvent struct {
	RequestID        string
	APIKeyID         string
	ModelPublicID    string
	Status           int
	TTFTMS           *int
	LatencyMS        *int
	PromptTokens     *int
	CompletionTokens *int
}

type usageWork struct {
	event      *UsageEvent
	touchKeyID string
}

type UsageRecorder struct {
	db      *sql.DB
	queue   chan usageWork
	wg      sync.WaitGroup
	closeMu sync.Once
}

func NewUsageRecorder(databaseURL string) (*UsageRecorder, error) {
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(getenvInt("USAGE_DB_MAX_OPEN_CONNS", 4))
	db.SetMaxIdleConns(getenvInt("USAGE_DB_MAX_IDLE_CONNS", 2))
	db.SetConnMaxLifetime(30 * time.Minute)

	recorder := &UsageRecorder{
		db:    db,
		queue: make(chan usageWork, getenvInt("USAGE_QUEUE_SIZE", 2048)),
	}
	recorder.wg.Add(1)
	go recorder.run()
	return recorder, nil
}

func (u *UsageRecorder) Close() error {
	if u == nil {
		return nil
	}
	u.closeMu.Do(func() {
		close(u.queue)
		u.wg.Wait()
	})
	return u.db.Close()
}

func (u *UsageRecorder) Ping(ctx context.Context) error {
	if u == nil {
		return errors.New("usage recorder unavailable")
	}
	return u.db.PingContext(ctx)
}

func (u *UsageRecorder) Enqueue(event UsageEvent) bool {
	if u == nil {
		return false
	}
	select {
	case u.queue <- usageWork{event: &event}:
		return true
	default:
		return false
	}
}

func (u *UsageRecorder) TouchLastUsed(apiKeyID string) bool {
	if u == nil || apiKeyID == "" {
		return false
	}
	select {
	case u.queue <- usageWork{touchKeyID: apiKeyID}:
		return true
	default:
		return false
	}
}

func (u *UsageRecorder) run() {
	defer u.wg.Done()
	for work := range u.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if work.event != nil {
			if err := u.recordUsage(ctx, *work.event); err != nil {
				gatewayLogger.Error(
					"usage event persistence failed",
					"event", "usage_persist_failed",
					"request_id", work.event.RequestID,
					"api_key_id", work.event.APIKeyID,
					"error", err.Error(),
				)
			}
		}
		if work.touchKeyID != "" {
			if err := u.touchLastUsed(ctx, work.touchKeyID); err != nil {
				gatewayLogger.Warn(
					"api key last-used persistence failed",
					"event", "last_used_persist_failed",
					"api_key_id", work.touchKeyID,
					"error", err.Error(),
				)
			}
		}
		cancel()
	}
}

func (u *UsageRecorder) recordUsage(ctx context.Context, event UsageEvent) error {
	_, err := u.db.ExecContext(ctx, `
		INSERT INTO usage_events (
			request_id,
			api_key_id,
			model_id,
			status,
			ttft_ms,
			latency_ms,
			prompt_tokens,
			completion_tokens
		)
		VALUES (
			$1,
			$2::uuid,
			(SELECT id FROM models WHERE public_id = NULLIF($3, '') LIMIT 1),
			$4,
			$5,
			$6,
			$7,
			$8
		)
		ON CONFLICT (request_id) DO NOTHING
	`,
		event.RequestID,
		event.APIKeyID,
		event.ModelPublicID,
		event.Status,
		event.TTFTMS,
		event.LatencyMS,
		event.PromptTokens,
		event.CompletionTokens,
	)
	return err
}

func (u *UsageRecorder) touchLastUsed(ctx context.Context, apiKeyID string) error {
	_, err := u.db.ExecContext(ctx, `
		UPDATE api_keys
		SET last_used_at = now()
		WHERE id = $1::uuid
		  AND (
			last_used_at IS NULL
			OR last_used_at < now() - interval '1 minute'
		  )
	`, apiKeyID)
	return err
}
