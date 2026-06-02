package api

import (
	"context"
	"log/slog"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"predixaai-backend/services/rule-service/internal/rules"
	"predixaai-backend/services/rule-service/internal/storage"
)

const (
	maxSelectedColumns = 200
	maxRuleIDs         = 500
)

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type machineUnitRequest struct {
	UnitID            string          `json:"unitId"`
	UnitName          string          `json:"unitName"`
	ConnectionRef     string          `json:"connectionRef"`
	SelectedTable     string          `json:"selectedTable"`
	TimestampColumn   string          `json:"timestampColumn"`
	SelectedColumns   []string        `json:"selectedColumns"`
	LiveParameters    json.RawMessage `json:"liveParameters"`
	RuleIDs           ruleIDList      `json:"rule"`
	Pos               *positionInput  `json:"pos"`
	ProductionLineID  string          `json:"productionLineId"`
}

type machineUnitResponse struct {
	UnitID            string          `json:"unitId"`
	UnitName          string          `json:"unitName"`
	ConnectionRef     string          `json:"connectionRef"`
	SelectedTable     string          `json:"selectedTable"`
	TimestampColumn   string          `json:"timestampColumn"`
	SelectedColumns   []string        `json:"selectedColumns"`
	LiveParameters    json.RawMessage `json:"liveParameters"`
	RuleIDs           []string        `json:"rule"`
	Pos               positionInput   `json:"pos"`
	ProductionLineID  string          `json:"productionLineId"`
}

type updateRulesRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type updateColumnsRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type replaceColumnsRequest struct {
	SelectedColumns []string `json:"selectedColumns"`
}

type updateTableRequest struct {
	SelectedTable   string   `json:"selectedTable"`
	SelectedColumns []string `json:"selectedColumns"`
	KeepColumns     bool     `json:"keepColumns"`
}

type updateConnectionRequest struct {
	ConnectionRef string `json:"connectionRef"`
}

type updatePositionRequest struct {
	Pos positionInput `json:"pos"`
}

type positionInput struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type ruleIDList []string

func (r *ruleIDList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*r = []string{}
		return nil
	}
	if strings.HasPrefix(trimmed, "{") {
		*r = []string{}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	*r = values
	return nil
}

func (h *Handler) RegisterMachineUnitRoutes(r chi.Router) {
	r.Route("/machine-units", func(r chi.Router) {
		r.Post("/", h.handleMachineUnitCreate)
		r.Get("/", h.handleMachineUnitList)
		r.Get("/{unitId}", h.handleMachineUnitGet)
		r.Put("/{unitId}", h.handleMachineUnitUpdate)
		r.Delete("/{unitId}", h.handleMachineUnitDelete)
		r.Post("/{unitId}/rules", h.handleMachineUnitRules)
		r.Post("/{unitId}/columns", h.handleMachineUnitColumns)
		r.Put("/{unitId}/columns", h.handleMachineUnitColumnsReplace)
		r.Put("/{unitId}/table", h.handleMachineUnitTable)
		r.Put("/{unitId}/connection", h.handleMachineUnitConnection)
		r.Patch("/{unitId}/position", h.handleMachineUnitPosition)
		r.Post("/{unitId}/parameter-stats", h.handleUpsertParameterStats)
		r.Get("/{unitId}/parameter-stats", h.handleListParameterStats)
	})
}

