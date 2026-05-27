package storage

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListProductionLines returns all production lines ordered by creation date.
func (r *Repository) ListProductionLines(ctx context.Context) ([]ProductionLine, error) {
	rows, err := r.Store.Pool.Query(ctx,
		`SELECT line_id, line_name, created_at, updated_at
		 FROM production_lines ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []ProductionLine{}
	for rows.Next() {
		var pl ProductionLine
		if err := rows.Scan(&pl.LineID, &pl.LineName, &pl.CreatedAt, &pl.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, pl)
	}
	return results, nil
}

// GetProductionLine returns a single production line by ID.
func (r *Repository) GetProductionLine(ctx context.Context, lineID string) (ProductionLine, error) {
	row := r.Store.Pool.QueryRow(ctx,
		`SELECT line_id, line_name, created_at, updated_at
		 FROM production_lines WHERE line_id=$1`, lineID)
	var pl ProductionLine
	if err := row.Scan(&pl.LineID, &pl.LineName, &pl.CreatedAt, &pl.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return ProductionLine{}, ErrNotFound
		}
		return ProductionLine{}, err
	}
	return pl, nil
}

// CreateProductionLine inserts a new production line and returns it.
func (r *Repository) CreateProductionLine(ctx context.Context, name string) (ProductionLine, error) {
	id := uuid.NewString()
	row := r.Store.Pool.QueryRow(ctx,
		`INSERT INTO production_lines (line_id, line_name, created_at, updated_at)
		 VALUES ($1, $2, now(), now())
		 RETURNING line_id, line_name, created_at, updated_at`,
		id, name)
	var pl ProductionLine
	if err := row.Scan(&pl.LineID, &pl.LineName, &pl.CreatedAt, &pl.UpdatedAt); err != nil {
		return ProductionLine{}, err
	}
	return pl, nil
}

// UpdateProductionLine renames a production line.
func (r *Repository) UpdateProductionLine(ctx context.Context, lineID, name string) (ProductionLine, error) {
	row := r.Store.Pool.QueryRow(ctx,
		`UPDATE production_lines SET line_name=$1, updated_at=now()
		 WHERE line_id=$2
		 RETURNING line_id, line_name, created_at, updated_at`,
		name, lineID)
	var pl ProductionLine
	if err := row.Scan(&pl.LineID, &pl.LineName, &pl.CreatedAt, &pl.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return ProductionLine{}, ErrNotFound
		}
		return ProductionLine{}, err
	}
	return pl, nil
}

// DeleteProductionLine deletes a production line. Machine units in this line
// will have their production_line_id set to NULL (via ON DELETE SET NULL).
func (r *Repository) DeleteProductionLine(ctx context.Context, lineID string) error {
	cmd, err := r.Store.Pool.Exec(ctx, `DELETE FROM production_lines WHERE line_id=$1`, lineID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMachineUnitsByLine returns all machine units for a given production line.
func (r *Repository) ListMachineUnitsByLine(ctx context.Context, lineID string) ([]MachineUnit, error) {
	rows, err := r.Store.Pool.Query(ctx,
		`SELECT unit_id, unit_name, connection_ref, selected_table, timestamp_column,
		        selected_columns, live_parameters, rule_ids, pos_x, pos_y,
		        production_line_id, created_at, updated_at
		 FROM machine_units
		 WHERE production_line_id=$1
		 ORDER BY created_at DESC`, lineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []MachineUnit{}
	for rows.Next() {
		unit, err := scanMachineUnit(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, unit)
	}
	return results, nil
}
