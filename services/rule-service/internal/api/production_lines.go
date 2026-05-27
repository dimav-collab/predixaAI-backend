package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"predixaai-backend/services/rule-service/internal/rules"
	"predixaai-backend/services/rule-service/internal/storage"
)

type productionLineRequest struct {
	LineName string `json:"lineName"`
}

type productionLineResponse struct {
	LineID    string `json:"lineId"`
	LineName  string `json:"lineName"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func buildProductionLineResponse(pl storage.ProductionLine) productionLineResponse {
	return productionLineResponse{
		LineID:    pl.LineID,
		LineName:  pl.LineName,
		CreatedAt: pl.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: pl.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// RegisterProductionLineRoutes registers all /production-lines routes.
func (h *Handler) RegisterProductionLineRoutes(r chi.Router) {
	r.Route("/production-lines", func(r chi.Router) {
		r.Post("/", h.handleProductionLineCreate)
		r.Get("/", h.handleProductionLineList)
		r.Get("/{lineId}", h.handleProductionLineGet)
		r.Put("/{lineId}", h.handleProductionLineUpdate)
		r.Delete("/{lineId}", h.handleProductionLineDelete)
		r.Get("/{lineId}/machine-units", h.handleProductionLineMachineUnits)
	})
}

func (h *Handler) handleProductionLineCreate(w http.ResponseWriter, r *http.Request) {
	var req productionLineRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	name := strings.TrimSpace(req.LineName)
	if name == "" {
		writeValidationError(w, "VALIDATION_ERROR", "lineName is required",
			[]rules.ErrorDetail{{Field: "lineName", Problem: "missing", Hint: "Provide lineName"}})
		return
	}
	pl, err := h.Repo.CreateProductionLine(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to create production line"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "productionLine": buildProductionLineResponse(pl)})
}

func (h *Handler) handleProductionLineList(w http.ResponseWriter, r *http.Request) {
	lines, err := h.Repo.ListProductionLines(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to list production lines"})
		return
	}
	responses := make([]productionLineResponse, 0, len(lines))
	for _, pl := range lines {
		responses = append(responses, buildProductionLineResponse(pl))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "productionLines": responses})
}

func (h *Handler) handleProductionLineGet(w http.ResponseWriter, r *http.Request) {
	lineID := chi.URLParam(r, "lineId")
	pl, err := h.Repo.GetProductionLine(r.Context(), lineID)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "production line not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch production line"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "productionLine": buildProductionLineResponse(pl)})
}

func (h *Handler) handleProductionLineUpdate(w http.ResponseWriter, r *http.Request) {
	lineID := chi.URLParam(r, "lineId")
	var req productionLineRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	name := strings.TrimSpace(req.LineName)
	if name == "" {
		writeValidationError(w, "VALIDATION_ERROR", "lineName is required",
			[]rules.ErrorDetail{{Field: "lineName", Problem: "missing", Hint: "Provide lineName"}})
		return
	}
	pl, err := h.Repo.UpdateProductionLine(r.Context(), lineID, name)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "production line not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update production line"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "productionLine": buildProductionLineResponse(pl)})
}

func (h *Handler) handleProductionLineDelete(w http.ResponseWriter, r *http.Request) {
	lineID := chi.URLParam(r, "lineId")
	if err := h.Repo.DeleteProductionLine(r.Context(), lineID); err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "production line not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to delete production line"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleProductionLineMachineUnits(w http.ResponseWriter, r *http.Request) {
	lineID := chi.URLParam(r, "lineId")
	// Verify the line exists first
	if _, err := h.Repo.GetProductionLine(r.Context(), lineID); err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "production line not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch production line"})
		return
	}
	units, err := h.Repo.ListMachineUnitsByLine(r.Context(), lineID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to list machine units"})
		return
	}
	responses := make([]machineUnitResponse, 0, len(units))
	for _, unit := range units {
		responses = append(responses, buildMachineUnitResponse(unit))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "units": responses})
}
