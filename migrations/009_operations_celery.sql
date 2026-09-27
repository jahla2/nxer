CREATE TABLE IF NOT EXISTS catalog_sync_state (
    provider_key varchar(32) PRIMARY KEY,
    status varchar(20) NOT NULL DEFAULT 'never',
    last_attempt_at timestamptz NULL,
    last_success_at timestamptz NULL,
    model_count integer NOT NULL DEFAULT 0,
    last_error text NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO catalog_sync_state (provider_key, status)
VALUES ('openrouter', 'never')
ON CONFLICT (provider_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS background_job_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name text NOT NULL,
    task_id text NULL,
    status varchar(20) NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    duration_ms integer NULL,
    result jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text NULL,
    worker_name text NULL
);

CREATE INDEX IF NOT EXISTS idx_background_job_runs_task_id
    ON background_job_runs(task_id)
    WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_background_job_runs_name_started
    ON background_job_runs(job_name, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_background_job_runs_status_started
    ON background_job_runs(status, started_at DESC);

ALTER TABLE usage_events
    ADD COLUMN IF NOT EXISTS aggregated_at timestamptz NULL;

CREATE INDEX IF NOT EXISTS idx_usage_events_unaggregated
    ON usage_events(created_at, id)
    WHERE aggregated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_usage_events_created_at
    ON usage_events(created_at);


-- Establish an exact baseline for the new incremental aggregator. Previous
-- alpha builds rebuilt usage_daily from raw usage_events, so recompute the
-- dimensions represented by retained raw events once and mark them processed.
WITH retained_dimensions AS (
    SELECT DISTINCT created_at::date AS usage_date, api_key_id, model_id
    FROM usage_events
)
DELETE FROM usage_daily daily
USING retained_dimensions dimensions
WHERE daily.usage_date=dimensions.usage_date
  AND daily.api_key_id=dimensions.api_key_id
  AND daily.model_id IS NOT DISTINCT FROM dimensions.model_id;

INSERT INTO usage_daily (
    usage_date,
    api_key_id,
    model_id,
    requests,
    prompt_tokens,
    completion_tokens
)
SELECT
    created_at::date,
    api_key_id,
    model_id,
    count(*),
    coalesce(sum(prompt_tokens), 0),
    coalesce(sum(completion_tokens), 0)
FROM usage_events
GROUP BY created_at::date, api_key_id, model_id
ON CONFLICT (usage_date, api_key_id, model_id) DO UPDATE SET
    requests=EXCLUDED.requests,
    prompt_tokens=EXCLUDED.prompt_tokens,
    completion_tokens=EXCLUDED.completion_tokens,
    updated_at=now();

UPDATE usage_events
SET aggregated_at=COALESCE(aggregated_at, now())
WHERE aggregated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at
    ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_expires
    ON password_reset_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_email_verification_tokens_expires
    ON email_verification_tokens(expires_at);

INSERT INTO models (
    public_id,
    upstream_id,
    display_name,
    provider_key,
    context_length,
    active,
    is_free,
    capabilities
)
VALUES (
    'auto-free',
    'openrouter/free',
    'Auto Free',
    'openrouter',
    NULL,
    true,
    true,
    '{"text": true, "streaming": true}'::jsonb
)
ON CONFLICT (public_id) DO UPDATE SET
    upstream_id = EXCLUDED.upstream_id,
    display_name = EXCLUDED.display_name,
    provider_key = EXCLUDED.provider_key,
    context_length = EXCLUDED.context_length,
    active = true,
    is_free = true,
    capabilities = EXCLUDED.capabilities,
    updated_at = now();
