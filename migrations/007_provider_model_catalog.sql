ALTER TABLE models
    ADD COLUMN IF NOT EXISTS provider_key varchar(32) NOT NULL DEFAULT 'openrouter',
    ADD COLUMN IF NOT EXISTS context_length integer NULL;

CREATE INDEX IF NOT EXISTS idx_models_provider_active_free
    ON models(provider_key, active, is_free);
