// file: mssql_connector.go
package dbconnector

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
)

type MSSQLConnector struct {
	baseConnector
}

func newMSSQLConnector(cfg ConnectionConfig) (*MSSQLConnector, error) {
	if cfg.Port == 0 {
		cfg.Port = 1433
	}
	user := url.QueryEscape(cfg.User)
	pass := url.QueryEscape(cfg.Password)
	sslMode := strings.ToLower(strings.TrimSpace(cfg.SSLMode))
	encrypt := "true"
	if sslMode == "disable" {
		encrypt = "disable"
	}
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=%s", user, pass, cfg.Host, cfg.Port, cfg.Database, encrypt)
	db, err := openDatabase("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mssql connection: %w", err)
	}
	return &MSSQLConnector{baseConnector{cfg: cfg, db: db}}, nil
}

func (c *MSSQLConnector) TestConnection(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mssql: %w", err)
	}
	return nil
}

func (c *MSSQLConnector) ListTables(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, "SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_CATALOG = DB_NAME()")
	if err != nil {
		return nil, fmt.Errorf("list mssql tables: %w", err)
	}
	defer rows.Close()
	results := []string{}
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return nil, fmt.Errorf("scan mssql table name: %w", err)
		}
		results = append(results, fmt.Sprintf("%s.%s", schema, name))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mssql tables: %w", err)
	}
	return results, nil
}

func (c *MSSQLConnector) DescribeTable(ctx context.Context, table string) (*TableSchema, error) {
	schema, name, err := parseMSSQLTable(table)
	if err != nil {
		return nil, err
	}
	colsStmt, err := c.db.PrepareContext(ctx, "SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_CATALOG = DB_NAME() AND TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2 ORDER BY ORDINAL_POSITION")
	if err != nil {
		return nil, fmt.Errorf("prepare mssql columns query: %w", err)
	}
	defer colsStmt.Close()
	rows, err := colsStmt.QueryContext(ctx, schema, name)
	if err != nil {
		return nil, fmt.Errorf("query mssql columns: %w", err)
	}
	defer rows.Close()
	columns := []ColumnInfo{}
	for rows.Next() {
		var colName, dataType, isNullable string
		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("scan mssql column: %w", err)
		}
		columns = append(columns, ColumnInfo{
			Name:     colName,
			Type:     dataType,
			Nullable: strings.EqualFold(isNullable, "YES"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mssql columns: %w", err)
	}

	pkStmt, err := c.db.PrepareContext(ctx, "SELECT kcu.COLUMN_NAME FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND tc.TABLE_SCHEMA = @p1 AND tc.TABLE_NAME = @p2 ORDER BY kcu.ORDINAL_POSITION")
	if err != nil {
		return nil, fmt.Errorf("prepare mssql pk query: %w", err)
	}
	defer pkStmt.Close()
	pkRows, err := pkStmt.QueryContext(ctx, schema, name)
	if err != nil {
		return nil, fmt.Errorf("query mssql pk columns: %w", err)
	}
	defer pkRows.Close()
	pkSet := map[string]struct{}{}
	for pkRows.Next() {
		var col string
		if err := pkRows.Scan(&col); err != nil {
			return nil, fmt.Errorf("scan mssql pk column: %w", err)
		}
		pkSet[col] = struct{}{}
	}
	if err := pkRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mssql pk columns: %w", err)
	}
	for i, col := range columns {
		if _, ok := pkSet[col.Name]; ok {
			columns[i].IsPK = true
		}
	}

	idxStmt, err := c.db.PrepareContext(ctx, "SELECT i.name, i.is_unique, c.name, ic.key_ordinal FROM sys.indexes i JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id JOIN sys.tables t ON i.object_id = t.object_id JOIN sys.schemas s ON t.schema_id = s.schema_id WHERE t.name = @p1 AND s.name = @p2 AND i.is_hypothetical = 0 AND i.type_desc <> 'HEAP' ORDER BY i.name, ic.key_ordinal")
	if err != nil {
		return nil, fmt.Errorf("prepare mssql index query: %w", err)
	}
	defer idxStmt.Close()
	idxRows, err := idxStmt.QueryContext(ctx, name, schema)
	if err != nil {
		return nil, fmt.Errorf("query mssql indexes: %w", err)
	}
	defer idxRows.Close()
	indexMap := map[string]*IndexInfo{}
	for idxRows.Next() {
		var idxName string
		var unique bool
		var col string
		var ord int
		if err := idxRows.Scan(&idxName, &unique, &col, &ord); err != nil {
			return nil, fmt.Errorf("scan mssql index: %w", err)
		}
		idx, ok := indexMap[idxName]
		if !ok {
			idx = &IndexInfo{Name: idxName, Unique: unique, Columns: []string{}}
			indexMap[idxName] = idx
		}
		idx.Columns = append(idx.Columns, col)
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mssql indexes: %w", err)
	}
	indexes := []IndexInfo{}
	for _, idx := range indexMap {
		indexes = append(indexes, *idx)
	}
	indexes = sortIndexColumns(indexes)
	return &TableSchema{Columns: columns, Indexes: indexes}, nil
}

func (c *MSSQLConnector) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return c.sampleRows(ctx, table, normalizeSampleLimit(limit), nil)
}

