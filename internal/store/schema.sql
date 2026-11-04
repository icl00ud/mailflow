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

-- Tenancy: a tenant owns one or more stores; people belong to tenants.
CREATE TABLE IF NOT EXISTS tenants (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- store_id is unique: a store belongs to exactly one tenant.
CREATE TABLE IF NOT EXISTS tenant_stores (
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  store_id  TEXT NOT NULL,
  PRIMARY KEY (tenant_id, store_id),
  UNIQUE (store_id)
);

CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL DEFAULT '', -- empty until the invite is accepted
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (email = lower(email))
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower ON users (lower(email));

CREATE TABLE IF NOT EXISTS memberships (
  user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  role      TEXT NOT NULL DEFAULT 'member',
  PRIMARY KEY (user_id, tenant_id),
  CHECK (role IN ('owner','member'))
);
CREATE INDEX IF NOT EXISTS memberships_tenant ON memberships (tenant_id);

-- Only the SHA-256 of the cookie token is stored.
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS invites (
  id         TEXT PRIMARY KEY,
  tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email      TEXT NOT NULL,
  role       TEXT NOT NULL DEFAULT 'member',
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at    TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (role IN ('owner','member'))
);
CREATE INDEX IF NOT EXISTS invites_tenant ON invites (tenant_id);

CREATE TABLE IF NOT EXISTS webauthn_credentials (
  id           BYTEA PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  public_key   BYTEA NOT NULL,
  sign_count   BIGINT NOT NULL DEFAULT 0,
  transports   TEXT NOT NULL DEFAULT '[]',
  name         TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS webauthn_credentials_user ON webauthn_credentials (user_id);

-- WebAuthn ceremony state: short-lived and single use (deleted when taken).
CREATE TABLE IF NOT EXISTS webauthn_challenges (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL DEFAULT '',
  kind       TEXT NOT NULL,
  data       BYTEA NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS webauthn_challenges_expires ON webauthn_challenges (expires_at);

