package api

import (
	"errors"
	"net/http"

	"duffel/src/backend/internal/storage"
)

func handleArchive(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		err := store.Archive(urlPath)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found", urlPath)
				return
			}
			if errors.Is(err, storage.ErrIsDirectory) {
				writeError(w, http.StatusBadRequest, "cannot archive a directory", urlPath)
				return
			}
			if errors.Is(err, storage.ErrPathTraversal) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			if errors.Is(err, storage.ErrAlreadyExists) {
				writeError(w, http.StatusConflict, "archive destination already exists", urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}
		triggerContentChanged(onContentChanged)

		writeJSON(w, http.StatusOK, map[string]string{"status": "archived", "path": urlPath})
	}
}

func handleUnarchive(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		err := store.Unarchive(urlPath)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found", urlPath)
				return
			}
			if errors.Is(err, storage.ErrPathTraversal) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			if errors.Is(err, storage.ErrAlreadyExists) {
				writeError(w, http.StatusConflict, "destination already exists", urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}
		triggerContentChanged(onContentChanged)

		writeJSON(w, http.StatusOK, map[string]string{"status": "unarchived", "path": urlPath})
	}
}
