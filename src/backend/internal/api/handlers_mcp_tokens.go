package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"duffel/src/backend/internal/auth"
)

func handleMCPTokens(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}

		switch r.Method {
		case http.MethodGet:
			tokens, err := authService.ListPATs(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
		case http.MethodPost:
			var body struct {
				Label string `json:"label"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body", r.URL.Path)
				return
			}
			plain, rec, err := authService.CreatePAT(r.Context(), body.Label)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{
				"token":  plain,
				"record": rec,
			})
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleMCPToken(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/mcp/tokens/"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "token id is required", r.URL.Path)
			return
		}
		if err := authService.RevokeToken(r.Context(), id); err != nil {
			if errors.Is(err, auth.ErrUnauthorized) {
				writeError(w, http.StatusNotFound, "token not found", r.URL.Path)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
	}
}
