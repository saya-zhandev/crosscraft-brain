-- FarmersFront vertical tables (example of what a fork adds; the skeleton core is untouched).
-- Ported from the validated farmersback model. One lot = one engine execution:
-- lots.execution_id is the link, so farm nodes never need cross-node references.

CREATE TABLE IF NOT EXISTS farms (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  address     TEXT,
  gln         TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lots (
  id            BIGSERIAL PRIMARY KEY,
  farm_id       BIGINT REFERENCES farms(id),
  tlc           TEXT UNIQUE NOT NULL,
  commodity     TEXT NOT NULL,
  variety       TEXT,
  harvest_date  DATE,
  status        TEXT NOT NULL DEFAULT 'harvested',
  execution_id  TEXT,                 -- = crosscraft execution id (one lot = one run)
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS events (
  id           BIGSERIAL PRIMARY KEY,
  lot_id       BIGINT NOT NULL REFERENCES lots(id),
  stage        TEXT NOT NULL,
  cte_type     TEXT,
  kde          JSONB NOT NULL DEFAULT '{}',
  location     TEXT,
  actor        TEXT,
  photo_url    TEXT,
  occurred_at  TIMESTAMPTZ,
  recorded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS events_lot_idx ON events (lot_id, occurred_at);

INSERT INTO farms (name, address, gln)
SELECT 'Demo Family Farm', '1 Orchard Rd, Salinas, CA', '0614141000005'
WHERE NOT EXISTS (SELECT 1 FROM farms);
