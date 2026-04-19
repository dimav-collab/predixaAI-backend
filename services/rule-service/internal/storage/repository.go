package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Repository struct {
	Store *Store
}

func NewRepository(store *Store) *Repository {
	return &Repository{Store: store}
}

func (r *Repository) CreateConnection(ctx context.Context, conn DBConnection) (string, error) {
	id := uuid.NewString()
	_, err := r.Store.Pool.Exec(ctx, `
		INSERT INTO db_connections (id, name, type, host, port, user_name, password_enc, database, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())`,
		id, conn.Name, conn.Type, conn.Host, conn.Port, conn.User, conn.Password, conn.Database,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) GetRule(ctx context.Context, id string) (RuleRecord, error) {
	row := r.Store.Pool.QueryRow(ctx, `
		SELECT id, name, description, connection_ref, parameter_name, rule_json, enabled, status, last_error, last_validated_at, created_at, updated_at
		FROM rules WHERE id=$1`, id)
	var rec RuleRecord
	if err := row.Scan(&rec.ID, &rec.Name, &rec.Description, &rec.ConnectionRef, &rec.ParameterName, &rec.RuleJSON, &rec.Enabled, &rec.Status, &rec.LastError, &rec.LastValidatedAt, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return RuleRecord{}, ErrNotFound
	}
	return rec, nil
}

func (r *Repository) ListRules(ctx context.Context) ([]RuleRecord, error) {
	rows, err := r.Store.Pool.Query(ctx, `
		SELECT id, name, description, connection_ref, parameter_name, rule_json, enabled, status, last_error, last_validated_at, created_at, updated_at
		FROM rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []RuleRecord{}
	for rows.Next() {
		var rec RuleRecord
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Description, &rec.ConnectionRef, &rec.ParameterName, &rec.RuleJSON, &rec.Enabled, &rec.Status, &rec.LastError, &rec.LastValidatedAt, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, nil
}

func (r *Repository) CreateRule(ctx context.Context, rec RuleRecord) (string, error) {
	id := uuid.NewString()
	_, err := r.Store.Pool.Exec(ctx, `
		INSERT INTO rules (id, name, description, connection_ref, parameter_name, rule_json, enabled, status, last_error, last_validated_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),now())`,
		id, rec.Name, rec.Description, rec.ConnectionRef, rec.ParameterName, rec.RuleJSON, rec.Enabled, rec.Status, rec.LastError, rec.LastValidatedAt,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) UpdateRule(ctx context.Context, rec RuleRecord) error {
	_, err := r.Store.Pool.Exec(ctx, `
		UPDATE rules
		SET name=$1, description=$2, parameter_name=$3, rule_json=$4, enabled=$5, status=$6, last_error=$7, last_validated_at=$8, updated_at=now()
		WHERE id=$9`,
		rec.Name, rec.Description, rec.ParameterName, rec.RuleJSON, rec.Enabled, rec.Status, rec.LastError, rec.LastValidatedAt, rec.ID,
	)
	return err
}

func (r *Repository) SetRuleEnabled(ctx context.Context, id string, enabled bool, status string) error {
	_, err := r.Store.Pool.Exec(ctx, `UPDATE rules SET enabled=$1, status=$2, updated_at=now() WHERE id=$3`, enabled, status, id)
	return err
}

// UpsertStepperRuleToRules inserts or updates a row in the `rules` table using the same UUID
// as the ui_rules record. This allows the scheduler to pick up stepper rules.
func (r *Repository) UpsertStepperRuleToRules(ctx context.Context, rec RuleRecord) error {
	_, err := r.Store.Pool.Exec(ctx, `
		INSERT INTO rules (id, name, description, connection_ref, parameter_name, rule_json, enabled, status, last_error, last_validated_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),now())
		ON CONFLICT (id) DO UPDATE
		  SET name=$2, description=$3, connection_ref=$4, parameter_name=$5,
		      rule_json=$6, enabled=$7, status=$8, last_error=$9, last_validated_at=$10,
		      updated_at=now()`,
		rec.ID, rec.Name, rec.Description, rec.ConnectionRef, rec.ParameterName,
		rec.RuleJSON, rec.Enabled, rec.Status, rec.LastError, rec.LastValidatedAt,
	)
	return err
}

// DeleteRule removes a rule from the `rules` table by ID.
func (r *Repository) DeleteRule(ctx context.Context, id string) error {
	_, err := r.Store.Pool.Exec(ctx, `DELETE FROM rules WHERE id=$1`, id)
	return err
}

func (r *Repository) ListAlerts(ctx context.Context, ruleID string) ([]AlertRecord, error) {
	rows, err := r.Store.Pool.Query(ctx, `
		SELECT id, rule_id, ts_utc, parameter_name, observed_value, limit_expression, detector_type, severity, anomaly_score, baseline_median, baseline_mad, hit, treated, metadata
		FROM alerts WHERE rule_id=$1 ORDER BY ts_utc DESC`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []AlertRecord{}
	for rows.Next() {
		var rec AlertRecord
		if err := rows.Scan(&rec.ID, &rec.RuleID, &rec.TSUTC, &rec.ParameterName, &rec.ObservedValue, &rec.LimitExpr, &rec.DetectorType, &rec.Severity, &rec.AnomalyScore, &rec.BaselineMedian, &rec.BaselineMAD, &rec.Hit, &rec.Treated, &rec.Metadata); err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, nil
}

func (r *Repository) UpdateAlertTreated(ctx context.Context, alertID int64, treated bool) error {
	_, err := r.Store.Pool.Exec(ctx, `UPDATE alerts SET treated=$1 WHERE id=$2`, treated, alertID)
	return err
}

func (r *Repository) CreateAlert(ctx context.Context, alert AlertRecord) error {
	_, err := r.Store.Pool.Exec(ctx, `
		INSERT INTO alerts (rule_id, ts_utc, parameter_name, observed_value, limit_expression, detector_type, severity, anomaly_score, baseline_median, baseline_mad, hit, treated, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		alert.RuleID, alert.TSUTC, alert.ParameterName, alert.ObservedValue, alert.LimitExpr, alert.DetectorType, alert.Severity, alert.AnomalyScore, alert.BaselineMedian, alert.BaselineMAD, alert.Hit, alert.Treated, alert.Metadata)
	return err
}

