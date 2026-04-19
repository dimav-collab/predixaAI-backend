package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	dbconnector "predixaai-backend"
	"predixaai-backend/cmd/service/internal/connections"
)

// --- mock connectors ---

type mockConnectorWithWriteErr struct {
	writeErr error
	affected int64
}

func (m *mockConnectorWithWriteErr) TestConnection(ctx context.Context) error { return nil }
func (m *mockConnectorWithWriteErr) ListTables(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockConnectorWithWriteErr) DescribeTable(ctx context.Context, table string) (*dbconnector.TableSchema, error) {
	return nil, nil
}
func (m *mockConnectorWithWriteErr) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return nil, nil
}
func (m *mockConnectorWithWriteErr) ProfileTable(ctx context.Context, table string, opts dbconnector.ProfileOptions) (*dbconnector.TableProfile, error) {
	return nil, nil
}
func (m *mockConnectorWithWriteErr) WriteRow(_ context.Context, _, _, _ string, _ any, _ map[string]any) (int64, error) {
	return m.affected, m.writeErr
}
func (m *mockConnectorWithWriteErr) Close() error { return nil }

type mockConnectorUnreachable struct{}

func (m *mockConnectorUnreachable) TestConnection(ctx context.Context) error {
	return errors.New("connection refused")
}
func (m *mockConnectorUnreachable) ListTables(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockConnectorUnreachable) DescribeTable(ctx context.Context, table string) (*dbconnector.TableSchema, error) {
	return nil, nil
}
func (m *mockConnectorUnreachable) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return nil, nil
}
func (m *mockConnectorUnreachable) ProfileTable(ctx context.Context, table string, opts dbconnector.ProfileOptions) (*dbconnector.TableProfile, error) {
	return nil, nil
}
func (m *mockConnectorUnreachable) WriteRow(_ context.Context, _, _, _ string, _ any, _ map[string]any) (int64, error) {
	return 0, errors.New("unreachable")
}
func (m *mockConnectorUnreachable) Close() error { return nil }

// captureConnector records arguments passed to WriteRow.
type captureConnector struct {
	capturedTable  string
	capturedValCol string
	capturedTsCol  string
	capturedValue  any
	capturedExtras map[string]any
	affected       int64
}

func (m *captureConnector) TestConnection(ctx context.Context) error { return nil }
func (m *captureConnector) ListTables(ctx context.Context) ([]string, error) { return nil, nil }
func (m *captureConnector) DescribeTable(ctx context.Context, table string) (*dbconnector.TableSchema, error) {
	return nil, nil
}
func (m *captureConnector) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return nil, nil
}
func (m *captureConnector) ProfileTable(ctx context.Context, table string, opts dbconnector.ProfileOptions) (*dbconnector.TableProfile, error) {
	return nil, nil
}
func (m *captureConnector) WriteRow(_ context.Context, table, valCol, tsCol string, value any, extras map[string]any) (int64, error) {
	m.capturedTable = table
	m.capturedValCol = valCol
	m.capturedTsCol = tsCol
	m.capturedValue = value
	m.capturedExtras = extras
	return m.affected, nil
}
func (m *captureConnector) Close() error { return nil }

// --- validation unit tests ---

func TestValidateSimulateWriteRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         simulateWriteRequest
		wantDetails []string
	}{
		{
			name: "valid minimal",
			req: simulateWriteRequest{
				ConnectionRef:   "some-uuid",
				TableName:       "etchers_data",
				ColumnName:      "rf_power",
				TimestampColumn: "run_order",
				Value:           42.5,
			},
		},
		{
			name: "valid with extraColumns",
			req: simulateWriteRequest{
				ConnectionRef:   "ref",
				TableName:       "etchers_data",
				ColumnName:      "rf_power",
				TimestampColumn: "run_order",
				Value:           30.0,
				ExtraColumns:    map[string]any{"unit": "SIM"},
			},
		},
		{
			name: "missing connectionRef",
			req: simulateWriteRequest{
				TableName:       "etchers_data",
				ColumnName:      "rf_power",
				TimestampColumn: "run_order",
			},
			wantDetails: []string{"connectionRef: required"},
		},
		{
			name: "tableName with spaces",
			req: simulateWriteRequest{
				ConnectionRef:   "ref",
				TableName:       "bad table",
				ColumnName:      "col",
				TimestampColumn: "ts",
			},
			wantDetails: []string{"tableName:"},
		},
		{
			name: "SQL injection in tableName",
			req: simulateWriteRequest{
				ConnectionRef:   "ref",
				TableName:       "t; DROP TABLE users;--",
				ColumnName:      "col",
				TimestampColumn: "ts",
			},
			wantDetails: []string{"tableName:"},
		},
		{
			name: "columnName starts with digit",
			req: simulateWriteRequest{
				ConnectionRef:   "ref",
				TableName:       "tbl",
				ColumnName:      "1bad",
				TimestampColumn: "ts",
			},
			wantDetails: []string{"columnName:"},
		},
		{
			name: "empty timestampColumn",
			req: simulateWriteRequest{
				ConnectionRef: "ref",
				TableName:     "tbl",
				ColumnName:    "col",
			},
			wantDetails: []string{"timestampColumn:"},
		},
		{
			name: "invalid extraColumns key",
			req: simulateWriteRequest{
				ConnectionRef:   "ref",
				TableName:       "tbl",
				ColumnName:      "col",
				TimestampColumn: "ts",
				ExtraColumns:    map[string]any{"bad-key!": "v"},
			},
			wantDetails: []string{"extraColumns key"},
		},
		{
			name: "multiple errors returned together",
			req: simulateWriteRequest{
				ConnectionRef:   "",
				TableName:       "bad;name",
				ColumnName:      "col",
				TimestampColumn: "ts",
			},
			wantDetails: []string{"connectionRef:", "tableName:"},
		},
		{
			name: "value zero is accepted",
			req: simulateWriteRequest{
				ConnectionRef:   "ref",
				TableName:       "tbl",
				ColumnName:      "col",
				TimestampColumn: "ts",
				Value:           0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := validateSimulateWriteRequest(tt.req)
			if len(tt.wantDetails) == 0 && len(details) > 0 {
				t.Fatalf("expected no errors, got: %v", details)
			}
			for _, want := range tt.wantDetails {
				found := false
				for _, d := range details {
					if len(d) >= len(want) && d[:len(want)] == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected a detail starting with %q, got: %v", want, details)
				}
			}
		})
	}
}

// --- handler tests ---

