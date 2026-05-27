CREATE TABLE IF NOT EXISTS production_lines (
  line_id   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  line_name text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- Seed a default production line so existing machine units can be assigned to it.
INSERT INTO production_lines (line_id, line_name)
VALUES ('00000000-0000-0000-0000-000000000001', 'Production Line 1')
ON CONFLICT (line_id) DO NOTHING;
