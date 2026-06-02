package api

import (
	"encoding/json"
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
			r.Get("/{lineId}/wires", h.handleProductionLineWiresList)
			r.Put("/{lineId}/wires", h.handleProductionLineWiresReplace)
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

// wireRequest is used for PUT /production-lines/{lineId}/wires body.
type wireRequest struct {
	ID            string  `json:"id"`
	SourceUnitID  string  `json:"sourceId"`
	TargetUnitID  string  `json:"targetId"`
	SourceOffsetX float64 `json:"sourceOffsetX"`
	SourceOffsetY float64 `json:"sourceOffsetY"`
	TargetOffsetX float64 `json:"targetOffsetX"`
	TargetOffsetY float64 `json:"targetOffsetY"`
	Label         string  `json:"label"`
}

type wireResponse struct {
	ID            string  `json:"id"`
	SourceUnitID  string  `json:"sourceId"`
	TargetUnitID  string  `json:"targetId"`
	SourceOffsetX float64 `json:"sourceOffsetX"`
	SourceOffsetY float64 `json:"sourceOffsetY"`
	TargetOffsetX float64 `json:"targetOffsetX"`
	TargetOffsetY float64 `json:"targetOffsetY"`
	Label         string  `json:"label"`
}

func wireToResponse(w storage.CanvasWire) wireResponse {
	return wireResponse{
		ID:            w.ID,
		SourceUnitID:  w.SourceUnitID,
		TargetUnitID:  w.TargetUnitID,
		SourceOffsetX: w.SourceOffsetX,
		SourceOffsetY: w.SourceOffsetY,
		TargetOffsetX: w.TargetOffsetX,
		TargetOffsetY: w.TargetOffsetY,
		Label:         w.Label,
	}
}

// GET /production-lines/{lineId}/wires
func (h *Handler) handleProductionLineWiresList(w http.ResponseWriter, r *http.Request) {
	lineID := chi.URLParam(r, "lineId")
	if lineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "lineId required"})
		return
	}
	wires, err := h.Repo.ListWiresByLine(r.Context(), lineID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to list wires"})
		return
	}
	resp := make([]wireResponse, 0, len(wires))
	for _, wire := range wires {
		resp = append(resp, wireToResponse(wire))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "wires": resp})
}

// PUT /production-lines/{lineId}/wires — replaces all wires for this line
func (h *Handler) handleProductionLineWiresReplace(w http.ResponseWriter, r *http.Request) {
	lineID := chi.URLParam(r, "lineId")
	if lineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "lineId required"})
		return
	}

	var body struct {
		Wires []wireRequest `json:"wires"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid body"})
		return
	}

	storageWires := make([]storage.CanvasWire, 0, len(body.Wires))
	for _, req := range body.Wires {
		storageWires = append(storageWires, storage.CanvasWire{
			ID:            req.ID,
			ProductionLineID: lineID,
			SourceUnitID:  req.SourceUnitID,
			TargetUnitID:  req.TargetUnitID,
			SourceOffsetX: req.SourceOffsetX,
			SourceOffsetY: req.SourceOffsetY,
			TargetOffsetX: req.TargetOffsetX,
			TargetOffsetY: req.TargetOffsetY,
			Label:         req.Label,
		})
	}

	saved, err := h.Repo.ReplaceWiresForLine(r.Context(), lineID, storageWires)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to save wires"})
		return
	}

	resp := make([]wireResponse, 0, len(saved))
	for _, wire := range saved {
		resp = append(resp, wireToResponse(wire))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "wires": resp})
}
