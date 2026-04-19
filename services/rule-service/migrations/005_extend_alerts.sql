ALTER TABLE alerts
  ADD COLUMN IF NOT EXISTS detector_type text,
  ADD COLUMN IF NOT EXISTS severity text,
  ADD COLUMN IF NOT EXISTS anomaly_score numeric,
  ADD COLUMN IF NOT EXISTS baseline_median numeric,
  ADD COLUMN IF NOT EXISTS baseline_mad numeric;
