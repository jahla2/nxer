CREATE TABLE IF NOT EXISTS playground_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title varchar(160) NOT NULL DEFAULT 'New chat',
    selected_model_id uuid NULL REFERENCES models(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz NULL
);

CREATE INDEX IF NOT EXISTS idx_playground_sessions_user_project_updated
    ON playground_sessions(user_id, project_id, updated_at DESC)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS playground_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES playground_sessions(id) ON DELETE CASCADE,
    role varchar(20) NOT NULL CHECK (role IN ('user', 'assistant')),
    content text NOT NULL,
    model_id uuid NULL REFERENCES models(id) ON DELETE SET NULL,
    request_id text NULL,
    status integer NULL,
    ttft_ms integer NULL,
    latency_ms integer NULL,
    prompt_tokens integer NULL,
    completion_tokens integer NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_playground_messages_session_created
    ON playground_messages(session_id, created_at, id);

CREATE UNIQUE INDEX IF NOT EXISTS ux_playground_messages_request_id
    ON playground_messages(request_id)
    WHERE request_id IS NOT NULL;
