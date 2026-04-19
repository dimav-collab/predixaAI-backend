// file: postgres_connector.go
package dbconnector

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

type PostgresConnector struct {
	baseConnector
}

func newPostgresConnector(cfg ConnectionConfig) (*PostgresConnector, error) {
	if cfg.Port == 0 {
		cfg.Port = 5432
	}
	sslMode := strings.ToLower(strings.TrimSpace(cfg.SSLMode))
	if sslMode == "" {
		sslMode = "disable"
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, sslMode)
	db, err := openDatabase("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	return &PostgresConnector{baseConnector{cfg: cfg, db: db}}, nil
}

func (c *PostgresConnector) TestConnection(ctx context.Context) error {
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func (c *PostgresConnector) ListTables(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'")
	if err != nil {
		return nil, fmt.Errorf("list postgres tables: %w", err)
	}
	defer rows.Close()
	results := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan postgres table name: %w", err)
		}
		results = append(results, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres tables: %w", err)
	}
	return results, nil
}

func (c *PostgresConnector) DescribeTable(ctx context.Context, table string) (*TableSchema, error) {
	_, parts, err := quoteQualified(table, 2, func(s string) string { return "\"" + s + "\"" })
	if err != nil {
		return nil, fmt.Errorf("invalid postgres table: %w", err)
	}
	name := parts[len(parts)-1]
	colsStmt, err := c.db.PrepareContext(ctx, "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 ORDER BY ordinal_position")
	if err != nil {
		return nil, fmt.Errorf("prepare postgres columns query: %w", err)
	}
	defer colsStmt.Close()
	rows, err := colsStmt.QueryContext(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("query postgres columns: %w", err)
	}
	defer rows.Close()
	columns := []ColumnInfo{}
	for rows.Next() {
		var colName, dataType, isNullable string
		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("scan postgres column: %w", err)
		}
		columns = append(columns, ColumnInfo{
			Name:     colName,
			Type:     dataType,
			Nullable: strings.EqualFold(isNullable, "YES"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres columns: %w", err)
	}

	pkStmt, err := c.db.PrepareContext(ctx, "SELECT a.attname FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey) WHERE i.indrelid = $1::regclass AND i.indisprimary")
	if err != nil {
		return nil, fmt.Errorf("prepare postgres pk query: %w", err)
	}
	defer pkStmt.Close()
	pkRows, err := pkStmt.QueryContext(ctx, strings.Join(parts, "."))
	if err != nil {
		return nil, fmt.Errorf("query postgres pk columns: %w", err)
	}
	defer pkRows.Close()
	pkSet := map[string]struct{}{}
	for pkRows.Next() {
		var name string
		if err := pkRows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan postgres pk column: %w", err)
		}
		pkSet[name] = struct{}{}
	}
	if err := pkRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres pk columns: %w", err)
	}
	for i, col := range columns {
		if _, ok := pkSet[col.Name]; ok {
			columns[i].IsPK = true
		}
	}

	idxStmt, err := c.db.PrepareContext(ctx, "SELECT i.relname, ix.indisunique, array_agg(a.attname ORDER BY x.n) FROM pg_class t JOIN pg_namespace n ON n.oid = t.relnamespace JOIN pg_index ix ON t.oid = ix.indrelid JOIN pg_class i ON i.oid = ix.indexrelid JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS x(attnum, n) ON true JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = x.attnum WHERE n.nspname = current_schema() AND t.relname = $1 GROUP BY i.relname, ix.indisunique ORDER BY i.relname")
	if err != nil {
		return nil, fmt.Errorf("prepare postgres index query: %w", err)
	}
	defer idxStmt.Close()
	idxRows, err := idxStmt.QueryContext(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("query postgres indexes: %w", err)
	}
	defer idxRows.Close()
	indexes := []IndexInfo{}
	for idxRows.Next() {
		var idxName string
		var unique bool
		var cols []string
		if err := idxRows.Scan(&idxName, &unique, &cols); err != nil {
			return nil, fmt.Errorf("scan postgres index: %w", err)
		}
		indexes = append(indexes, IndexInfo{Name: idxName, Unique: unique, Columns: ensureSlice(cols)})
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres indexes: %w", err)
	}
	indexes = sortIndexColumns(indexes)
	return &TableSchema{Columns: columns, Indexes: indexes}, nil
}

func (c *PostgresConnector) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return c.sampleRows(ctx, table, normalizeSampleLimit(limit), nil)
}

