package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"predixaai-backend/services/rule-service/internal/storage"
)

// syncStepperRule converts the stepper rule into a RuleSpec, upserts it into the
// `rules` table (the scheduler's source of truth), then publishes a NATS event.
// Errors here are non-fatal for the HTTP response: we log them and move on.
func (h *Handler) syncStepperRule(ctx context.Context, rec storage.StepperRule, natsSubject string) {
	unit, err := h.Repo.GetMachineUnit(ctx, rec.UnitID)
	if err != nil {
		return // unit not found; scheduler cannot evaluate without a unit
	}
	spec, err := BuildRuleSpecFromStepper(rec, unit)
	if err != nil {
		return // unsupported rule type or bad config; leave scheduler untouched
	}
	ruleJSON, err := json.Marshal(spec)
	if err != nil {
		return
	}
	_ = h.Repo.UpsertStepperRuleToRules(ctx, storage.RuleRecord{
		ID:            rec.ID,
		Name:          rec.Name,
		Description:   "",
		ConnectionRef: unit.ConnectionRef,
		ParameterName: rec.ParameterID,
		RuleJSON:      ruleJSON,
		Enabled:       rec.Enabled,
		Status:        "DRAFT",
	})
	if h.Bus != nil {
		_ = h.Bus.Publish(natsSubject, map[string]any{"rule_id": rec.ID})
	}
}

func (h *Handler) handleStepperRuleCreate(w http.ResponseWriter, r *http.Request) {
	var req stepperRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if fieldErrors := validateStepperRuleRequest(req); len(fieldErrors) > 0 {
		writeStepperValidationError(w, "INVALID_REQUEST", "invalid rule request", fieldErrors)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	unit, err := h.Repo.GetMachineUnit(ctx, req.UnitID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
		return
	}
	if !parameterInUnit(unit.SelectedTable, unit.SelectedColumns, req.ParameterID) {
		writeStepperValidationError(w, "PARAMETER_NOT_FOUND", "parameterId not found", []FieldError{{Field: "parameterId", Problem: "not_found"}})
		return
	}
	rec, err := h.Repo.CreateStepperRule(ctx, toStepperRecord(req, true))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to create rule"})
		return
	}
	h.syncStepperRule(ctx, rec, "rule.created")
	writeJSON(w, http.StatusOK, toStepperResponse(rec))
}


func (h *Handler) handleStepperRuleGetByID(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	rec, err := h.Repo.GetStepperRule(ctx, ruleID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, toStepperResponse(rec))
}

func (h *Handler) handleStepperRuleUpdate(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")
	var req stepperRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if fieldErrors := validateStepperRuleRequest(req); len(fieldErrors) > 0 {
		writeStepperValidationError(w, "INVALID_REQUEST", "invalid rule request", fieldErrors)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	if _, err := h.Repo.GetStepperRule(ctx, ruleID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	unit, err := h.Repo.GetMachineUnit(ctx, req.UnitID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
		return
	}
	if !parameterInUnit(unit.SelectedTable, unit.SelectedColumns, req.ParameterID) {
		writeStepperValidationError(w, "PARAMETER_NOT_FOUND", "parameterId not found", []FieldError{{Field: "parameterId", Problem: "not_found"}})
		return
	}
	rec := toStepperRecord(req, true)
	rec.ID = ruleID
	updated, err := h.Repo.UpdateStepperRule(ctx, rec)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update rule"})
		return
	}
	h.syncStepperRule(ctx, updated, "rule.updated")
	writeJSON(w, http.StatusOK, toStepperResponse(updated))
}

func (h *Handler) handleStepperRuleDelete(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	if err := h.Repo.DeleteStepperRule(ctx, ruleID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	// Remove from rules table so the scheduler stops evaluating it.
	_ = h.Repo.DeleteRule(ctx, ruleID)
	if h.Bus != nil {
		_ = h.Bus.Publish("rule.deleted", map[string]any{"rule_id": ruleID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleStepperRuleEnable(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	if err := h.Repo.SetStepperRuleEnabled(ctx, ruleID, true); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	if rec, err := h.Repo.GetStepperRule(ctx, ruleID); err == nil {
		h.syncStepperRule(ctx, rec, "rule.enabled")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleStepperRuleDisable(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	if err := h.Repo.SetStepperRuleEnabled(ctx, ruleID, false); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	if rec, err := h.Repo.GetStepperRule(ctx, ruleID); err == nil {
		h.syncStepperRule(ctx, rec, "rule.disabled")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleStepperRuleList(w http.ResponseWriter, r *http.Request) {
	unitID := strings.TrimSpace(r.URL.Query().Get("unitId"))
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	recs, err := h.Repo.ListStepperRules(ctx, unitID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to list rules"})
		return
	}
	responses := make([]stepperRuleResponse, 0, len(recs))
	for _, rec := range recs {
		responses = append(responses, toStepperResponse(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": responses})
}

func toStepperRecord(req stepperRuleRequest, includeEnabled bool) storage.StepperRule {
	enabled := true
	if includeEnabled && req.Enabled != nil {
		enabled = *req.Enabled
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.RuleType
	}
	config := json.RawMessage("{}")
	if len(req.Config) > 0 {
		config = req.Config
	}
	return storage.StepperRule{
		UnitID:      req.UnitID,
		Name:        name,
		RuleType:    req.RuleType,
		ParameterID: req.ParameterID,
		Config:      config,
		Enabled:     enabled,
	}
}

func toStepperResponse(rec storage.StepperRule) stepperRuleResponse {
	return stepperRuleResponse{
		ID:          rec.ID,
		UnitID:      rec.UnitID,
		Name:        rec.Name,
		RuleType:    rec.RuleType,
		ParameterID: rec.ParameterID,
		Enabled:     rec.Enabled,
		Config:      rec.Config,
		CreatedAt:   rec.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   rec.UpdatedAt.UTC().Format(time.RFC3339),
	}
}


func parameterInUnit(table string, columns []string, parameterID string) bool {
	paramTable, paramCol := parseParameterID(parameterID)
	if paramTable == "" || paramCol == "" {
		return false
	}
	if paramTable != table {
		return false
	}
	for _, col := range columns {
		if col == paramCol {
			return true
		}
	}
	return false
}
