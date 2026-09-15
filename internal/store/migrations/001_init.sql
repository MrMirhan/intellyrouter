CREATE TABLE providers (
  id INTEGER PRIMARY KEY,
  type TEXT NOT NULL,
  name TEXT NOT NULL UNIQUE,
  base_url TEXT NOT NULL DEFAULT '',
  api_key_enc BLOB,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

CREATE TABLE models (
  id INTEGER PRIMARY KEY,
  provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
  model_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  price_in REAL NOT NULL DEFAULT 0,
  price_out REAL NOT NULL DEFAULT 0,
  price_cache_read REAL NOT NULL DEFAULT 0,
  price_cache_write REAL NOT NULL DEFAULT 0,
  context INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 0,
  UNIQUE (provider_id, model_id)
);

CREATE TABLE routes (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  strategy TEXT NOT NULL,
  settings TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);

-- Position 0 is the base model; higher positions are escalation targets.
CREATE TABLE route_tiers (
  route_id INTEGER NOT NULL REFERENCES routes(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
  label TEXT NOT NULL,
  PRIMARY KEY (route_id, position)
);

CREATE INDEX route_tiers_model ON route_tiers (model_id);

CREATE TABLE gateway_keys (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at INTEGER
);

CREATE TABLE requests (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  agent_id TEXT NOT NULL DEFAULT '',
  route TEXT NOT NULL,
  strategy TEXT NOT NULL,
  client_model TEXT NOT NULL,
  stream INTEGER NOT NULL,
  status TEXT NOT NULL,
  http_status INTEGER NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  -- API spend; subscription legs are excluded and valued separately.
  cost_usd REAL NOT NULL DEFAULT 0,
  subscription_value_usd REAL NOT NULL DEFAULT 0,
  reference_cost_usd REAL NOT NULL DEFAULT 0,
  latency_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX requests_ts ON requests (ts);
CREATE INDEX requests_session ON requests (session_id);

CREATE TABLE legs (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  role TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  -- 'api' or 'subscription'; cost_usd is the API-equivalent price either way.
  billing TEXT NOT NULL DEFAULT 'api',
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  cost_usd REAL NOT NULL DEFAULT 0,
  latency_ms INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  stop_reason TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT ''
);

CREATE INDEX legs_request ON legs (request_id);

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
