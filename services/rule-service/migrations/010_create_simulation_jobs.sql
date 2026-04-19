CREATE TABLE IF NOT EXISTS simulation_jobs (
  id           uuid PRIMARY KEY,
  rule_id      uuid NOT NULL,
  status       text NOT NULL DEFAULT 'stopped',  -- 'running' | 'stopped' | 'error'
  mode         text NOT NULL DEFAULT 'normal',   -- 'normal' | 'violation' | 'trend_up' | 'trend_down' | 'spike'
  interval_seconds int NOT NULL DEFAULT 3,
  inserted_count   int NOT NULL DEFAULT 0,
  last_error   text,
  config       jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_simulation_jobs_rule_id ON simulation_jobs (rule_id);
