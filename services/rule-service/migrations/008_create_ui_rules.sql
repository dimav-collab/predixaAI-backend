CREATE TABLE IF NOT EXISTS ui_rules (
  id uuid PRIMARY KEY,
  unit_id text NOT NULL,
  name text NOT NULL,
  rule_type text NOT NULL,
  parameter_id text NOT NULL,
  config jsonb NOT NULL DEFAULT '{}'::jsonb,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ui_rules_unit_id ON ui_rules (unit_id);
