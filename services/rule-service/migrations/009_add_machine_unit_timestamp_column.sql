ALTER TABLE machine_units
ADD COLUMN IF NOT EXISTS timestamp_column text NOT NULL DEFAULT '';