func (c *MSSQLConnector) ProfileTable(ctx context.Context, table string, opts ProfileOptions) (*TableProfile, error) {
	opts = normalizeProfileOptions(opts)
	schema, err := c.DescribeTable(ctx, table)
	if err != nil {
		return nil, err
	}
	rowCount, err := c.estimateRowCount(ctx, table)
	if err != nil {
		return nil, err
	}
	columns := schema.Columns
	if len(columns) > opts.MaxColumns {
		columns = columns[:opts.MaxColumns]
	}
	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = col.Name
	}
	sampleRows, err := c.sampleRows(ctx, table, opts.SampleLimit, columnNames)
	if err != nil {
		return nil, err
	}
	preview := sampleRows
	if len(preview) > maxSamplePreview {
		preview = preview[:maxSamplePreview]
	}
	profiling := profileFromSample(*schema, sampleRows, opts.MaxColumns)
	return &TableProfile{
		Table:         table,
		RowCount:      rowCount,
		Schema:        *schema,
		Profiling:     profiling,
		SamplePreview: preview,
	}, nil
}

func (c *MSSQLConnector) sampleRows(ctx context.Context, table string, limit int, columns []string) ([]map[string]any, error) {
	quotedTable, err := quoteMSSQLTable(table)
	if err != nil {
		return nil, err
	}
	selectClause := "*"
	if len(columns) > 0 {
		selectClause, err = quoteList(columns, func(s string) string { return "[" + s + "]" })
		if err != nil {
			return nil, fmt.Errorf("invalid mssql column list: %w", err)
		}
	}
	query := fmt.Sprintf("SELECT TOP (@p1) %s FROM %s", selectClause, quotedTable)
	stmt, err := c.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("prepare mssql sample query: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("query mssql sample rows: %w", err)
	}
	defer rows.Close()
	result, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, fmt.Errorf("scan mssql sample rows: %w", err)
	}
	return result, nil
}

func (c *MSSQLConnector) estimateRowCount(ctx context.Context, table string) (int64, error) {
	schema, name, err := parseMSSQLTable(table)
	if err != nil {
		return 0, err
	}
	stmt, err := c.db.PrepareContext(ctx, "SELECT SUM(ps.row_count) FROM sys.dm_db_partition_stats ps JOIN sys.tables t ON ps.object_id = t.object_id JOIN sys.schemas s ON t.schema_id = s.schema_id WHERE ps.index_id IN (0, 1) AND t.name = @p1 AND s.name = @p2")
	if err != nil {
		return 0, fmt.Errorf("prepare mssql row count query: %w", err)
	}
	defer stmt.Close()
	var count sql.NullInt64
	if err := stmt.QueryRowContext(ctx, name, schema).Scan(&count); err != nil {
		return 0, fmt.Errorf("query mssql row count: %w", err)
	}
	if !count.Valid {
		return 0, nil
	}
	return count.Int64, nil
}

func parseMSSQLTable(table string) (string, string, error) {
	_, parts, err := quoteQualified(table, 2, func(s string) string { return "[" + s + "]" })
	if err != nil {
		return "", "", fmt.Errorf("invalid mssql table: %w", err)
	}
	if len(parts) == 1 {
		return "dbo", parts[0], nil
	}
	return parts[0], parts[1], nil
}

func quoteMSSQLTable(table string) (string, error) {
	quoted, _, err := quoteQualified(table, 2, func(s string) string { return "[" + s + "]" })
	if err != nil {
		return "", fmt.Errorf("invalid mssql table: %w", err)
	}
	return quoted, nil
}

func (c *MSSQLConnector) WriteRow(ctx context.Context, table, valueColumn, timestampColumn string, value any, extraColumns map[string]any) (int64, error) {
	quotedTable, err := quoteMSSQLTable(table)
	if err != nil {
		return 0, err
	}
	quotedVal, err := quoteList([]string{valueColumn}, func(s string) string { return "[" + s + "]" })
	if err != nil {
		return 0, fmt.Errorf("invalid mssql value column: %w", err)
	}
	quotedTs, err := quoteList([]string{timestampColumn}, func(s string) string { return "[" + s + "]" })
	if err != nil {
		return 0, fmt.Errorf("invalid mssql timestamp column: %w", err)
	}

	colParts := []string{quotedVal, quotedTs}
	placeholders := []string{"@p1", "GETDATE()"}
	args := []any{value}
	idx := 2

	for k, v := range extraColumns {
		qk, qErr := quoteList([]string{k}, func(s string) string { return "[" + s + "]" })
		if qErr != nil {
			return 0, fmt.Errorf("invalid extra column %q: %w", k, qErr)
		}
		colParts = append(colParts, qk)
		placeholders = append(placeholders, fmt.Sprintf("@p%d", idx))
		args = append(args, v)
		idx++
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotedTable,
		strings.Join(colParts, ", "),
		strings.Join(placeholders, ", "),
	)
	result, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mssql write row: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mssql rows affected: %w", err)
	}
	return n, nil
}
