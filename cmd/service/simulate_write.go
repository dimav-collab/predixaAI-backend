package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"predixaai-backend/cmd/service/internal/connections"
)

// identRe validates SQL identifiers per spec: letter/underscore start,
// then alphanumerics or underscores, max 128 chars total.
var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,127}$`)

// simulateWriteRequest is the JSON body for POST /simulate/write.
type simulateWriteRequest struct {
	ConnectionRef   string         `json:"connectionRef"`
	TableName       string         `json:"tableName"`
	ColumnName      string         `json:"columnName"`
	TimestampColumn string         `json:"timestampColumn"`
	Value           float64        `json:"value"`
	ExtraColumns    map[string]any `json:"extraColumns"`
}

type simulateWriteResponse struct {
	Ok           bool  `json:"ok"`
	RowsAffected int64 `json:"rowsAffected"`
}

type simulateWriteError struct {
	Ok      bool     `json:"ok"`
	Error   string   `json:"error"`
	Message string   `json:"message,omitempty"`
	Details []string `json:"details,omitempty"`
}

func writeSimError(w http.ResponseWriter, status int, errCode string, details ...string) {
	e := simulateWriteError{Ok: false, Error: errCode}
	if len(details) > 0 {
		e.Details = details
	}
	writeJSON(w, status, e)
}

func writeSimErrorMsg(w http.ResponseWriter, status int, errCode, message string) {
	writeJSON(w, status, simulateWriteError{Ok: false, Error: errCode, Message: message})
}

func (h *Handler) HandleSimulateWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSimError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	// Use a lenient decoder here so that future optional fields don't break existing clients.
	var req simulateWriteRequest
	if err := decodeJSONLenient(r, &req); err != nil {
		writeSimError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	if details := validateSimulateWriteRequest(req); len(details) > 0 {
		writeSimError(w, http.StatusBadRequest, "validation_error", details...)
		return
	}

	connectionCfg, err := h.resolveByRef(r, req.ConnectionRef)
	if err != nil {
		switch {
		case isErr(err, connections.ErrNotFound), isErr(err, connections.ErrInvalidInput):
			writeSimError(w, http.StatusNotFound, "connection_not_found")
		case isErr(err, connections.ErrNotConfigured):
			writeSimError(w, http.StatusBadRequest, "connection_not_configured")
		default:
			// A DB-level error during lookup (e.g. bad UUID cast) also means
			// the connection wasn't found from the caller's perspective.
			writeSimError(w, http.StatusNotFound, "connection_not_found")
		}
		return
	}

	conn, err := h.ConnectorFactory(connectionCfg)
	if err != nil {
		writeSimErrorMsg(w, http.StatusBadRequest, "connection_failed", err.Error())
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := conn.TestConnection(ctx); err != nil {
		writeSimError(w, http.StatusBadGateway, "db_unreachable")
		return
	}

	n, err := conn.WriteRow(ctx, req.TableName, req.ColumnName, req.TimestampColumn, req.Value, req.ExtraColumns)
	if err != nil {
		log.Printf("[simulate/write] write failed connectionRef=%s table=%s column=%s: %v",
			req.ConnectionRef, req.TableName, req.ColumnName, err)
		writeSimErrorMsg(w, http.StatusBadGateway, "db_write_failed", sanitizeDBError(err))
		return
	}

	log.Printf("[simulate/write] ok connectionRef=%s table=%s column=%s value=%v rowsAffected=%d",
		req.ConnectionRef, req.TableName, req.ColumnName, req.Value, n)

	writeJSON(w, http.StatusOK, simulateWriteResponse{Ok: true, RowsAffected: n})
}

// validateSimulateWriteRequest returns a list of validation failure messages.
func validateSimulateWriteRequest(req simulateWriteRequest) []string {
	var details []string
	if strings.TrimSpace(req.ConnectionRef) == "" {
		details = append(details, "connectionRef: required")
	}
	if !identRe.MatchString(strings.TrimSpace(req.TableName)) {
		details = append(details, "tableName: must be a valid SQL identifier (letters, digits, underscore; max 128 chars)")
	}
	if !identRe.MatchString(strings.TrimSpace(req.ColumnName)) {
		details = append(details, "columnName: must be a valid SQL identifier (letters, digits, underscore; max 128 chars)")
	}
	if !identRe.MatchString(strings.TrimSpace(req.TimestampColumn)) {
		details = append(details, "timestampColumn: must be a valid SQL identifier (letters, digits, underscore; max 128 chars)")
	}
	for k := range req.ExtraColumns {
		if !identRe.MatchString(strings.TrimSpace(k)) {
			details = append(details, fmt.Sprintf("extraColumns key %q: must be a valid SQL identifier", k))
		}
	}
	return details
}

// sanitizeDBError returns a safe message from a DB error — never the raw driver string
// (which could contain connection strings). We keep it brief.
func sanitizeDBError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Truncate very long messages and strip anything after a newline.
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		msg = msg[:idx]
	}
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}