func TestHandleSimulateWrite(t *testing.T) {
	validPayload := map[string]any{
		"connectionRef":   "conn-ref-123",
		"tableName":       "etchers_data",
		"columnName":      "rf_power",
		"timestampColumn": "run_order",
		"value":           99.5,
	}

	tests := []struct {
		name           string
		method         string
		payload        any
		resolverErr    error
		connector      dbconnector.DbConnector
		expectedStatus int
		expectedOk     *bool
		expectedRows   *int64
		expectedErrKey string
	}{
		{
			name:           "happy path",
			method:         http.MethodPost,
			payload:        validPayload,
			connector:      &mockConnectorWithWriteErr{affected: 1},
			expectedStatus: http.StatusOK,
			expectedOk:     boolPtr(true),
			expectedRows:   int64Ptr(1),
		},
		{
			name:   "happy path with extraColumns",
			method: http.MethodPost,
			payload: map[string]any{
				"connectionRef": "conn-ref-123", "tableName": "etchers_data",
				"columnName": "rf_power", "timestampColumn": "run_order",
				"value": 30.0, "extraColumns": map[string]any{"unit": "SIM"},
			},
			connector:      &mockConnectorWithWriteErr{affected: 1},
			expectedStatus: http.StatusOK,
			expectedOk:     boolPtr(true),
			expectedRows:   int64Ptr(1),
		},
		{
			name:           "wrong HTTP method → 405",
			method:         http.MethodGet,
			payload:        validPayload,
			connector:      &mockConnectorWithWriteErr{},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedErrKey: "method_not_allowed",
		},
		{
			name:           "missing connectionRef → 400 validation_error",
			method:         http.MethodPost,
			payload:        map[string]any{"tableName": "t", "columnName": "c", "timestampColumn": "ts"},
			connector:      &mockConnectorWithWriteErr{},
			expectedStatus: http.StatusBadRequest,
			expectedErrKey: "validation_error",
		},
		{
			name:           "invalid tableName → 400 validation_error",
			method:         http.MethodPost,
			payload:        map[string]any{"connectionRef": "ref", "tableName": "bad;name", "columnName": "c", "timestampColumn": "ts"},
			connector:      &mockConnectorWithWriteErr{},
			expectedStatus: http.StatusBadRequest,
			expectedErrKey: "validation_error",
		},
		{
			name:   "invalid extraColumns key → 400 validation_error",
			method: http.MethodPost,
			payload: map[string]any{
				"connectionRef": "ref", "tableName": "tbl",
				"columnName": "c", "timestampColumn": "ts",
				"extraColumns": map[string]any{"bad-key!": "v"},
			},
			connector:      &mockConnectorWithWriteErr{},
			expectedStatus: http.StatusBadRequest,
			expectedErrKey: "validation_error",
		},
		{
			name:           "connection not found → 404 connection_not_found",
			method:         http.MethodPost,
			payload:        validPayload,
			resolverErr:    connections.ErrNotFound,
			connector:      &mockConnectorWithWriteErr{},
			expectedStatus: http.StatusNotFound,
			expectedErrKey: "connection_not_found",
		},
		{
			name:           "resolver not configured → 400 connection_not_configured",
			method:         http.MethodPost,
			payload:        validPayload,
			resolverErr:    connections.ErrNotConfigured,
			connector:      &mockConnectorWithWriteErr{},
			expectedStatus: http.StatusBadRequest,
			expectedErrKey: "connection_not_configured",
		},
		{
			name:           "target DB unreachable → 502 db_unreachable",
			method:         http.MethodPost,
			payload:        validPayload,
			connector:      &mockConnectorUnreachable{},
			expectedStatus: http.StatusBadGateway,
			expectedErrKey: "db_unreachable",
		},
		{
			name:           "write fails at DB → 502 db_write_failed",
			method:         http.MethodPost,
			payload:        validPayload,
			connector:      &mockConnectorWithWriteErr{writeErr: errors.New("column not found")},
			expectedStatus: http.StatusBadGateway,
			expectedErrKey: "db_write_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockResolver{
				cfg: dbconnector.ConnectionConfig{Type: "mysql", Host: "localhost"},
				err: tt.resolverErr,
			}
			h := NewHandler(resolver, func(cfg dbconnector.ConnectionConfig) (dbconnector.DbConnector, error) {
				return tt.connector, nil
			})

			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest(tt.method, "/simulate/write", bytes.NewReader(body))
			resp := httptest.NewRecorder()
			h.HandleSimulateWrite(resp, req)

			if resp.Code != tt.expectedStatus {
				t.Fatalf("expected status %d, got %d (body: %s)", tt.expectedStatus, resp.Code, resp.Body.String())
			}

			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if tt.expectedOk != nil {
				if ok, _ := result["ok"].(bool); ok != *tt.expectedOk {
					t.Fatalf("expected ok=%v got %v", *tt.expectedOk, result)
				}
			}
			if tt.expectedRows != nil {
				rows, _ := result["rowsAffected"].(float64)
				if int64(rows) != *tt.expectedRows {
					t.Fatalf("expected rowsAffected=%d got %v", *tt.expectedRows, rows)
				}
			}
			if tt.expectedErrKey != "" {
				if errVal, _ := result["error"].(string); errVal != tt.expectedErrKey {
					t.Fatalf("expected error=%q got %q", tt.expectedErrKey, errVal)
				}
			}
		})
	}
}

// TestHandleSimulateWritePassesExtraColumns verifies extraColumns reach the connector.
func TestHandleSimulateWritePassesExtraColumns(t *testing.T) {
	cap := &captureConnector{affected: 1}
	resolver := &mockResolver{cfg: dbconnector.ConnectionConfig{Type: "postgres", Host: "db"}}
	h := NewHandler(resolver, func(_ dbconnector.ConnectionConfig) (dbconnector.DbConnector, error) {
		return cap, nil
	})

	body, _ := json.Marshal(map[string]any{
		"connectionRef": "ref", "tableName": "etchers_data",
		"columnName": "rf_power", "timestampColumn": "run_order",
		"value": 30.0, "extraColumns": map[string]any{"unit": "SIM", "tool_log": "auto"},
	})
	req := httptest.NewRequest(http.MethodPost, "/simulate/write", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	h.HandleSimulateWrite(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if cap.capturedTable != "etchers_data" {
		t.Errorf("table: want etchers_data, got %q", cap.capturedTable)
	}
	if cap.capturedExtras["unit"] != "SIM" {
		t.Errorf("extras unit: want SIM, got %v", cap.capturedExtras["unit"])
	}
	if cap.capturedExtras["tool_log"] != "auto" {
		t.Errorf("extras tool_log: want auto, got %v", cap.capturedExtras["tool_log"])
	}
}

// helpers
func boolPtr(b bool) *bool    { return &b }
func int64Ptr(n int64) *int64 { return &n }
