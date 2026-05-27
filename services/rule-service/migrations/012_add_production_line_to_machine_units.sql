ALTER TABLE machine_units
  ADD COLUMN IF NOT EXISTS production_line_id uuid
  REFERENCES production_lines(line_id) ON DELETE SET NULL;

-- Assign all existing machine units to the default production line.
UPDATE machine_units
SET production_line_id = '00000000-0000-0000-0000-000000000001'
WHERE production_line_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_machine_units_production_line_id
  ON machine_units(production_line_id);
