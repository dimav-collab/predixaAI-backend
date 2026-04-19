CREATE TABLE IF NOT EXISTS machine_units (
  unit_id text PRIMARY KEY,
  unit_name text NOT NULL,
  connection_ref uuid NOT NULL REFERENCES db_connections(id),
  selected_table text NOT NULL,
  selected_columns jsonb NOT NULL DEFAULT '[]'::jsonb,
  live_parameters jsonb NOT NULL DEFAULT '[]'::jsonb,
  rule_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_machine_units_connection_ref ON machine_units(connection_ref);
