package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"duffel/pkg/duffellib/storage"
)

const (
	illustrationVersion  = 1
	maxIllustrationBytes = 10 << 20 // 10 MiB
)

type illustrationDocument struct {
	Version  int             `json:"version"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
	Mermaid  string          `json:"mermaid,omitempty"`
}

type illustrationResponse struct {
	Path      string                `json:"path"`
	Exists    bool                  `json:"exists"`
	Revision  string                `json:"revision,omitempty"`
	UpdatedAt time.Time             `json:"updatedAt,omitempty"`
	Document  *illustrationDocument `json:"document,omitempty"`
}

func canonicalNotePath(raw string) (string, error) {
	trimmed := strings.Trim(raw, "/")
	if trimmed == "" {
		return "", storage.ErrPathTraversal
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+trimmed), "/")
	if cleaned != trimmed || cleaned == "." || cleaned == ".duffel" || strings.HasPrefix(cleaned, ".duffel/") {
		return "", storage.ErrPathTraversal
	}
	return cleaned, nil
}

func illustrationPath(notePath string) (string, error) {
	canonical, err := canonicalNotePath(notePath)
	if err != nil {
		return "", err
	}
	return ".duffel/illustrations/" + canonical + ".json", nil
}

func ensureIllustratableNote(store *storage.Store, notePath string) error {
	canonical, err := canonicalNotePath(notePath)
	if err != nil {
		return err
	}
	info, err := store.Read(canonical)
	if err != nil {
		return err
	}
	if info.Kind == "image" {
		return fmt.Errorf("illustrations can only be attached to text notes")
	}
	return nil
}

func readIllustration(store *storage.Store, notePath string) (*illustrationResponse, error) {
	canonical, err := canonicalNotePath(notePath)
	if err != nil {
		return nil, err
	}
	if err := ensureIllustratableNote(store, canonical); err != nil {
		return nil, err
	}
	sidecar, err := illustrationPath(canonical)
	if err != nil {
		return nil, err
	}
	file, err := store.Read(sidecar)
	if errors.Is(err, storage.ErrNotFound) {
		return &illustrationResponse{Path: canonical, Exists: false, Revision: "none"}, nil
	}
	if err != nil {
		return nil, err
	}
	var document illustrationDocument
	if err := json.Unmarshal([]byte(file.Content), &document); err != nil {
		return nil, fmt.Errorf("invalid illustration data: %w", err)
	}
	if err := validateIllustrationDocument(&document); err != nil {
		return nil, err
	}
	return &illustrationResponse{
		Path:      canonical,
		Exists:    true,
		Revision:  illustrationRevision(file.ModTime, file.Size),
		UpdatedAt: file.ModTime,
		Document:  &document,
	}, nil
}

func writeIllustration(store *storage.Store, notePath string, document illustrationDocument, expectedRevision string) (*illustrationResponse, error) {
	canonical, err := canonicalNotePath(notePath)
	if err != nil {
		return nil, err
	}
	if err := ensureIllustratableNote(store, canonical); err != nil {
		return nil, err
	}
	if err := validateIllustrationDocument(&document); err != nil {
		return nil, err
	}
	sidecar, err := illustrationPath(canonical)
	if err != nil {
		return nil, err
	}
	if expectedRevision != "" {
		current, err := store.Read(sidecar)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		currentRevision := "none"
		if current != nil {
			currentRevision = illustrationRevision(current.ModTime, current.Size)
		}
		if currentRevision != expectedRevision {
			return nil, errIllustrationConflict
		}
	}
	content, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	if len(content) > maxIllustrationBytes {
		return nil, errIllustrationTooLarge
	}
	if err := store.Write(sidecar, content); err != nil {
		return nil, err
	}
	return readIllustration(store, canonical)
}

func deleteIllustration(store *storage.Store, notePath string) error {
	sidecar, err := illustrationPath(notePath)
	if err != nil {
		return err
	}
	err = store.Delete(sidecar)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	return err
}

func validateIllustrationDocument(document *illustrationDocument) error {
	if document.Version != illustrationVersion {
		return fmt.Errorf("illustration version must be %d", illustrationVersion)
	}
	if len(document.Snapshot) == 0 && strings.TrimSpace(document.Mermaid) == "" {
		return errors.New("illustration requires a snapshot or Mermaid source")
	}
	if len(document.Snapshot) > 0 {
		var snapshot any
		if err := json.Unmarshal(document.Snapshot, &snapshot); err != nil {
			return errors.New("snapshot must be valid JSON")
		}
		if snapshot == nil {
			return errors.New("snapshot must not be null")
		}
	}
	return nil
}

func illustrationRevision(modTime time.Time, size int64) string {
	return fmt.Sprintf("%d-%d", modTime.UnixNano(), size)
}

var (
	errIllustrationConflict = errors.New("illustration changed since it was opened")
	errIllustrationTooLarge = errors.New("illustration exceeds maximum size")
)

func handleIllustration(store *storage.Store, onContentChanged ContentChangeNotifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notePath := extractPath(r)
		switch r.Method {
		case http.MethodGet:
			result, err := readIllustration(store, notePath)
			if err != nil {
				writeIllustrationError(w, err, notePath)
				return
			}
			writeJSON(w, http.StatusOK, result)
		case http.MethodPut:
			r.Body = http.MaxBytesReader(w, r.Body, maxIllustrationBytes)
			var document illustrationDocument
			if err := json.NewDecoder(r.Body).Decode(&document); err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					writeError(w, http.StatusRequestEntityTooLarge, errIllustrationTooLarge.Error(), notePath)
					return
				}
				writeError(w, http.StatusBadRequest, "invalid JSON body", notePath)
				return
			}
			result, err := writeIllustration(store, notePath, document, strings.TrimSpace(r.Header.Get("If-Match")))
			if err != nil {
				writeIllustrationError(w, err, notePath)
				return
			}
			triggerContentChanged(onContentChanged, notePath)
			writeJSON(w, http.StatusOK, result)
		case http.MethodDelete:
			if err := deleteIllustration(store, notePath); err != nil {
				writeIllustrationError(w, err, notePath)
				return
			}
			triggerContentChanged(onContentChanged, notePath)
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "path": notePath})
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleIllustrationConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"licenseKey": strings.TrimSpace(os.Getenv("DUFFEL_TLDRAW_LICENSE_KEY")),
		})
	}
}

func writeIllustrationError(w http.ResponseWriter, err error, notePath string) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "note not found", notePath)
	case errors.Is(err, storage.ErrPathTraversal):
		writeError(w, http.StatusBadRequest, err.Error(), notePath)
	case errors.Is(err, errIllustrationConflict):
		writeError(w, http.StatusConflict, err.Error(), notePath)
	case errors.Is(err, errIllustrationTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error(), notePath)
	case errors.Is(err, fs.ErrPermission):
		writeError(w, http.StatusForbidden, err.Error(), notePath)
	default:
		writeError(w, http.StatusBadRequest, err.Error(), notePath)
	}
}

func moveIllustration(store *storage.Store, sourcePath string, destinationPath string) error {
	source, err := illustrationPath(sourcePath)
	if err != nil {
		return err
	}
	destination, err := illustrationPath(destinationPath)
	if err != nil {
		return err
	}
	if _, err := store.Read(source); err == nil {
		return store.Move(source, destination)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	sourceCanonical, err := canonicalNotePath(sourcePath)
	if err != nil {
		return err
	}
	destinationCanonical, err := canonicalNotePath(destinationPath)
	if err != nil {
		return err
	}
	sourceDir := ".duffel/illustrations/" + sourceCanonical
	destinationDir := ".duffel/illustrations/" + destinationCanonical
	if _, err := store.List(sourceDir, false); errors.Is(err, storage.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	return store.Move(sourceDir, destinationDir)
}

func deleteIllustrationsForPath(store *storage.Store, notePath string, recursive bool) error {
	canonical, err := canonicalNotePath(notePath)
	if err != nil {
		return err
	}
	if recursive {
		sidecarDir := ".duffel/illustrations/" + canonical
		if err := store.DeleteRecursive(sidecarDir); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
	}
	return deleteIllustration(store, canonical)
}
