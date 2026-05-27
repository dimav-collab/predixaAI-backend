package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"predixaai-backend/services/rule-service/internal/storage"
)

func setupProductionLineFixture(t *testing.T) (*storage.Repository, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL not set")
	}
	store, err := storage.NewStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	if err := store.RunMigrations(context.Background()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := storage.NewRepository(store)
	return repo, func() { store.Close() }
}

func newProductionLineRouter(repo *storage.Repository) http.Handler {
	h := &Handler{Repo: repo}
	r := chi.NewRouter()
	h.RegisterProductionLineRoutes(r)
	return r
}

// TestCreateProductionLine verifies a production line can be created and returned.
func TestCreateProductionLine(t *testing.T) {
	repo, cleanup := setupProductionLineFixture(t)
	defer cleanup()
	router := newProductionLineRouter(repo)

	body, _ := json.Marshal(map[string]string{"lineName": "Line Alpha"})
	req := httptest.NewRequest(http.MethodPost, "/production-lines", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pl := resp["productionLine"].(map[string]any)
	if pl["lineName"] != "Line Alpha" {
		t.Errorf("expected lineName=Line Alpha, got %v", pl["lineName"])
	}
	if pl["lineId"] == "" {
		t.Error("expected non-empty lineId")
	}
}

// TestCreateProductionLineMissingName verifies 400 when lineName is empty.
func TestCreateProductionLineMissingName(t *testing.T) {
	repo, cleanup := setupProductionLineFixture(t)
	defer cleanup()
	router := newProductionLineRouter(repo)

	body, _ := json.Marshal(map[string]string{"lineName": ""})
	req := httptest.NewRequest(http.MethodPost, "/production-lines", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestListProductionLines verifies the list endpoint returns created lines.
func TestListProductionLines(t *testing.T) {
	repo, cleanup := setupProductionLineFixture(t)
	defer cleanup()

	// Create a line directly
	pl, err := repo.CreateProductionLine(context.Background(), "Test List Line")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer repo.DeleteProductionLine(context.Background(), pl.LineID) //nolint

	router := newProductionLineRouter(repo)
	req := httptest.NewRequest(http.MethodGet, "/production-lines", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp) //nolint
	lines := resp["productionLines"].([]any)
	if len(lines) == 0 {
		t.Error("expected at least one production line")
	}
}

// TestUpdateProductionLine verifies renaming works.
func TestUpdateProductionLine(t *testing.T) {
	repo, cleanup := setupProductionLineFixture(t)
	defer cleanup()
	router := newProductionLineRouter(repo)

	pl, err := repo.CreateProductionLine(context.Background(), "Old Name")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer repo.DeleteProductionLine(context.Background(), pl.LineID) //nolint

	body, _ := json.Marshal(map[string]string{"lineName": "New Name"})
	req := httptest.NewRequest(http.MethodPut, "/production-lines/"+pl.LineID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp) //nolint
	updated := resp["productionLine"].(map[string]any)
	if updated["lineName"] != "New Name" {
		t.Errorf("expected New Name, got %v", updated["lineName"])
	}
}

// TestDeleteProductionLine verifies deletion returns 200, subsequent GET returns 404.
func TestDeleteProductionLine(t *testing.T) {
	repo, cleanup := setupProductionLineFixture(t)
	defer cleanup()
	router := newProductionLineRouter(repo)

	pl, err := repo.CreateProductionLine(context.Background(), "To Delete")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Delete
	req := httptest.NewRequest(http.MethodDelete, "/production-lines/"+pl.LineID, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Verify 404
	req2 := httptest.NewRequest(http.MethodGet, "/production-lines/"+pl.LineID, nil)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rr2.Code)
	}
}

// TestProductionLineMachineUnitsEndpoint verifies the /machine-units sub-resource.
func TestProductionLineMachineUnitsEndpoint(t *testing.T) {
	repo, cleanup := setupProductionLineFixture(t)
	defer cleanup()
	router := newProductionLineRouter(repo)

	pl, err := repo.CreateProductionLine(context.Background(), "Line For Units")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer repo.DeleteProductionLine(context.Background(), pl.LineID) //nolint

	req := httptest.NewRequest(http.MethodGet, "/production-lines/"+pl.LineID+"/machine-units", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp) //nolint
	units := resp["units"].([]any)
	if units == nil {
		t.Error("expected units array")
	}
}
