CREATE TABLE IF NOT EXISTS canvas_wires (
    id                TEXT        NOT NULL,
    production_line_id UUID       NOT NULL REFERENCES production_lines(line_id) ON DELETE CASCADE,
    source_unit_id    TEXT        NOT NULL,
    target_unit_id    TEXT        NOT NULL,
    source_offset_x   DOUBLE PRECISION NOT NULL DEFAULT 0,
    source_offset_y   DOUBLE PRECISION NOT NULL DEFAULT 0,
    target_offset_x   DOUBLE PRECISION NOT NULL DEFAULT 0,
    target_offset_y   DOUBLE PRECISION NOT NULL DEFAULT 0,
    label             TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS canvas_wires_line_idx ON canvas_wires(production_line_id);
