package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"predixaai-backend/services/rule-service/internal/bus"
	"predixaai-backend/services/rule-service/internal/crypto"
	"predixaai-backend/services/rule-service/internal/rules"
	"predixaai-backend/services/rule-service/internal/storage"
)

type Handler struct {
	Repo      *storage.Repository
	Bus       *bus.Publisher
	Encryptor crypto.Encryptor
	MinPoll   int
	MaxPoll   int
	Timeout   time.Duration
	DBConnectorURL string
	SchedulerURL   string
}

type errorResponse struct {
	Ok      bool                `json:"ok"`
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details []rules.ErrorDetail `json:"details"`
}

type connectionRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

type rulePromptRequest struct {
	RulePrompt    string                `json:"rulePrompt"`
	ConnectionRef string                `json:"connectionRef"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Enabled       *bool                 `json:"enabled"`
	TableName     string                `json:"tableName"`
	ColumnName    string                `json:"columnName"`
	Timestamp     string                `json:"timestamp"`
	Parameters    []rules.ParameterSpec `json:"parameters"`
	Draft         *rules.RuleDraft      `json:"draft"`
}

type ruleUpdateRequest struct {
	RulePrompt string                `json:"rulePrompt"`
	Rule       *rules.RuleSpec       `json:"rule"`
	Enabled    *bool                 `json:"enabled"`
	TableName  string                `json:"tableName"`
	ColumnName string                `json:"columnName"`
	Timestamp  string                `json:"timestamp"`
	Parameters []rules.ParameterSpec `json:"parameters"`
	Draft      *rules.RuleDraft      `json:"draft"`
}

func buildRulePrompt(prompt string, tableName string, columnName string, timestamp string) string {
	parts := make([]string, 0, 4)
	if strings.TrimSpace(tableName) != "" {
		parts = append(parts, "table "+strings.TrimSpace(tableName))
	}
	if strings.TrimSpace(columnName) != "" {
		parts = append(parts, "column "+strings.TrimSpace(columnName))
	}
	if strings.TrimSpace(timestamp) != "" {
		parts = append(parts, "timestamp "+strings.TrimSpace(timestamp))
	}
	if strings.TrimSpace(prompt) != "" {
		parts = append(parts, strings.TrimSpace(prompt))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func buildDraft(req rulePromptRequest) *rules.RuleDraft {
	if req.Draft != nil {
		return req.Draft
	}
	if strings.TrimSpace(req.TableName) == "" && strings.TrimSpace(req.Timestamp) == "" && len(req.Parameters) == 0 && strings.TrimSpace(req.ColumnName) == "" {
		return nil
	}
	params := req.Parameters
	if len(params) == 0 && strings.TrimSpace(req.ColumnName) != "" {
		params = []rules.ParameterSpec{{ParameterName: strings.TrimSpace(req.ColumnName), ValueColumn: strings.TrimSpace(req.ColumnName)}}
	}
	return &rules.RuleDraft{
		Table:           strings.TrimSpace(req.TableName),
		TimestampColumn: strings.TrimSpace(req.Timestamp),
		Parameters:      params,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/connections", h.handleConnections)
	r.Post("/rules/validate", h.handleRulesValidate)
	h.RegisterStepperRoutes(r)
	h.RegisterMachineUnitRoutes(r)
	h.RegisterProductionLineRoutes(r)
	r.Route("/rules", func(r chi.Router) {
		r.Post("/", h.handleRulesCreate)
		r.Get("/", h.handleRulesList)
		r.Get("/{id}", h.handleRuleGetByID)
		r.Put("/{id}", h.handleRuleUpdateByID)
		r.Post("/{id}/enable", h.handleRuleEnable)
		r.Post("/{id}/disable", h.handleRuleDisable)
		r.Get("/{id}/alerts", h.handleRuleAlerts)
	})
	r.Post("/alerts/{id}/treated", h.handleAlertUpdate)
}

func (h *Handler) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "message": "method not allowed"})
		return
	}
	var req connectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	cipherText, err := h.Encryptor.Encrypt(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "encryption failed"})
		return
	}
	id, err := h.Repo.CreateConnection(ctx, storage.DBConnection{
		Name:     req.Name,
		Type:     req.Type,
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: cipherText,
		Database: req.Database,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to store connection"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectionRef": id})
}

func (h *Handler) handleRulesValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "message": "method not allowed"})
		return
	}
	var req rulePromptRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	draft := buildDraft(req)
	spec, parseErr := rules.ParsePromptWithDraft(req.RulePrompt, req.ConnectionRef, draft)
	if parseErr != nil {
		writeRuleError(w, parseErr)
		return
	}
	if req.Name != "" {
		spec.Name = req.Name
	}
	if req.Description != "" {
		spec.Description = req.Description
	}
	if req.Enabled != nil {
		spec.Enabled = *req.Enabled
	}
	if spec.Name == "" {
		spec.Name = spec.ParameterName
	}
	if err := rules.ValidateRuleSpec(spec, h.MinPoll, h.MaxPoll); err != nil {
		writeRuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rule": spec})
}

func (h *Handler) handleRulesCreate(w http.ResponseWriter, r *http.Request) {
	var req rulePromptRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	draft := buildDraft(req)
	spec, parseErr := rules.ParsePromptWithDraft(req.RulePrompt, req.ConnectionRef, draft)
	if parseErr != nil {
		writeRuleError(w, parseErr)
		return
	}
	if req.Name != "" {
		spec.Name = req.Name
	}
	if req.Description != "" {
		spec.Description = req.Description
	}
	if req.Enabled != nil {
		spec.Enabled = *req.Enabled
	}
	if spec.Name == "" {
		spec.Name = spec.ParameterName
	}
	if err := rules.ValidateRuleSpec(spec, h.MinPoll, h.MaxPoll); err != nil {
		writeRuleError(w, err)
		return
	}
	ruleJSON, _ := json.Marshal(spec)
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	id, err := h.Repo.CreateRule(ctx, storage.RuleRecord{
		Name:            spec.Name,
		Description:     spec.Description,
		ConnectionRef:   spec.ConnectionRef,
		ParameterName:   spec.ParameterName,
		RuleJSON:        ruleJSON,
		Enabled:         spec.Enabled,
		Status:          "DRAFT",
		LastError:       nil,
		LastValidatedAt: nil,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to persist rule"})
		return
	}
	_ = h.Bus.Publish("rule.created", map[string]any{"rule_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"rule_id": id, "rule": spec})
}

func (h *Handler) handleRulesList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	rulesList, err := h.Repo.ListRules(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to list rules"})
		return
	}
	writeJSON(w, http.StatusOK, rulesList)
}
func (h *Handler) handleRuleGetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	rule, err := h.Repo.GetRule(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handler) handleRuleUpdateByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req ruleUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	rec, err := h.Repo.GetRule(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "rule not found"})
		return
	}
	var spec rules.RuleSpec
	if req.Rule != nil {
		spec = *req.Rule
	} else if req.RulePrompt != "" || req.TableName != "" || req.ColumnName != "" || req.Timestamp != "" || len(req.Parameters) > 0 || req.Draft != nil {
		draft := buildDraft(rulePromptRequest{
			RulePrompt: req.RulePrompt,
			TableName:  req.TableName,
			ColumnName: req.ColumnName,
			Timestamp:  req.Timestamp,
			Parameters: req.Parameters,
			Draft:      req.Draft,
		})
		parsed, parseErr := rules.ParsePromptWithDraft(req.RulePrompt, rec.ConnectionRef, draft)
		if parseErr != nil {
			writeRuleError(w, parseErr)
			return
		}
		spec = parsed
	}
	if req.Enabled != nil {
		spec.Enabled = *req.Enabled
	}
	if spec.Name == "" {
		spec.Name = rec.Name
	}
	if err := rules.ValidateRuleSpec(spec, h.MinPoll, h.MaxPoll); err != nil {
		writeRuleError(w, err)
		return
	}
	ruleJSON, _ := json.Marshal(spec)
	rec.Name = spec.Name
	rec.Description = spec.Description
	rec.ParameterName = spec.ParameterName
	rec.RuleJSON = ruleJSON
	rec.Enabled = spec.Enabled
	rec.Status = "DRAFT"
	rec.LastError = nil
	rec.LastValidatedAt = nil
	if err := h.Repo.UpdateRule(ctx, rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update rule"})
		return
	}
	_ = h.Bus.Publish("rule.updated", map[string]any{"rule_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rule": spec})
}

func (h *Handler) handleRuleEnable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Repo.SetRuleEnabled(r.Context(), id, true, "DRAFT"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to enable rule"})
		return
	}
	_ = h.Bus.Publish("rule.enabled", map[string]any{"rule_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleRuleDisable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Repo.SetRuleEnabled(r.Context(), id, false, "DISABLED"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to disable rule"})
		return
	}
	_ = h.Bus.Publish("rule.disabled", map[string]any{"rule_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleRuleAlerts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	alerts, err := h.Repo.ListAlerts(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to fetch alerts"})
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (h *Handler) handleAlertUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	alertID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid alert id"})
		return
	}
	var req struct {
		Treated bool `json:"treated"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	if err := h.Repo.UpdateAlertTreated(ctx, alertID, req.Treated); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "failed to update alert"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeRuleError(w http.ResponseWriter, parseErr *rules.ParseError) {
	writeJSON(w, http.StatusBadRequest, errorResponse{
		Ok:      false,
		Code:    parseErr.Code,
		Message: parseErr.Message,
		Details: parseErr.Details,
	})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
