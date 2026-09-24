-- Agent sessions (Google ADK-style): session metadata, events, app/user state.

CREATE TABLE IF NOT EXISTS hai_agent_session (
    tenant_id  TEXT NOT NULL REFERENCES hai_tenant (id),
    app_name   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    id         TEXT NOT NULL,
    subject    TEXT,
    state_json JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, app_name, user_id, id)
);

CREATE INDEX IF NOT EXISTS idx_agent_session_updated
    ON hai_agent_session (tenant_id, app_name, user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS hai_agent_session_event (
    tenant_id   TEXT NOT NULL,
    app_name    TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    event_id    TEXT NOT NULL,
    timestamp   TIMESTAMPTZ NOT NULL,
    event_json  JSONB NOT NULL,
    PRIMARY KEY (tenant_id, app_name, user_id, session_id, event_id),
    FOREIGN KEY (tenant_id, app_name, user_id, session_id)
        REFERENCES hai_agent_session (tenant_id, app_name, user_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agent_session_event_time
    ON hai_agent_session_event (tenant_id, app_name, user_id, session_id, timestamp);

CREATE TABLE IF NOT EXISTS hai_agent_app_state (
    tenant_id  TEXT NOT NULL REFERENCES hai_tenant (id),
    app_name   TEXT NOT NULL,
    state_json JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, app_name)
);

CREATE TABLE IF NOT EXISTS hai_agent_user_state (
    tenant_id  TEXT NOT NULL REFERENCES hai_tenant (id),
    app_name   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    state_json JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, app_name, user_id)
);
