-- Change machine_units.production_line_id FK to CASCADE so deleting a
-- production line also removes all its machine units (and their wires
-- via the existing canvas_wires FK).
ALTER TABLE machine_units
  DROP CONSTRAINT IF EXISTS machine_units_production_line_id_fkey;

ALTER TABLE machine_units
  ADD CONSTRAINT machine_units_production_line_id_fkey
  FOREIGN KEY (production_line_id)
  REFERENCES production_lines(line_id)
  ON DELETE CASCADE;
