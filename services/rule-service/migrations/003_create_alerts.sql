CREATE TABLE IF NOT EXISTS alerts (
  id bigserial PRIMARY KEY,
  rule_id uuid NOT NULL REFERENCES rules(id),
  ts_utc timestamptz NOT NULL,
  parameter_name text NOT NULL,
  observed_value text NOT NULL,
  limit_expression text NOT NULL,
  hit boolean NOT NULL,
  treated boolean NOT NULL DEFAULT false,
  metadata jsonb
);
