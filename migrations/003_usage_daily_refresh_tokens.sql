CREATE TABLE IF NOT EXISTS usage_daily (
    usage_date date NOT NULL,
    api_key_id uuid NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    model_id uuid NULL REFERENCES models(id) ON DELETE SET NULL,
    requests bigint NOT NULL DEFAULT 0,
    prompt_tokens bigint NOT NULL DEFAULT 0,
    completion_tokens bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (usage_date, api_key_id, model_id)
);
CREATE INDEX IF NOT EXISTS idx_usage_daily_date ON usage_daily(usage_date);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_active ON refresh_tokens(user_id, expires_at) WHERE revoked_at IS NULL;
