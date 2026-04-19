package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type errorResponse struct {
	Error string `json:"error"`
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("invalid json payload")
		}
		return err
	}
	return nil
}

// decodeJSONLenient decodes JSON without rejecting unknown fields.
// Use this for endpoints where the client may send optional fields
// that aren't present in the server struct.
func decodeJSONLenient(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

// isErr reports whether err (or any in its chain) matches target.
func isErr(err, target error) bool {
	return errors.Is(err, target)
}

