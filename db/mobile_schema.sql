-- mobile / client-enablement schema for crosscraft-brain
-- Applied after schema.sql (pnpm db:migrate db/mobile_schema.sql)

CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT PRIMARY KEY,
    key_hash    TEXT UNIQUE NOT NULL,   -- SHA-256 hex of the bearer token
    name        TEXT NOT NULL,          -- human label (e.g. "iOS app", "Android scanner")
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS api_keys_hash_idx ON api_keys (key_hash);