func (h *Handler) handleMachineUnitCreate(w http.ResponseWriter, r *http.Request) {
	var req machineUnitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	unit, details := h.validateMachineUnitRequest(r.Context(), req)
	if len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid machine unit request", details)
		return
	}
	if strings.TrimSpace(req.UnitID) != "" {
		_, err := h.Repo.GetMachineUnit(r.Context(), req.UnitID)
		if err != nil {
			if err == storage.ErrNotFound {
				writeValidationError(w, "UNIT_ID_NOT_ALLOWED", "unitId is server-generated", []rules.ErrorDetail{{Field: "unitId", Problem: "not_allowed", Hint: "Remove unitId or use an existing unitId"}})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch machine unit"})
			return
		}
		unit.UnitID = req.UnitID
		updated, err := h.Repo.UpdateMachineUnit(r.Context(), unit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update machine unit"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
		return
	}
	unit.UnitID = "machine-" + uuid.NewString()
	created, err := h.Repo.CreateMachineUnit(r.Context(), unit)
	if err != nil {
		slog.Error("create machine unit", "err", err); writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to create machine unit"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(created)})
}

func (h *Handler) handleMachineUnitList(w http.ResponseWriter, r *http.Request) {
	units, err := h.Repo.ListMachineUnits(r.Context())
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

func (h *Handler) handleMachineUnitGet(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	unit, err := h.Repo.GetMachineUnit(r.Context(), unitID)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch machine unit"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(unit)})
}

func (h *Handler) handleMachineUnitUpdate(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req machineUnitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.UnitID) != "" && req.UnitID != unitID {
		writeValidationError(w, "UNIT_ID_IMMUTABLE", "unitId cannot be changed", []rules.ErrorDetail{{Field: "unitId", Problem: "immutable", Hint: "unitId in path must be preserved"}})
		return
	}
	unit, details := h.validateMachineUnitRequest(r.Context(), req)
	if len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid machine unit request", details)
		return
	}
	unit.UnitID = unitID
	updated, err := h.Repo.UpdateMachineUnit(r.Context(), unit)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update machine unit"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) handleMachineUnitDelete(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	if err := h.Repo.DeleteMachineUnit(r.Context(), unitID); err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to delete machine unit"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleMachineUnitRules(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req updateRulesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	add := dedupePreserveOrder(req.Add)
	remove := dedupePreserveOrder(req.Remove)
	if details := validateUUIDList("rule.add", add); len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid rule ids", details)
		return
	}
	if details := validateUUIDList("rule.remove", remove); len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid rule ids", details)
		return
	}
	missing, err := h.findMissingRuleIDs(r.Context(), add)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to validate rule ids"})
		return
	}
	if len(missing) > 0 {
		writeValidationError(w, "RULES_NOT_FOUND", "some rule ids do not exist", []rules.ErrorDetail{{Field: "rule.add", Problem: "not_found", Hint: "Missing: " + strings.Join(missing, ", ")}})
		return
	}
	current, err := h.Repo.GetMachineUnit(r.Context(), unitID)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch machine unit"})
		return
	}
	if len(applyAddRemove(current.RuleIDs, add, remove)) > maxRuleIDs {
		writeValidationError(w, "LIMIT_EXCEEDED", "rule limit exceeded", []rules.ErrorDetail{{Field: "rule", Problem: "max", Hint: "Maximum 500 rules per unit"}})
		return
	}
	updated, err := h.Repo.UpdateRules(r.Context(), unitID, add, remove)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update rule ids"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) handleMachineUnitColumns(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req updateColumnsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	add := dedupePreserveOrder(req.Add)
	remove := dedupePreserveOrder(req.Remove)
	if details := validateIdentifierList("selectedColumns.add", add); len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid selectedColumns", details)
		return
	}
	if details := validateIdentifierList("selectedColumns.remove", remove); len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid selectedColumns", details)
		return
	}
	current, err := h.Repo.GetMachineUnit(r.Context(), unitID)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch machine unit"})
		return
	}
	if len(applyAddRemove(current.SelectedColumns, add, remove)) > maxSelectedColumns {
		writeValidationError(w, "LIMIT_EXCEEDED", "selectedColumns limit exceeded", []rules.ErrorDetail{{Field: "selectedColumns", Problem: "max", Hint: "Maximum 200 columns"}})
		return
	}
	updated, err := h.Repo.UpdateColumns(r.Context(), unitID, add, remove)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update selected columns"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) handleMachineUnitColumnsReplace(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req replaceColumnsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	columns := dedupePreserveOrder(req.SelectedColumns)
	if len(columns) > maxSelectedColumns {
		writeValidationError(w, "LIMIT_EXCEEDED", "selectedColumns limit exceeded", []rules.ErrorDetail{{Field: "selectedColumns", Problem: "max", Hint: "Maximum 200 columns"}})
		return
	}
	if details := validateIdentifierList("selectedColumns", columns); len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid selectedColumns", details)
		return
	}
	current, err := h.Repo.GetMachineUnit(r.Context(), unitID)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch machine unit"})
		return
	}
	updated, err := h.Repo.UpdateColumns(r.Context(), unitID, columns, current.SelectedColumns)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update selected columns"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) handleMachineUnitTable(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req updateTableRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.SelectedTable) == "" || !identifierRe.MatchString(req.SelectedTable) {
		writeValidationError(w, "VALIDATION_ERROR", "invalid selectedTable", []rules.ErrorDetail{{Field: "selectedTable", Problem: "invalid", Hint: "Use a valid table identifier"}})
		return
	}
	var columns *[]string
	if len(req.SelectedColumns) > 0 {
		clean := dedupePreserveOrder(req.SelectedColumns)
		if len(clean) > maxSelectedColumns {
			writeValidationError(w, "LIMIT_EXCEEDED", "selectedColumns limit exceeded", []rules.ErrorDetail{{Field: "selectedColumns", Problem: "max", Hint: "Maximum 200 columns"}})
			return
		}
		if details := validateIdentifierList("selectedColumns", clean); len(details) > 0 {
			writeValidationError(w, "VALIDATION_ERROR", "invalid selectedColumns", details)
			return
		}
		columns = &clean
	}
	updated, err := h.Repo.UpdateTable(r.Context(), unitID, req.SelectedTable, columns, req.KeepColumns)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update selected table"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) handleMachineUnitConnection(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req updateConnectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.ConnectionRef) == "" {
		writeValidationError(w, "VALIDATION_ERROR", "missing connectionRef", []rules.ErrorDetail{{Field: "connectionRef", Problem: "missing", Hint: "Provide connectionRef"}})
		return
	}
	if _, err := uuid.Parse(req.ConnectionRef); err != nil {
		writeValidationError(w, "VALIDATION_ERROR", "invalid connectionRef", []rules.ErrorDetail{{Field: "connectionRef", Problem: "invalid", Hint: "Must be a UUID"}})
		return
	}
	exists, err := h.Repo.ConnectionExists(r.Context(), req.ConnectionRef)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to validate connectionRef"})
		return
	}
	if !exists {
		writeValidationError(w, "CONNECTION_NOT_FOUND", "connectionRef does not exist", []rules.ErrorDetail{{Field: "connectionRef", Problem: "not_found", Hint: "Provide a valid connectionRef"}})
		return
	}
	updated, err := h.Repo.UpdateConnection(r.Context(), unitID, req.ConnectionRef)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update connection"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) handleMachineUnitPosition(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	var req updatePositionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if details := validatePosition(req.Pos); len(details) > 0 {
		writeValidationError(w, "VALIDATION_ERROR", "invalid position", details)
		return
	}
	updated, err := h.Repo.UpdatePosition(r.Context(), unitID, req.Pos.X, req.Pos.Y)
	if err != nil {
		if err == storage.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "machine unit not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update position"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": buildMachineUnitResponse(updated)})
}

