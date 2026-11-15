-- Attest schema: durable AI tool-call runs with human approval gates.
-- Postgres is the source of truth for runs, attempts, approvals, and the job queue.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspace_members (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'approver', 'viewer')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

-- API keys authenticate agent callers. Prefix is shown in the UI; only the hash is stored.
CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);

CREATE TABLE tools (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    risk_level       TEXT NOT NULL CHECK (risk_level IN ('safe', 'irreversible')),
    requires_approval BOOLEAN NOT NULL DEFAULT false,
    mock_handler     TEXT NOT NULL DEFAULT 'echo',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE runs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    idempotency_key  TEXT NOT NULL,
    status           TEXT NOT NULL CHECK (status IN (
        'queued', 'awaiting_approval', 'running', 'succeeded', 'failed', 'rejected', 'cancelled'
    )),
    input_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_json      JSONB,
    error_message    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, idempotency_key)
);

CREATE TABLE run_steps (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    workspace_id      UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    tool_id           UUID NOT NULL REFERENCES tools(id),
    step_index        INT NOT NULL,
    status            TEXT NOT NULL CHECK (status IN (
        'pending', 'awaiting_approval', 'claimed', 'running', 'succeeded', 'failed', 'rejected'
    )),
    input_json        JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_json       JSONB,
    error_message     TEXT,
    attempt_count     INT NOT NULL DEFAULT 0,
    max_attempts      INT NOT NULL DEFAULT 5,
    next_attempt_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_by        TEXT,
    claim_expires_at  TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, step_index)
);

CREATE TABLE step_attempts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    step_id       UUID NOT NULL REFERENCES run_steps(id) ON DELETE CASCADE,
    attempt       INT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    output_json   JSONB,
    error_message TEXT,
    UNIQUE (step_id, attempt)
);

CREATE TABLE approvals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    step_id      UUID NOT NULL UNIQUE REFERENCES run_steps(id) ON DELETE CASCADE,
    run_id       UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at   TIMESTAMPTZ,
    decided_by   UUID REFERENCES users(id),
    reason       TEXT
);

-- Append-only audit trail shown in the inbox timeline.
CREATE TABLE events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    run_id       UUID REFERENCES runs(id) ON DELETE CASCADE,
    step_id      UUID REFERENCES run_steps(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    message      TEXT NOT NULL,
    meta_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Postgres job queue claimed with FOR UPDATE SKIP LOCKED.
CREATE TABLE jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    step_id      UUID NOT NULL REFERENCES run_steps(id) ON DELETE CASCADE,
    job_type     TEXT NOT NULL DEFAULT 'execute_step',
    status       TEXT NOT NULL CHECK (status IN ('pending', 'claimed', 'done', 'dead')),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts     INT NOT NULL DEFAULT 0,
    claimed_by   TEXT,
    claim_expires_at TIMESTAMPTZ,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_jobs_claim ON jobs (status, available_at) WHERE status = 'pending';
CREATE INDEX idx_run_steps_status ON run_steps (workspace_id, status);
CREATE INDEX idx_approvals_pending ON approvals (workspace_id, status) WHERE status = 'pending';
CREATE INDEX idx_events_run ON events (run_id, created_at);
CREATE INDEX idx_runs_workspace_created ON runs (workspace_id, created_at DESC);
