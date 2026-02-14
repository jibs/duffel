package search

import (
	"testing"
)

func TestMapPaths(t *testing.T) {
	storeRoot := "/home/user/duffel-data"

	results := []Result{
		{Path: "/home/user/duffel-data/notes/hello.md", Title: "Hello", Snippet: "world", Score: -1.5, ModifiedAt: "2026-01-01T00:00:00Z"},
		{Path: "/home/user/duffel-data/journal/2024-01-01.md", Title: "Journal", Snippet: "entry", Score: -1.2},
		{Path: "/other/path/file.md", Title: "Other", Snippet: "outside store", Score: -0.5},
	}

	mapped := MapPaths(results, storeRoot)

	if len(mapped) != 3 {
		t.Fatalf("got %d results, want 3", len(mapped))
	}

	if mapped[0].Path != "notes/hello.md" {
		t.Errorf("mapped[0].Path = %q, want %q", mapped[0].Path, "notes/hello.md")
	}
	if mapped[1].Path != "journal/2024-01-01.md" {
		t.Errorf("mapped[1].Path = %q, want %q", mapped[1].Path, "journal/2024-01-01.md")
	}
	// Path outside store root should remain unchanged
	if mapped[2].Path != "/other/path/file.md" {
		t.Errorf("mapped[2].Path = %q, want %q", mapped[2].Path, "/other/path/file.md")
	}

	// Verify other fields are preserved
	if mapped[0].Title != "Hello" {
		t.Errorf("title not preserved: got %q", mapped[0].Title)
	}
	if mapped[0].Score != -1.5 {
		t.Errorf("score not preserved: got %f", mapped[0].Score)
	}
	if mapped[0].ModifiedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("modified_at not preserved: got %q", mapped[0].ModifiedAt)
	}
}

func TestMapPathsTrailingSlash(t *testing.T) {
	results := []Result{
		{Path: "/data/notes/test.md", Title: "Test"},
	}

	mapped := MapPaths(results, "/data/")
	if mapped[0].Path != "notes/test.md" {
		t.Errorf("got %q, want %q", mapped[0].Path, "notes/test.md")
	}
}

func TestMapPathsEmpty(t *testing.T) {
	mapped := MapPaths(nil, "/data")
	if len(mapped) != 0 {
		t.Errorf("got %d results, want 0", len(mapped))
	}
}
