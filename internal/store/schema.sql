CREATE TABLE IF NOT EXISTS orders (
  id            BIGSERIAL PRIMARY KEY,
  store_id      TEXT NOT NULL,
  external_id   TEXT NOT NULL,
  name          TEXT NOT NULL DEFAULT '',
  email         TEXT NOT NULL,
  customer_name TEXT NOT NULL DEFAULT '',
  currency      TEXT NOT NULL DEFAULT '',
  total         TEXT NOT NULL DEFAULT '',
  country       TEXT NOT NULL DEFAULT '',
  items         JSONB NOT NULL DEFAULT '[]',
  paid_at       TIMESTAMPTZ NOT NULL,
  tracking_number TEXT NOT NULL DEFAULT '',
  carrier       TEXT NOT NULL DEFAULT '',
  tracking_url  TEXT NOT NULL DEFAULT '',
  dispatched_at TIMESTAMPTZ,
  canceled      BOOLEAN NOT NULL DEFAULT false,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (store_id, external_id)
);
CREATE INDEX IF NOT EXISTS orders_store_paid ON orders (store_id, paid_at DESC);

CREATE TABLE IF NOT EXISTS tracking_events (
  id       BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  event    TEXT NOT NULL,
  label    TEXT NOT NULL DEFAULT '',
  source   TEXT NOT NULL DEFAULT '',
  at       TIMESTAMPTZ NOT NULL,
  UNIQUE (order_id, event)
);

CREATE TABLE IF NOT EXISTS jobs (
  id         BIGSERIAL PRIMARY KEY,
  order_id   BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  store_id   TEXT NOT NULL,
  step_id    TEXT NOT NULL,
  due_at     TIMESTAMPTZ NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  held       BOOLEAN NOT NULL DEFAULT false,
  attempts   INT NOT NULL DEFAULT 0,
  reason     TEXT NOT NULL DEFAULT '',
  message_id TEXT NOT NULL DEFAULT '',
  sent_at    TIMESTAMPTZ,
  claimed_at TIMESTAMPTZ,
  UNIQUE (order_id, step_id)
);
CREATE INDEX IF NOT EXISTS jobs_due ON jobs (due_at) WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS step_overrides (
  store_id TEXT NOT NULL,
  step_id  TEXT NOT NULL,
  status   TEXT NOT NULL,
  PRIMARY KEY (store_id, step_id)
);
