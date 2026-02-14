package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"duffel/src/backend/internal/storage"
)

func handleJournal(store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		// Check if this is an append request (path ends with /append)
		if basePath, ok := strings.CutSuffix(urlPath, "/append"); ok {
			handleJournalAppend(store, w, r, basePath)
			return
		}

		// Otherwise it's a create request
		handleJournalCreate(store, w, r, urlPath)
	}
}

func handleJournalCreate(store *storage.Store, w http.ResponseWriter, r *http.Request, urlPath string) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", urlPath)
		return
	}

	err := store.CreateJournal(urlPath, body.Content)
	if err != nil {
		if errors.Is(err, storage.ErrPathTraversal) {
			writeError(w, http.StatusBadRequest, err.Error(), urlPath)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}

	file, err := store.Read(urlPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}
	writeJSON(w, http.StatusCreated, file)
}

func handleJournalAppend(store *storage.Store, w http.ResponseWriter, r *http.Request, urlPath string) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", urlPath)
		return
	}

	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required", urlPath)
		return
	}

	err := store.AppendJournal(urlPath, body.Content)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found", urlPath)
			return
		}
		if errors.Is(err, storage.ErrPathTraversal) {
			writeError(w, http.StatusBadRequest, err.Error(), urlPath)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}

	file, err := store.Read(urlPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}
	writeJSON(w, http.StatusOK, file)
}
