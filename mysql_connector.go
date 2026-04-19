// file: mysql_connector.go
package dbconnector

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

type MySQLConnector struct {
	baseConnector
}

func newMySQLConnector(cfg ConnectionConfig) (*MySQLConnector, error) {
	if cfg.Port == 0 {
		cfg.Port = 3306
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	sslMode := strings.ToLower(strings.TrimSpace(cfg.SSLMode))
	if sslMode == "disable" {
		dsn += "&tls=false"
	} else if sslMode != "" {
		dsn += "&tls=true"
	}
	db, err := openDatabase("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql connection: %w", err)
	}
	return &MySQLConnector{baseConnector{cfg: cfg, db: db}}, nil
}

func (c *MySQLConnector) TestConnection(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
	}
	return nil
}

func (c *MySQLConnector) ListTables(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'")
	if err != nil {
		return nil, fmt.Errorf("list mysql tables: %w", err)
	}
	defer rows.Close()
	results := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan mysql table name: %w", err)
		}
		results = append(results, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql tables: %w", err)
	}
	return results, nil
}

func (c *MySQLConnector) DescribeTable(ctx context.Context, table string) (*TableSchema, error) {
	_, _, err := quoteQualified(table, 1, func(s string) string { return "`" + s + "`" })
	if err != nil {
		return nil, fmt.Errorf("invalid mysql table: %w", err)
	}
	colsStmt, err := c.db.PrepareContext(ctx, "SELECT column_name, data_type, is_nullable, column_key FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position")
	if err != nil {
		return nil, fmt.Errorf("prepare mysql columns query: %w", err)
	}
	defer colsStmt.Close()
	rows, err := colsStmt.QueryContext(ctx, table)
	if err != nil {
		return nil, fmt.Errorf("query mysql columns: %w", err)
	}
	defer rows.Close()
	columns := []ColumnInfo{}
	for rows.Next() {
		var name, dataType, isNullable, key string
		if err := rows.Scan(&name, &dataType, &isNullable, &key); err != nil {
			return nil, fmt.Errorf("scan mysql column: %w", err)
		}
		columns = append(columns, ColumnInfo{
			Name:     name,
			Type:     dataType,
			Nullable: strings.EqualFold(isNullable, "YES"),
			IsPK:     strings.EqualFold(key, "PRI"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql columns: %w", err)
	}

	idxStmt, err := c.db.PrepareContext(ctx, "SELECT index_name, non_unique, column_name, seq_in_index FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? ORDER BY index_name, seq_in_index")
	if err != nil {
		return nil, fmt.Errorf("prepare mysql index query: %w", err)
	}
	defer idxStmt.Close()
	idxRows, err := idxStmt.QueryContext(ctx, table)
	if err != nil {
		return nil, fmt.Errorf("query mysql indexes: %w", err)
	}
	defer idxRows.Close()
	indexMap := map[string]*IndexInfo{}
	for idxRows.Next() {
		var name string
		var nonUnique int
		var column string
		var seq int
		if err := idxRows.Scan(&name, &nonUnique, &column, &seq); err != nil {
			return nil, fmt.Errorf("scan mysql index: %w", err)
		}
		idx, ok := indexMap[name]
		if !ok {
			idx = &IndexInfo{Name: name, Unique: nonUnique == 0, Columns: []string{}}
			indexMap[name] = idx
		}
		idx.Columns = append(idx.Columns, column)
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql indexes: %w", err)
	}
	indexes := []IndexInfo{}
	for _, idx := range indexMap {
		indexes = append(indexes, *idx)
	}
	indexes = sortIndexColumns(indexes)
	return &TableSchema{Columns: columns, Indexes: indexes}, nil
}

func (c *MySQLConnector) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return c.sampleRows(ctx, table, normalizeSampleLimit(limit), nil)
}

func (c *MySQLConnector) ProfileTable(ctx context.Context, table string, opts ProfileOptions) (*TableProfile, error) {
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

func (c *MySQLConnector) sampleRows(ctx context.Context, table string, limit int, columns []string) ([]map[string]any, error) {
	quotedTable, _, err := quoteQualified(table, 1, func(s string) string { return "`" + s + "`" })
	if err != nil {
		return nil, fmt.Errorf("invalid mysql table: %w", err)
	}
	selectClause := "*"
	if len(columns) > 0 {
		selectClause, err = quoteList(columns, func(s string) string { return "`" + s + "`" })
		if err != nil {
			return nil, fmt.Errorf("invalid mysql column list: %w", err)
		}
	}
	query := fmt.Sprintf("SELECT %s FROM %s LIMIT ?", selectClause, quotedTable)
	stmt, err := c.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("prepare mysql sample query: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("query mysql sample rows: %w", err)
	}
	defer rows.Close()
	result, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, fmt.Errorf("scan mysql sample rows: %w", err)
	}
	return result, nil
}

func (c *MySQLConnector) estimateRowCount(ctx context.Context, table string) (int64, error) {
	stmt, err := c.db.PrepareContext(ctx, "SELECT table_rows FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?")
	if err != nil {
		return 0, fmt.Errorf("prepare mysql row count query: %w", err)
	}
	defer stmt.Close()
	var count sql.NullInt64
	if err := stmt.QueryRowContext(ctx, table).Scan(&count); err != nil {
		return 0, fmt.Errorf("query mysql row count: %w", err)
	}
	if !count.Valid {
		return 0, nil
	}
	return count.Int64, nil
}

func (c *MySQLConnector) WriteRow(ctx context.Context, table, valueColumn, timestampColumn string, value any, extraColumns map[string]any) (int64, error) {
	quotedTable, _, err := quoteQualified(table, 1, func(s string) string { return "`" + s + "`" })
	if err != nil {
		return 0, fmt.Errorf("invalid mysql table: %w", err)
	}
	quotedVal, err := quoteList([]string{valueColumn}, func(s string) string { return "`" + s + "`" })
	if err != nil {
		return 0, fmt.Errorf("invalid mysql value column: %w", err)
	}
	quotedTs, err := quoteList([]string{timestampColumn}, func(s string) string { return "`" + s + "`" })
	if err != nil {
		return 0, fmt.Errorf("invalid mysql timestamp column: %w", err)
	}

	colParts := []string{quotedVal, quotedTs}
	placeholders := []string{"?", "NOW()"}
	args := []any{value}

	for k, v := range extraColumns {
		qk, qErr := quoteList([]string{k}, func(s string) string { return "`" + s + "`" })
		if qErr != nil {
			return 0, fmt.Errorf("invalid extra column %q: %w", k, qErr)
		}
		colParts = append(colParts, qk)
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotedTable,
		strings.Join(colParts, ", "),
		strings.Join(placeholders, ", "),
	)
	result, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mysql write row: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mysql rows affected: %w", err)
	}
	return n, nil
}
