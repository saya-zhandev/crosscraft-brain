-- mobile / client-enablement schema for crosscraft-brain
-- Applied after schema.sql (pnpm db:migrate db/mobile_schema.sql)

CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT PRIMARY KEY,
    key_hash    TEXT UNIQUE NOT NULL,   -- SHA-256 hex of the bearer token
    name        TEXT NOT NULL,          -- human label (e.g. "iOS app", "Android scanner")
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS api_keys_hash_idx ON api_keys (key_hash);

-- Device tokens for push notification targeting.
CREATE TABLE IF NOT EXISTS device_tokens (
    id           TEXT PRIMARY KEY,
    api_key_id   TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    device_token TEXT NOT NULL,
    platform     TEXT NOT NULL,  -- 'ios' or 'android'
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_tokens_key_idx ON device_tokens (api_key_id);
CREATE UNIQUE INDEX IF NOT EXISTS device_tokens_token_idx ON device_tokens (device_token);
