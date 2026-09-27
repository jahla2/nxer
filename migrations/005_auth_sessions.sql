ALTER TABLE users
    ADD COLUMN IF NOT EXISTS display_name varchar(120),
    ADD COLUMN IF NOT EXISTS role varchar(20) NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS email_verified boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

UPDATE users
SET display_name = COALESCE(NULLIF(display_name, ''), split_part(email, '@', 1))
WHERE display_name IS NULL OR display_name = '';

ALTER TABLE users
    ALTER COLUMN display_name SET NOT NULL;

DO $$
BEGIN
    IF to_regclass('public.refresh_tokens') IS NOT NULL
       AND to_regclass('public.user_sessions') IS NULL THEN
        ALTER TABLE refresh_tokens RENAME TO user_sessions;
    END IF;
END
$$;

ALTER TABLE user_sessions
    ADD COLUMN IF NOT EXISTS access_token_hash bytea,
    ADD COLUMN IF NOT EXISTS access_expires_at timestamptz,
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS last_seen_at timestamptz,
    ADD COLUMN IF NOT EXISTS user_agent varchar(512);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'user_sessions'
          AND column_name = 'token_hash'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'user_sessions'
          AND column_name = 'refresh_token_hash'
    ) THEN
        ALTER TABLE user_sessions RENAME COLUMN token_hash TO refresh_token_hash;
    END IF;
END
$$;

DELETE FROM user_sessions
WHERE access_token_hash IS NULL
   OR access_expires_at IS NULL;

ALTER TABLE user_sessions
    ALTER COLUMN access_token_hash SET NOT NULL,
    ALTER COLUMN access_expires_at SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_user_sessions_access_hash
    ON user_sessions(access_token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS ux_user_sessions_refresh_hash
    ON user_sessions(refresh_token_hash);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user_active
    ON user_sessions(user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user_active
    ON password_reset_tokens(user_id, expires_at)
    WHERE used_at IS NULL;
