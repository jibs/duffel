package api

import (
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	Error string `json:"error"`
	Path  string `json:"path,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string, path string) {
	writeJSON(w, status, errorResponse{Error: msg, Path: path})
}
