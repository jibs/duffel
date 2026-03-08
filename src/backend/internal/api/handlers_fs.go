package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"duffel/src/backend/internal/markdown"
	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"

	"github.com/go-chi/chi/v5"
)

func extractPath(r *http.Request) string {
	p := chi.URLParam(r, "*")
	if p == "" {
		return "/"
	}
	return p
}

type fileGetResponse struct {
	Path        string          `json:"path"`
	Content     string          `json:"content"`
	Size        int64           `json:"size"`
	ModTime     time.Time       `json:"modTime"`
	IsJournal   bool            `json:"isJournal"`
	Recommended []search.Result `json:"recommended"`
}

func handleFSGet(store *storage.Store, getSearcher func() *search.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)
		archived := r.URL.Query().Get("archived") == "true"

		// Try as directory first
		dir, err := store.List(urlPath, archived)
		if err == nil {
			// If archived flag is set, include archived entries
			if archived {
				archivedDir, err := store.ListArchived(urlPath)
				if err == nil && len(archivedDir.Entries) > 0 {
					dir.Entries = append(dir.Entries, archivedDir.Entries...)
				}
			}
			writeJSON(w, http.StatusOK, dir)
			return
		}

		if !errors.Is(err, storage.ErrNotDirectory) {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found", urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}

		// Try as file
		file, err := store.Read(urlPath)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found", urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}

		recommended := recommendForFile(store, getSearcher, file)
		if recommended == nil {
			recommended = []search.Result{}
		}

		writeJSON(w, http.StatusOK, fileGetResponse{
			Path:        file.Path,
			Content:     file.Content,
			Size:        file.Size,
			ModTime:     file.ModTime,
			IsJournal:   file.IsJournal,
			Recommended: recommended,
		})
	}
}

func handleFSPut(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", urlPath)
			return
		}

		err := store.Write(urlPath, []byte(body.Content))
		if err != nil {
			if errors.Is(err, storage.ErrIsJournal) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			if errors.Is(err, storage.ErrPathTraversal) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			if errors.Is(err, markdown.ErrNullBytes) || errors.Is(err, markdown.ErrBinary) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}
		triggerContentChanged(onContentChanged)

		// Read back the file to return full info
		file, err := store.Read(urlPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}
		writeJSON(w, http.StatusOK, file)
	}
}

func handleFSDelete(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		err := store.Delete(urlPath)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found", urlPath)
				return
			}
			if errors.Is(err, storage.ErrNotEmpty) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			if errors.Is(err, storage.ErrPathTraversal) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}
		triggerContentChanged(onContentChanged)

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "path": urlPath})
	}
}

func handleFSMove(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srcPath := extractPath(r)

		var body struct {
			Destination string `json:"destination"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", srcPath)
			return
		}
		if body.Destination == "" {
			writeError(w, http.StatusBadRequest, "destination is required", srcPath)
			return
		}

		err := store.Move(srcPath, body.Destination)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found", srcPath)
				return
			}
			if errors.Is(err, storage.ErrPathTraversal) {
				writeError(w, http.StatusBadRequest, err.Error(), srcPath)
				return
			}
			if errors.Is(err, storage.ErrAlreadyExists) {
				writeError(w, http.StatusConflict, "destination already exists", body.Destination)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), srcPath)
			return
		}
		triggerContentChanged(onContentChanged)

		writeJSON(w, http.StatusOK, map[string]string{
			"status": "moved",
			"from":   srcPath,
			"to":     body.Destination,
		})
	}
}

func triggerContentChanged(onContentChanged func()) {
	if onContentChanged != nil {
		onContentChanged()
	}
}

func handleFSPost(store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		var body struct {
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", urlPath)
			return
		}

		if strings.ToLower(body.Type) != "directory" {
			writeError(w, http.StatusBadRequest, "type must be \"directory\"", urlPath)
			return
		}

		err := store.CreateDir(urlPath)
		if err != nil {
			if errors.Is(err, storage.ErrPathTraversal) {
				writeError(w, http.StatusBadRequest, err.Error(), urlPath)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "path": urlPath})
	}
}