func (r *Repository) ConnectionExists(ctx context.Context, id string) (bool, error) {
	row := r.Store.Pool.QueryRow(ctx, `SELECT 1 FROM db_connections WHERE id=$1`, id)
	var exists int
	if err := row.Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *Repository) ListRuleIDs(ctx context.Context, ids []string) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.Store.Pool.Query(ctx, `SELECT id FROM rules WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = struct{}{}
	}
	return result, nil
}

func (r *Repository) CreateMachineUnit(ctx context.Context, unit MachineUnit) (MachineUnit, error) {
	selectedColumnsJSON, err := json.Marshal(unit.SelectedColumns)
	if err != nil {
		return MachineUnit{}, err
	}
	ruleIDsJSON, err := json.Marshal(unit.RuleIDs)
	if err != nil {
		return MachineUnit{}, err
	}
	liveParamsJSON := normalizeRawJSON(unit.LiveParameters)
	row := r.Store.Pool.QueryRow(ctx, `
		INSERT INTO machine_units (unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),now())
		RETURNING unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at`,
		unit.UnitID, unit.UnitName, unit.ConnectionRef, unit.SelectedTable, unit.TimestampColumn, selectedColumnsJSON, liveParamsJSON, ruleIDsJSON, unit.PosX, unit.PosY,
	)
	return scanMachineUnit(row)
}

func (r *Repository) ListMachineUnits(ctx context.Context) ([]MachineUnit, error) {
	rows, err := r.Store.Pool.Query(ctx, `
		SELECT unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at
		FROM machine_units ORDER BY created_at DESC`)
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

func (r *Repository) GetMachineUnit(ctx context.Context, unitID string) (MachineUnit, error) {
	row := r.Store.Pool.QueryRow(ctx, `
		SELECT unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at
		FROM machine_units WHERE unit_id=$1`, unitID)
	unit, err := scanMachineUnit(row)
	if err != nil {
		if err == ErrNotFound {
			return MachineUnit{}, ErrNotFound
		}
		return MachineUnit{}, err
	}
	return unit, nil
}

func (r *Repository) UpdateMachineUnit(ctx context.Context, unit MachineUnit) (MachineUnit, error) {
	selectedColumnsJSON, err := json.Marshal(unit.SelectedColumns)
	if err != nil {
		return MachineUnit{}, err
	}
	ruleIDsJSON, err := json.Marshal(unit.RuleIDs)
	if err != nil {
		return MachineUnit{}, err
	}
	liveParamsJSON := normalizeRawJSON(unit.LiveParameters)
	row := r.Store.Pool.QueryRow(ctx, `
		UPDATE machine_units
		SET unit_name=$1, connection_ref=$2, selected_table=$3, timestamp_column=$4, selected_columns=$5, live_parameters=$6, rule_ids=$7, pos_x=$8, pos_y=$9, updated_at=now()
		WHERE unit_id=$10
		RETURNING unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at`,
		unit.UnitName, unit.ConnectionRef, unit.SelectedTable, unit.TimestampColumn, selectedColumnsJSON, liveParamsJSON, ruleIDsJSON, unit.PosX, unit.PosY, unit.UnitID,
	)
	updated, err := scanMachineUnit(row)
	if err != nil {
		return MachineUnit{}, err
	}
	return updated, nil
}

func (r *Repository) DeleteMachineUnit(ctx context.Context, unitID string) error {
	cmd, err := r.Store.Pool.Exec(ctx, `DELETE FROM machine_units WHERE unit_id=$1`, unitID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) UpdateRules(ctx context.Context, unitID string, add []string, remove []string) (MachineUnit, error) {
	return r.updateMachineUnitJSONArrays(ctx, unitID, add, remove, "rule_ids")
}

func (r *Repository) UpdateColumns(ctx context.Context, unitID string, add []string, remove []string) (MachineUnit, error) {
	return r.updateMachineUnitJSONArrays(ctx, unitID, add, remove, "selected_columns")
}

func (r *Repository) UpdateTable(ctx context.Context, unitID string, table string, columns *[]string, keepColumns bool) (MachineUnit, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := r.Store.Pool.Begin(ctx)
	if err != nil {
		return MachineUnit{}, err
	}
	defer tx.Rollback(ctx)

	var currentColumnsRaw []byte
	row := tx.QueryRow(ctx, `SELECT selected_columns FROM machine_units WHERE unit_id=$1 FOR UPDATE`, unitID)
	if err := row.Scan(&currentColumnsRaw); err != nil {
		return MachineUnit{}, ErrNotFound
	}
	currentColumns, err := decodeStringArray(currentColumnsRaw)
	if err != nil {
		return MachineUnit{}, err
	}

	var nextColumns []string
	if columns != nil {
		nextColumns = *columns
	} else if keepColumns {
		nextColumns = currentColumns
	} else {
		nextColumns = []string{}
	}

	columnsJSON, err := json.Marshal(nextColumns)
	if err != nil {
		return MachineUnit{}, err
	}

	updatedRow := tx.QueryRow(ctx, `
		UPDATE machine_units
		SET selected_table=$1, timestamp_column=$2, selected_columns=$3, updated_at=now()
		WHERE unit_id=$4
		RETURNING unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at`,
		table, "", columnsJSON, unitID,
	)
	updated, err := scanMachineUnit(updatedRow)
	if err != nil {
		return MachineUnit{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MachineUnit{}, err
	}
	return updated, nil
}

func (r *Repository) UpdateConnection(ctx context.Context, unitID string, connectionRef string) (MachineUnit, error) {
	row := r.Store.Pool.QueryRow(ctx, `
		UPDATE machine_units SET connection_ref=$1, updated_at=now()
		WHERE unit_id=$2
		RETURNING unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at`,
		connectionRef, unitID,
	)
	updated, err := scanMachineUnit(row)
	if err != nil {
		return MachineUnit{}, err
	}
	return updated, nil
}

func (r *Repository) UpdatePosition(ctx context.Context, unitID string, x float64, y float64) (MachineUnit, error) {
	row := r.Store.Pool.QueryRow(ctx, `
		UPDATE machine_units SET pos_x=$1, pos_y=$2, updated_at=now()
		WHERE unit_id=$3
		RETURNING unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at`,
		x, y, unitID,
	)
	updated, err := scanMachineUnit(row)
	if err != nil {
		return MachineUnit{}, err
	}
	return updated, nil
}

func (r *Repository) updateMachineUnitJSONArrays(ctx context.Context, unitID string, add []string, remove []string, column string) (MachineUnit, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := r.Store.Pool.Begin(ctx)
	if err != nil {
		return MachineUnit{}, err
	}
	defer tx.Rollback(ctx)

	query := `SELECT ` + column + ` FROM machine_units WHERE unit_id=$1 FOR UPDATE`
	var currentRaw []byte
	if err := tx.QueryRow(ctx, query, unitID).Scan(&currentRaw); err != nil {
		return MachineUnit{}, ErrNotFound
	}
	current, err := decodeStringArray(currentRaw)
	if err != nil {
		return MachineUnit{}, err
	}
	updatedList := applyAddRemove(current, add, remove)
	updatedJSON, err := json.Marshal(updatedList)
	if err != nil {
		return MachineUnit{}, err
	}
	updateQuery := `UPDATE machine_units SET ` + column + `=$1, updated_at=now() WHERE unit_id=$2
		RETURNING unit_id, unit_name, connection_ref, selected_table, timestamp_column, selected_columns, live_parameters, rule_ids, pos_x, pos_y, created_at, updated_at`
	row := tx.QueryRow(ctx, updateQuery, updatedJSON, unitID)
	unit, err := scanMachineUnit(row)
	if err != nil {
		return MachineUnit{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MachineUnit{}, err
	}
	return unit, nil
}

func applyAddRemove(current []string, add []string, remove []string) []string {
	result := make([]string, 0, len(current)+len(add))
	seen := map[string]bool{}
	removeSet := map[string]bool{}
	for _, id := range remove {
		removeSet[id] = true
	}
	for _, value := range current {
		if removeSet[value] {
			continue
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, value := range add {
		if removeSet[value] {
			continue
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func scanMachineUnit(row pgx.Row) (MachineUnit, error) {
	var unit MachineUnit
	var selectedColumnsRaw []byte
	var liveParamsRaw []byte
	var ruleIDsRaw []byte
	if err := row.Scan(&unit.UnitID, &unit.UnitName, &unit.ConnectionRef, &unit.SelectedTable, &unit.TimestampColumn, &selectedColumnsRaw, &liveParamsRaw, &ruleIDsRaw, &unit.PosX, &unit.PosY, &unit.CreatedAt, &unit.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return MachineUnit{}, ErrNotFound
		}
		return MachineUnit{}, err
	}
	selectedColumns, err := decodeStringArray(selectedColumnsRaw)
	if err != nil {
		return MachineUnit{}, err
	}
	ruleIDs, err := decodeStringArray(ruleIDsRaw)
	if err != nil {
		return MachineUnit{}, err
	}
	unit.SelectedColumns = selectedColumns
	unit.RuleIDs = ruleIDs
	unit.LiveParameters = normalizeRawJSON(liveParamsRaw)
	return unit, nil
}

func decodeStringArray(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var result []string
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeRawJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte("[]")
	}
	return raw
}

func nowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}