func (c *PostgresConnector) ProfileTable(ctx context.Context, table string, opts ProfileOptions) (*TableProfile, error) {
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

func (c *PostgresConnector) sampleRows(ctx context.Context, table string, limit int, columns []string) ([]map[string]any, error) {
	quotedTable, _, err := quoteQualified(table, 2, func(s string) string { return "\"" + s + "\"" })
	if err != nil {
		return nil, fmt.Errorf("invalid postgres table: %w", err)
	}
	selectClause := "*"
	if len(columns) > 0 {
		selectClause, err = quoteList(columns, func(s string) string { return "\"" + s + "\"" })
		if err != nil {
			return nil, fmt.Errorf("invalid postgres column list: %w", err)
		}
	}
	query := fmt.Sprintf("SELECT %s FROM %s LIMIT $1", selectClause, quotedTable)
	stmt, err := c.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("prepare postgres sample query: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("query postgres sample rows: %w", err)
	}
	defer rows.Close()
	result, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, fmt.Errorf("scan postgres sample rows: %w", err)
	}
	return result, nil
}

func (c *PostgresConnector) estimateRowCount(ctx context.Context, table string) (int64, error) {
	_, parts, err := quoteQualified(table, 2, func(s string) string { return "\"" + s + "\"" })
	if err != nil {
		return 0, fmt.Errorf("invalid postgres table: %w", err)
	}
	name := parts[len(parts)-1]
	stmt, err := c.db.PrepareContext(ctx, "SELECT reltuples::bigint FROM pg_class WHERE relname = $1 AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = current_schema())")
	if err != nil {
		return 0, fmt.Errorf("prepare postgres row count query: %w", err)
	}
	defer stmt.Close()
	var count sql.NullInt64
	if err := stmt.QueryRowContext(ctx, name).Scan(&count); err != nil {
		return 0, fmt.Errorf("query postgres row count: %w", err)
	}
	if !count.Valid {
		return 0, nil
	}
	return count.Int64, nil
}

func (c *PostgresConnector) WriteRow(ctx context.Context, table, valueColumn, timestampColumn string, value any, extraColumns map[string]any) (int64, error) {
	quotedTable, _, err := quoteQualified(table, 2, func(s string) string { return "\"" + s + "\"" })
	if err != nil {
		return 0, fmt.Errorf("invalid postgres table: %w", err)
	}
	quotedVal, err := quoteList([]string{valueColumn}, func(s string) string { return "\"" + s + "\"" })
	if err != nil {
		return 0, fmt.Errorf("invalid postgres value column: %w", err)
	}
	quotedTs, err := quoteList([]string{timestampColumn}, func(s string) string { return "\"" + s + "\"" })
	if err != nil {
		return 0, fmt.Errorf("invalid postgres timestamp column: %w", err)
	}

	colParts := []string{quotedVal, quotedTs}
	placeholders := []string{"$1", "NOW()"}
	args := []any{value}
	idx := 2

	for k, v := range extraColumns {
		qk, qErr := quoteList([]string{k}, func(s string) string { return "\"" + s + "\"" })
		if qErr != nil {
			return 0, fmt.Errorf("invalid extra column %q: %w", k, qErr)
		}
		colParts = append(colParts, qk)
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx))
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
		return 0, fmt.Errorf("postgres write row: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("postgres rows affected: %w", err)
	}
	return n, nil
}