func (h *Handler) validateMachineUnitRequest(ctx context.Context, req machineUnitRequest) (storage.MachineUnit, []rules.ErrorDetail) {
	details := []rules.ErrorDetail{}
	unitName := strings.TrimSpace(req.UnitName)
	if unitName == "" {
		details = append(details, rules.ErrorDetail{Field: "unitName", Problem: "missing", Hint: "Provide unitName"})
	}
	connectionRef := strings.TrimSpace(req.ConnectionRef)
	if connectionRef == "" {
		details = append(details, rules.ErrorDetail{Field: "connectionRef", Problem: "missing", Hint: "Provide connectionRef"})
	} else if _, err := uuid.Parse(connectionRef); err != nil {
		details = append(details, rules.ErrorDetail{Field: "connectionRef", Problem: "invalid", Hint: "Must be a UUID"})
	} else if ok, err := h.Repo.ConnectionExists(ctx, connectionRef); err != nil {
		details = append(details, rules.ErrorDetail{Field: "connectionRef", Problem: "invalid", Hint: "Failed to validate connectionRef"})
	} else if !ok {
		details = append(details, rules.ErrorDetail{Field: "connectionRef", Problem: "not_found", Hint: "Provide a valid connectionRef"})
	}
	selectedTable := strings.TrimSpace(req.SelectedTable)
	if selectedTable == "" {
		details = append(details, rules.ErrorDetail{Field: "selectedTable", Problem: "missing", Hint: "Provide selectedTable"})
	} else if !identifierRe.MatchString(selectedTable) {
		details = append(details, rules.ErrorDetail{Field: "selectedTable", Problem: "invalid", Hint: "Use a valid table identifier"})
	}
	timestampColumn := strings.TrimSpace(req.TimestampColumn)
	if timestampColumn != "" && !identifierRe.MatchString(timestampColumn) {
		details = append(details, rules.ErrorDetail{Field: "timestampColumn", Problem: "invalid", Hint: "Use a valid column identifier"})
	}
	columns := dedupePreserveOrder(req.SelectedColumns)
	if len(columns) > maxSelectedColumns {
		details = append(details, rules.ErrorDetail{Field: "selectedColumns", Problem: "max", Hint: "Maximum 200 columns"})
	} else {
		details = append(details, validateIdentifierList("selectedColumns", columns)...)
	}
	ruleIDs := dedupePreserveOrder([]string(req.RuleIDs))
	if len(ruleIDs) > maxRuleIDs {
		details = append(details, rules.ErrorDetail{Field: "rule", Problem: "max", Hint: "Maximum 500 rules"})
	} else {
		details = append(details, validateUUIDList("rule", ruleIDs)...)
		if len(ruleIDs) > 0 {
			missing, err := h.findMissingRuleIDs(ctx, ruleIDs)
			if err != nil {
				details = append(details, rules.ErrorDetail{Field: "rule", Problem: "invalid", Hint: "Failed to validate rule ids"})
			} else if len(missing) > 0 {
				details = append(details, rules.ErrorDetail{Field: "rule", Problem: "not_found", Hint: "Missing: " + strings.Join(missing, ", ")})
			}
		}
	}
	liveParams := normalizeRawMessage(req.LiveParameters)
	posX := 0.0
	posY := 0.0
	if req.Pos != nil {
		posDetails := validatePosition(*req.Pos)
		if len(posDetails) > 0 {
			details = append(details, posDetails...)
		} else {
			posX = req.Pos.X
			posY = req.Pos.Y
		}
	}
	return storage.MachineUnit{
		UnitName:         unitName,
		ConnectionRef:    connectionRef,
		SelectedTable:    selectedTable,
		TimestampColumn:  timestampColumn,
		SelectedColumns:  columns,
		LiveParameters:   liveParams,
		RuleIDs:          ruleIDs,
		PosX:             posX,
		PosY:             posY,
		ProductionLineID: strings.TrimSpace(req.ProductionLineID),
	}, details
}

