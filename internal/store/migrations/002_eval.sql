CREATE TABLE eval_runs (
  id INTEGER PRIMARY KEY,
  created_at INTEGER NOT NULL,
  finished_at INTEGER,
  status TEXT NOT NULL,
  mode TEXT NOT NULL,
  -- JSON arrays of route names and task IDs.
  routes TEXT NOT NULL,
  tasks TEXT NOT NULL,
  parallel INTEGER NOT NULL,
  total INTEGER NOT NULL,
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE eval_results (
  id INTEGER PRIMARY KEY,
  run_id INTEGER NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
  task TEXT NOT NULL,
  route TEXT NOT NULL,
  passed INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  requests INTEGER NOT NULL,
  escalated_requests INTEGER NOT NULL,
  cost_usd REAL NOT NULL,
  subscription_value_usd REAL NOT NULL,
  api_tokens INTEGER NOT NULL,
  subscription_tokens INTEGER NOT NULL,
  claude_output TEXT NOT NULL,
  test_output TEXT NOT NULL,
  error TEXT NOT NULL
);

CREATE INDEX eval_results_run ON eval_results (run_id);
