ALTER TABLE usage_events
    ADD COLUMN IF NOT EXISTS public_model_id text,
    ADD COLUMN IF NOT EXISTS error_class text;

CREATE INDEX IF NOT EXISTS idx_usage_events_public_model_created
    ON usage_events(public_model_id, created_at);