func (h *Handler) findMissingRuleIDs(ctx context.Context, ids []string) ([]string, error) {
	existing, err := h.Repo.ListRuleIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	missing := []string{}
	for _, id := range ids {
		if _, ok := existing[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func validateIdentifierList(field string, values []string) []rules.ErrorDetail {
	details := []rules.ErrorDetail{}
	for idx, value := range values {
		if !identifierRe.MatchString(value) {
			details = append(details, rules.ErrorDetail{Field: field + "[" + itoa(idx) + "]", Problem: "invalid", Hint: "Must match ^[a-zA-Z_][a-zA-Z0-9_]*$"})
		}
	}
	return details
}

func validateUUIDList(field string, values []string) []rules.ErrorDetail {
	details := []rules.ErrorDetail{}
	for idx, value := range values {
		if _, err := uuid.Parse(value); err != nil {
			details = append(details, rules.ErrorDetail{Field: field + "[" + itoa(idx) + "]", Problem: "invalid", Hint: "Must be a UUID"})
		}
	}
	return details
}

func dedupePreserveOrder(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func applyAddRemove(current []string, add []string, remove []string) []string {
	removeSet := map[string]bool{}
	for _, value := range remove {
		removeSet[value] = true
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(current)+len(add))
	for _, value := range current {
		if removeSet[value] {
			continue
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, value := range add {
		if removeSet[value] {
			continue
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func normalizeRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("[]")
	}
	return raw
}

func buildMachineUnitResponse(unit storage.MachineUnit) machineUnitResponse {
	live := normalizeRawMessage(unit.LiveParameters)
	return machineUnitResponse{
		UnitID:           unit.UnitID,
		UnitName:         unit.UnitName,
		ConnectionRef:    unit.ConnectionRef,
		SelectedTable:    unit.SelectedTable,
		TimestampColumn:  unit.TimestampColumn,
		SelectedColumns:  unit.SelectedColumns,
		LiveParameters:   live,
		RuleIDs:          unit.RuleIDs,
		ProductionLineID: unit.ProductionLineID,
		Pos: positionInput{
			X: unit.PosX,
			Y: unit.PosY,
		},
	}
}

func writeValidationError(w http.ResponseWriter, code string, message string, details []rules.ErrorDetail) {
	writeJSON(w, http.StatusBadRequest, errorResponse{
		Ok:      false,
		Code:    code,
		Message: message,
		Details: details,
	})
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func validatePosition(pos positionInput) []rules.ErrorDetail {
	details := []rules.ErrorDetail{}
	if math.IsNaN(pos.X) || math.IsInf(pos.X, 0) {
		details = append(details, rules.ErrorDetail{Field: "pos.x", Problem: "invalid", Hint: "Must be a finite number"})
	} else if pos.X < 0 || pos.X > 1 {
		details = append(details, rules.ErrorDetail{Field: "pos.x", Problem: "out_of_range", Hint: "Must be between 0 and 1"})
	}
	if math.IsNaN(pos.Y) || math.IsInf(pos.Y, 0) {
		details = append(details, rules.ErrorDetail{Field: "pos.y", Problem: "invalid", Hint: "Must be a finite number"})
	} else if pos.Y < 0 || pos.Y > 1 {
		details = append(details, rules.ErrorDetail{Field: "pos.y", Problem: "out_of_range", Hint: "Must be between 0 and 1"})
	}
	return details
}

// ── Parameter stats endpoints ─────────────────────────────────────────────────

type parameterStatInput struct {
	ParameterID string   `json:"parameterId"`
	ColumnName  string   `json:"columnName"`
	TableName   string   `json:"tableName"`
	RowCount    int      `json:"rowCount"`
	Avg         *float64 `json:"avg"`
	Min         *float64 `json:"min"`
	Max         *float64 `json:"max"`
	StdDev      *float64 `json:"stdDev"`
	Sigma2Lower *float64 `json:"sigma2Lower"`
	Sigma2Upper *float64 `json:"sigma2Upper"`
	Sigma3Lower *float64 `json:"sigma3Lower"`
	Sigma3Upper *float64 `json:"sigma3Upper"`
}

type parameterStatOutput struct {
	ID          string   `json:"id"`
	ParameterID string   `json:"parameterId"`
	ColumnName  string   `json:"columnName"`
	TableName   string   `json:"tableName"`
	RowCount    int      `json:"rowCount"`
	Avg         *float64 `json:"avg"`
	Min         *float64 `json:"min"`
	Max         *float64 `json:"max"`
	StdDev      *float64 `json:"stdDev"`
	Sigma2Lower *float64 `json:"sigma2Lower"`
	Sigma2Upper *float64 `json:"sigma2Upper"`
	Sigma3Lower *float64 `json:"sigma3Lower"`
	Sigma3Upper *float64 `json:"sigma3Upper"`
	ComputedAt  string   `json:"computedAt"`
}

// POST /machine-units/:unitId/parameter-stats
// Accepts a batch of computed stats and upserts each one.
func (h *Handler) handleUpsertParameterStats(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	if strings.TrimSpace(unitID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid unit id"})
		return
	}

	var body struct {
		Stats []parameterStatInput `json:"stats"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Stats) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "stats array is empty"})
		return
	}

	repo := h.Repo
	for _, s := range body.Stats {
		if s.ParameterID == "" || s.ColumnName == "" || s.TableName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "parameterId, columnName and tableName are required"})
			return
		}
		record := storage.ParameterStats{
			ID:          uuid.NewString(),
			UnitID:      unitID,
			ParameterID: s.ParameterID,
			ColumnName:  s.ColumnName,
			TableName:   s.TableName,
			RowCount:    s.RowCount,
			AvgVal:      s.Avg,
			MinVal:      s.Min,
			MaxVal:      s.Max,
			StdDev:      s.StdDev,
			Sigma2Lower: s.Sigma2Lower,
			Sigma2Upper: s.Sigma2Upper,
			Sigma3Lower: s.Sigma3Lower,
			Sigma3Upper: s.Sigma3Upper,
		}
		if err := repo.UpsertParameterStats(r.Context(), record); err != nil {
			slog.Error("upsert parameter stats", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "db error"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Stats)})
}

// GET /machine-units/:unitId/parameter-stats
func (h *Handler) handleListParameterStats(w http.ResponseWriter, r *http.Request) {
	unitID := chi.URLParam(r, "unitId")
	if strings.TrimSpace(unitID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid unit id"})
		return
	}

	repo := h.Repo
	records, err := repo.ListParameterStatsByUnit(r.Context(), unitID)
	if err != nil {
		slog.Error("list parameter stats", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "db error"})
		return
	}

	out := make([]parameterStatOutput, 0, len(records))
	for _, s := range records {
		out = append(out, parameterStatOutput{
			ID:          s.ID,
			ParameterID: s.ParameterID,
			ColumnName:  s.ColumnName,
			TableName:   s.TableName,
			RowCount:    s.RowCount,
			Avg:         s.AvgVal,
			Min:         s.MinVal,
			Max:         s.MaxVal,
			StdDev:      s.StdDev,
			Sigma2Lower: s.Sigma2Lower,
			Sigma2Upper: s.Sigma2Upper,
			Sigma3Lower: s.Sigma3Lower,
			Sigma3Upper: s.Sigma3Upper,
			ComputedAt:  s.ComputedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stats": out})
}
