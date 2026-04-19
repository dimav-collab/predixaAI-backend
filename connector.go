// file: connector.go
package dbconnector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxColumns  = 25
	defaultSampleLimit = 50
	maxSamplePreview   = 5
)

type DbConnector interface {
	TestConnection(ctx context.Context) error

	ListTables(ctx context.Context) ([]string, error)

	DescribeTable(ctx context.Context, table string) (*TableSchema, error)

	SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error)

	ProfileTable(ctx context.Context, table string, opts ProfileOptions) (*TableProfile, error)

	// WriteRow inserts a single row into table, setting valueColumn to value,
	// timestampColumn to the database's current timestamp, and any extra columns
	// provided in extraColumns. Returns the number of rows affected (always 1 on success).
	WriteRow(ctx context.Context, table, valueColumn, timestampColumn string, value any, extraColumns map[string]any) (int64, error)

	Close() error
}

type ConnectionConfig struct {
	Type     string // mysql | postgres | mssql
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

type ProfileOptions struct {
	MaxColumns  int
	SampleLimit int
}

type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	IsPK     bool
}

type IndexInfo struct {
	Name    string
	Columns []string
	Unique  bool
}

type TableSchema struct {
	Columns []ColumnInfo
	Indexes []IndexInfo
}

type ColumnProfile struct {
	Column           string
	Type             string
	Nullable         bool
	IsPK             bool
	SampleCount      int
	Nulls            int
	NullRate         float64
	DistinctInSample int
	Min              any
	Max              any
	Examples         []string
}

type TableProfile struct {
	Table         string
	RowCount      int64
	Schema        TableSchema
	Profiling     []ColumnProfile
	SamplePreview []map[string]any
}

type baseConnector struct {
	cfg ConnectionConfig
	db  *sql.DB
}

func (b *baseConnector) Close() error {
	if b.db == nil {
		return nil
	}
	return b.db.Close()
}

var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

func splitIdentifier(ident string) ([]string, error) {
	trimmed := strings.TrimSpace(ident)
	if trimmed == "" {
		return nil, errors.New("identifier is empty")
	}
	parts := strings.Split(trimmed, ".")
	for _, part := range parts {
		if part == "" {
			return nil, errors.New("identifier contains empty segment")
		}
		if !identPattern.MatchString(part) {
			return nil, fmt.Errorf("identifier segment %q is invalid", part)
		}
	}
	return parts, nil
}

func quoteQualified(ident string, maxSegments int, quote func(string) string) (string, []string, error) {
	parts, err := splitIdentifier(ident)
	if err != nil {
		return "", nil, err
	}
	if maxSegments > 0 && len(parts) > maxSegments {
		return "", nil, fmt.Errorf("identifier %q has too many segments", ident)
	}
	quoted := make([]string, len(parts))
	for i, part := range parts {
		quoted[i] = quote(part)
	}
	return strings.Join(quoted, "."), parts, nil
}

func quoteList(names []string, quote func(string) string) (string, error) {
	if len(names) == 0 {
		return "", errors.New("no columns provided")
	}
	quoted := make([]string, len(names))
	for i, name := range names {
		if name == "" {
			return "", errors.New("column name is empty")
		}
		parts, err := splitIdentifier(name)
		if err != nil || len(parts) != 1 {
			return "", fmt.Errorf("invalid column name %q", name)
		}
		quoted[i] = quote(name)
	}
	return strings.Join(quoted, ", "), nil
}

func normalizeProfileOptions(opts ProfileOptions) ProfileOptions {
	if opts.MaxColumns <= 0 {
		opts.MaxColumns = defaultMaxColumns
	}
	if opts.SampleLimit <= 0 {
		opts.SampleLimit = defaultSampleLimit
	}
	return opts
}

func normalizeSampleLimit(limit int) int {
	if limit <= 0 {
		return defaultSampleLimit
	}
	return limit
}

func scanRowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(cols))
		for i := range values {
			var v any
			values[i] = &v
		}
		if err := rows.Scan(values...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := *(values[i].(*any))
			row[col] = normalizeValue(v)
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	default:
		return t
	}
}

func profileFromSample(schema TableSchema, sample []map[string]any, maxColumns int) []ColumnProfile {
	profiles := make([]ColumnProfile, 0)
	columns := schema.Columns
	if maxColumns > 0 && len(columns) > maxColumns {
		columns = columns[:maxColumns]
	}
	sampleCount := len(sample)
	for _, col := range columns {
		profile := ColumnProfile{
			Column:      col.Name,
			Type:        col.Type,
			Nullable:    col.Nullable,
			IsPK:        col.IsPK,
			SampleCount: sampleCount,
			Examples:    []string{},
		}
		nulls := 0
		distinct := map[string]struct{}{}
		examples := map[string]struct{}{}
		var minVal any
		var maxVal any
		for _, row := range sample {
			v, ok := row[col.Name]
			if !ok || v == nil {
				nulls++
				continue
			}
			text := fmt.Sprint(v)
			distinct[text] = struct{}{}
			if len(profile.Examples) < 3 {
				if _, seen := examples[text]; !seen {
					profile.Examples = append(profile.Examples, text)
					examples[text] = struct{}{}
				}
			}
			minVal, maxVal = updateMinMax(minVal, maxVal, v)
		}
		profile.Nulls = nulls
		if sampleCount > 0 {
			profile.NullRate = float64(nulls) / float64(sampleCount)
		}
		profile.DistinctInSample = len(distinct)
		profile.Min = minVal
		profile.Max = maxVal
		profiles = append(profiles, profile)
	}
	return profiles
}

func updateMinMax(currentMin any, currentMax any, v any) (any, any) {
	if v == nil {
		return currentMin, currentMax
	}
	if t, ok := toTime(v); ok {
		if currentMin == nil || t.Before(currentMin.(time.Time)) {
			currentMin = t
		}
		if currentMax == nil || t.After(currentMax.(time.Time)) {
			currentMax = t
		}
		return currentMin, currentMax
	}
	if f, ok := toFloat(v); ok {
		if currentMin == nil || f < currentMin.(float64) {
			currentMin = f
		}
		if currentMax == nil || f > currentMax.(float64) {
			currentMax = f
		}
		return currentMin, currentMax
	}
	return currentMin, currentMax
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case float32:
		return float64(t), true
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	case []byte:
		f, err := strconv.ParseFloat(string(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		return parseTime(t)
	case []byte:
		return parseTime(string(t))
	default:
		return time.Time{}, false
	}
}

func parseTime(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func ensureSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func sortIndexColumns(indexes []IndexInfo) []IndexInfo {
	for i := range indexes {
		indexes[i].Columns = ensureSlice(indexes[i].Columns)
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		return indexes[i].Name < indexes[j].Name
	})
	return indexes
}
