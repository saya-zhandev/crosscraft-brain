-- crosscraft-brain core schema (vertical-agnostic).
-- Verticals add their own tables in a separate migration; they never edit this file.

CREATE TABLE IF NOT EXISTS workflows (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  graph       JSONB NOT NULL,
  active      BOOLEAN NOT NULL DEFAULT false,
  version     INTEGER NOT NULL DEFAULT 1,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS executions (
  id              TEXT PRIMARY KEY,
  workflow_id     TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  status          TEXT NOT NULL DEFAULT 'running',  -- running | waiting | success | error
  resume_token    TEXT,
  waiting_node_id TEXT,
  state           JSONB,                            -- serialized RunState for resume
  started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS executions_wf_idx ON executions (workflow_id, started_at DESC);

CREATE TABLE IF NOT EXISTS execution_steps (
  id            TEXT PRIMARY KEY,
  execution_id  TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  node_id       TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'running',    -- running | success | error
  input         JSONB NOT NULL DEFAULT '[]',
  output        JSONB NOT NULL DEFAULT '[]',
  logs          JSONB NOT NULL DEFAULT '[]',
  error         TEXT,
  started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS steps_exec_idx ON execution_steps (execution_id, started_at ASC);

CREATE TABLE IF NOT EXISTS credentials (
  id              TEXT PRIMARY KEY,
  type            TEXT NOT NULL,
  name            TEXT NOT NULL,
  data_encrypted  TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
