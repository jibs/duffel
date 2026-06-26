package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"duffel/pkg/duffellib/storage"

	"github.com/go-chi/chi/v5"
)

func TestHandleFSPutRejectsMismatchedImageContentType(t *testing.T) {
	store, err := storage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	router.Put("/api/fs/*", handleFSPut(store, func() {}))

	req := httptest.NewRequest(http.MethodPut, "/api/fs/_attachments/pic.png", bytes.NewReader([]byte("not a png")))
	req.Header.Set("Content-Type", "image/heic")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if _, err := store.RawPath("_attachments/pic.png"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("RawPath err = %v, want ErrNotFound", err)
	}
}

func TestImageUploadContentTypeMatchesAllowsAliases(t *testing.T) {
	cases := []struct {
		path        string
		contentType string
	}{
		{"_attachments/photo.jpg", "image/jpg"},
		{"_attachments/icon.ico", "image/vnd.microsoft.icon"},
		{"_attachments/bitmap.bmp", "image/x-ms-bmp"},
		{"_attachments/photo.png", "image/png; charset=binary"},
	}

	for _, tc := range cases {
		if !imageUploadContentTypeMatches(tc.path, tc.contentType) {
			t.Errorf("imageUploadContentTypeMatches(%q, %q) = false, want true", tc.path, tc.contentType)
		}
	}
}
