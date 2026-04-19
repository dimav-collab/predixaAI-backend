CREATE TABLE IF NOT EXISTS rules (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  description text,
  connection_ref uuid NOT NULL REFERENCES db_connections(id),
  parameter_name text NOT NULL,
  rule_json jsonb NOT NULL,
  enabled boolean NOT NULL,
  status text NOT NULL,
  last_error jsonb,
  last_validated_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);
