ALTER TABLE machine_units
  ADD COLUMN IF NOT EXISTS pos_x double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS pos_y double precision NOT NULL DEFAULT 0;
