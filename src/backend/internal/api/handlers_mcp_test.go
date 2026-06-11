package api

import "testing"

func TestMCPStructuredContentPassesObjectsThrough(t *testing.T) {
	got := mcpStructuredContent(map[string]any{
		"status": "ok",
		"path":   "notes/a.md",
	})

	if got["status"] != "ok" {
		t.Fatalf("status = %v, want ok", got["status"])
	}
	if got["path"] != "notes/a.md" {
		t.Fatalf("path = %v, want notes/a.md", got["path"])
	}
}

func TestMCPStructuredContentWrapsArrays(t *testing.T) {
	got := mcpStructuredContent([]map[string]any{
		{"path": "notes/a.md"},
		{"path": "notes/b.md"},
	})

	results, ok := got["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", got["results"])
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if got["total"] != 2 {
		t.Fatalf("total = %v, want 2", got["total"])
	}
}
