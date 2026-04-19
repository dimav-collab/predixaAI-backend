ALTER TABLE rules
  ADD COLUMN IF NOT EXISTS last_run_at timestamptz;
