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

CREATE TABLE IF NOT EXISTS usage_aggregation_state (
    name text PRIMARY KEY,
    last_created_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    last_event_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO usage_aggregation_state (name)
VALUES ('usage_daily')
ON CONFLICT (name) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_usage_events_created_at
    ON usage_events(created_at);
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
