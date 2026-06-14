package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"duffel/src/backend/internal/markdown"
	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"

	"github.com/go-chi/chi/v5"
)

// maxImageBytes bounds the size of a binary (image) upload.
const maxImageBytes = 25 << 20 // 25 MiB

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
	Kind        string          `json:"kind"`
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

		// Image files are served as raw bytes (?raw=...) or as metadata-only JSON
		// (their bytes must never be routed through the string Content field).
		if storage.IsImagePath(urlPath) {
			if r.URL.Query().Get("raw") != "" {
				serveRawFile(w, r, store, urlPath)
				return
			}
			info, err := store.StatFile(urlPath)
			if err != nil {
				writeFSReadError(w, err, urlPath)
				return
			}
			writeJSON(w, http.StatusOK, fileGetResponse{
				Path:        info.Path,
				Content:     info.Content,
				Size:        info.Size,
				ModTime:     info.ModTime,
				IsJournal:   info.IsJournal,
				Kind:        info.Kind,
				Recommended: []search.Result{},
			})
			return
		}

		// Try as file
		file, err := store.Read(urlPath)
		if err != nil {
			writeFSReadError(w, err, urlPath)
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
			Kind:        file.Kind,
			Recommended: recommended,
		})
	}
}

func handleFSPut(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		// Binary (image) uploads are sent with a non-JSON Content-Type and a raw
		// body; text files continue to use the JSON {content} envelope.
		if isBinaryUpload(r) {
			handleBinaryPut(w, r, store, onContentChanged, urlPath)
			return
		}

		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body", urlPath)
			return
		}

		err := store.Write(urlPath, []byte(body.Content))
		if err != nil {
			writeFSWriteError(w, err, urlPath)
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

// isBinaryUpload reports whether a PUT carries a raw binary body rather than the
// JSON {content} envelope, based on the request Content-Type.
func isBinaryUpload(r *http.Request) bool {
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if ct == "" {
		return false
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct != "application/json" && ct != "text/json"
}

func handleBinaryPut(w http.ResponseWriter, r *http.Request, store *storage.Store, onContentChanged func(), urlPath string) {
	if !storage.IsImagePath(urlPath) {
		writeError(w, http.StatusBadRequest, storage.ErrUnsupportedBinary.Error(), urlPath)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "image exceeds maximum size", urlPath)
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read request body", urlPath)
		return
	}

	if err := store.WriteRaw(urlPath, content); err != nil {
		writeFSWriteError(w, err, urlPath)
		return
	}
	triggerContentChanged(onContentChanged)

	info, err := store.StatFile(urlPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// serveRawFile streams a file's bytes with an allowlisted Content-Type. SVGs are
// served with hardening headers so script-bearing SVGs cannot execute.
func serveRawFile(w http.ResponseWriter, r *http.Request, store *storage.Store, urlPath string) {
	fsPath, err := store.RawPath(urlPath)
	if err != nil {
		writeFSReadError(w, err, urlPath)
		return
	}
	f, err := os.Open(fsPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
		return
	}

	if ct, ok := storage.ImageContentType(urlPath); ok {
		w.Header().Set("Content-Type", ct)
		if ct == "image/svg+xml" {
			// Neutralize script-in-SVG: forbid execution and force download rather
			// than top-level rendering.
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
			w.Header().Set("Content-Disposition", "attachment")
		} else {
			w.Header().Set("Content-Disposition", "inline")
		}
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// writeFSReadError maps storage read errors to HTTP responses.
func writeFSReadError(w http.ResponseWriter, err error, urlPath string) {
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found", urlPath)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
}

// writeFSWriteError maps storage write errors to HTTP responses.
func writeFSWriteError(w http.ResponseWriter, err error, urlPath string) {
	switch {
	case errors.Is(err, storage.ErrIsJournal),
		errors.Is(err, storage.ErrPathTraversal),
		errors.Is(err, storage.ErrUnsupportedBinary),
		errors.Is(err, markdown.ErrNullBytes),
		errors.Is(err, markdown.ErrBinary):
		writeError(w, http.StatusBadRequest, err.Error(), urlPath)
	default:
		writeError(w, http.StatusInternalServerError, err.Error(), urlPath)
	}
}

func handleFSDelete(store *storage.Store, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := extractPath(r)

		var err error
		if r.URL.Query().Get("recursive") == "true" {
			err = store.DeleteRecursive(urlPath)
		} else {
			err = store.Delete(urlPath)
		}
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

func handleFSPost(store *storage.Store, onContentChanged func()) http.HandlerFunc {
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
		triggerContentChanged(onContentChanged)

		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "path": urlPath})
	}
}
