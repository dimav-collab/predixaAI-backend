-- Parameter statistics computed from historical data at machine unit setup time.
-- One row per (unit_id, parameter_id) – UPSERT replaces on re-compute.

CREATE TABLE IF NOT EXISTS parameter_stats (
  id            TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::text,
  unit_id       TEXT        NOT NULL REFERENCES machine_units(unit_id) ON DELETE CASCADE,
  parameter_id  TEXT        NOT NULL,
  column_name   TEXT        NOT NULL,
  table_name    TEXT        NOT NULL,
  row_count     INTEGER     NOT NULL DEFAULT 0,
  avg_val       NUMERIC(24,6),
  min_val       NUMERIC(24,6),
  max_val       NUMERIC(24,6),
  std_dev       NUMERIC(24,6),
  sigma2_lower  NUMERIC(24,6),
  sigma2_upper  NUMERIC(24,6),
  sigma3_lower  NUMERIC(24,6),
  sigma3_upper  NUMERIC(24,6),
  computed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (unit_id, parameter_id)
);
