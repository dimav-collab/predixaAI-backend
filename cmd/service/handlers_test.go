package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbconnector "predixaai-backend"
	"predixaai-backend/cmd/service/internal/connections"
)

type mockResolver struct {
	cfg     dbconnector.ConnectionConfig
	err     error
	called  bool
	lastRef string
}

func (m *mockResolver) ResolveByRef(ctx context.Context, connectionRef string) (dbconnector.ConnectionConfig, error) {
	m.called = true
	m.lastRef = connectionRef
	return m.cfg, m.err
}

type mockConnector struct {
	listCalled       bool
	describeCalled   bool
	writeRowCalled   bool
	writeRowErr      error
	writeRowAffected int64
}

func (m *mockConnector) TestConnection(ctx context.Context) error { return nil }
func (m *mockConnector) ListTables(ctx context.Context) ([]string, error) {
	m.listCalled = true
	return []string{"t1"}, nil
}
func (m *mockConnector) DescribeTable(ctx context.Context, table string) (*dbconnector.TableSchema, error) {
	m.describeCalled = true
	return &dbconnector.TableSchema{}, nil
}
func (m *mockConnector) SampleRows(ctx context.Context, table string, limit int) ([]map[string]any, error) {
	return nil, nil
}
func (m *mockConnector) ProfileTable(ctx context.Context, table string, opts dbconnector.ProfileOptions) (*dbconnector.TableProfile, error) {
	return &dbconnector.TableProfile{}, nil
}
func (m *mockConnector) WriteRow(_ context.Context, _, _, _ string, _ any, _ map[string]any) (int64, error) {
	m.writeRowCalled = true
	return m.writeRowAffected, m.writeRowErr
}
func (m *mockConnector) Close() error { return nil }

func TestTablesStrictConnectionRef(t *testing.T) {
	tests := []struct {
		name           string
		payload        map[string]any
		resolverErr    error
		expectedStatus int
		expectCalled   bool
	}{
		{
			name:           "missing connectionRef",
			payload:        map[string]any{},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "inline connection rejected",
			payload: map[string]any{
				"connectionRef": "ref",
				"connection":    map[string]any{"type": "postgres"},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "valid connectionRef",
			payload: map[string]any{
				"connectionRef": "ref",
			},
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockResolver{cfg: dbconnector.ConnectionConfig{Type: "postgres", Host: "db"}, err: tt.resolverErr}
			connector := &mockConnector{}
			h := NewHandler(resolver, func(cfg dbconnector.ConnectionConfig) (dbconnector.DbConnector, error) {
				return connector, nil
			})

			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest(http.MethodPost, "/tables", bytes.NewReader(body))
			resp := httptest.NewRecorder()
			h.HandleListTables(resp, req)

			if resp.Code != tt.expectedStatus {
				t.Fatalf("expected %d, got %d", tt.expectedStatus, resp.Code)
			}
			if tt.expectCalled && !resolver.called {
				t.Fatalf("expected resolver to be called")
			}
		})
	}
}

func TestDescribeStrictConnectionRef(t *testing.T) {
	tests := []struct {
		name           string
		payload        map[string]any
		resolverErr    error
		expectedStatus int
		expectCalled   bool
	}{
		{
			name:           "missing connectionRef",
			payload:        map[string]any{"table": "users"},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "inline connection rejected",
			payload: map[string]any{
				"connectionRef": "ref",
				"connection":    map[string]any{"type": "postgres"},
				"table":         "users",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "valid connectionRef",
			payload: map[string]any{
				"connectionRef": "ref",
				"table":         "users",
			},
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockResolver{cfg: dbconnector.ConnectionConfig{Type: "postgres", Host: "db"}, err: tt.resolverErr}
			connector := &mockConnector{}
			h := NewHandler(resolver, func(cfg dbconnector.ConnectionConfig) (dbconnector.DbConnector, error) {
				return connector, nil
			})

			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest(http.MethodPost, "/describe", bytes.NewReader(body))
			resp := httptest.NewRecorder()
			h.HandleDescribeTable(resp, req)

			if resp.Code != tt.expectedStatus {
				t.Fatalf("expected %d, got %d", tt.expectedStatus, resp.Code)
			}
			if tt.expectCalled && !resolver.called {
				t.Fatalf("expected resolver to be called")
			}
		})
	}
}

func TestResolverErrorsMapped(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{"not configured", connections.ErrNotConfigured, http.StatusBadRequest},
		{"not found", connections.ErrNotFound, http.StatusNotFound},
		{"invalid", connections.ErrInvalidInput, http.StatusBadRequest},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockResolver{err: tt.err}
			h := NewHandler(resolver, func(cfg dbconnector.ConnectionConfig) (dbconnector.DbConnector, error) {
				return &mockConnector{}, nil
			})

			payload := map[string]any{"connectionRef": "ref"}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/tables", bytes.NewReader(body))
			resp := httptest.NewRecorder()
			h.HandleListTables(resp, req)

			if resp.Code != tt.expectedStatus {
				t.Fatalf("expected %d, got %d", tt.expectedStatus, resp.Code)
			}
		})
	}
}
