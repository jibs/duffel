package api

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"

	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

var mcpPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03}

func TestMCPWriteImageBase64(t *testing.T) {
	store, err := storage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = runMCPTool(store, func() *search.Searcher { return nil }, func() {}, mcpToolCallParams{
		Name: "duffel_write",
		Arguments: map[string]any{
			"path":     "_attachments/pic.png",
			"content":  base64.StdEncoding.EncodeToString(mcpPNG),
			"encoding": "base64",
		},
	})
	if err != nil {
		t.Fatalf("duffel_write base64: %v", err)
	}

	fsPath, err := store.RawPath("_attachments/pic.png")
	if err != nil {
		t.Fatalf("RawPath: %v", err)
	}
	got, err := os.ReadFile(fsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, mcpPNG) {
		t.Errorf("written bytes differ from base64 input")
	}
}

func TestMCPWriteImageBase64RejectsNonImage(t *testing.T) {
	store, err := storage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = runMCPTool(store, func() *search.Searcher { return nil }, func() {}, mcpToolCallParams{
		Name: "duffel_write",
		Arguments: map[string]any{
			"path":     "notes/evil.md",
			"content":  base64.StdEncoding.EncodeToString(mcpPNG),
			"encoding": "base64",
		},
	})
	if err == nil {
		t.Fatal("expected error writing binary to a non-image extension")
	}
}

func TestMCPReadImageReturnsMetadata(t *testing.T) {
	store, err := storage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteRaw("_attachments/pic.png", mcpPNG); err != nil {
		t.Fatal(err)
	}

	out, err := runMCPTool(store, func() *search.Searcher { return nil }, func() {}, mcpToolCallParams{
		Name:      "duffel_read",
		Arguments: map[string]any{"path": "_attachments/pic.png"},
	})
	if err != nil {
		t.Fatalf("duffel_read: %v", err)
	}
	resp, ok := out.(fileGetResponse)
	if !ok {
		t.Fatalf("response type = %T, want fileGetResponse", out)
	}
	if resp.Kind != "image" {
		t.Errorf("kind = %q, want image", resp.Kind)
	}
	if resp.Content != "" {
		t.Errorf("content = %q, want empty for image", resp.Content)
	}
}
