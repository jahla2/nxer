CREATE TABLE system_settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    version integer NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO system_settings (key, value) VALUES
    ('catalog.default_model_alias', '"auto-free"'::jsonb),
    ('catalog.provider_free_router', '"openrouter/free"'::jsonb)
ON CONFLICT (key) DO NOTHING;