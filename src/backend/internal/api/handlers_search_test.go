package api

import (
	"reflect"
	"testing"

	"duffel/src/backend/internal/search"
)

func TestParseSearchFields(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fields, err := parseSearchFields("")
		if err != nil {
			t.Fatalf("parseSearchFields returned error: %v", err)
		}
		if fields != nil {
			t.Fatalf("fields = %#v, want nil", fields)
		}
	})

	t.Run("valid with dedupe and spaces", func(t *testing.T) {
		fields, err := parseSearchFields(" path ,title,path, modified_at ")
		if err != nil {
			t.Fatalf("parseSearchFields returned error: %v", err)
		}
		want := []string{"path", "title", "modified_at"}
		if !reflect.DeepEqual(fields, want) {
			t.Fatalf("fields = %#v, want %#v", fields, want)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := parseSearchFields("path,bad")
		if err == nil {
			t.Fatal("parseSearchFields error = nil, want non-nil")
		}
	})
}

func TestProjectSearchResults(t *testing.T) {
	results := []search.Result{
		{
			Path:       "notes/a.md",
			Title:      "A",
			Snippet:    "snippet",
			Score:      1.23,
			ModifiedAt: "2026-02-20T10:00:00Z",
		},
	}

	got := projectSearchResults(results, []string{"path", "title"})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0]) != 2 {
		t.Fatalf("len(got[0]) = %d, want 2", len(got[0]))
	}
	if got[0]["path"] != "notes/a.md" {
		t.Fatalf("path = %v, want notes/a.md", got[0]["path"])
	}
	if got[0]["title"] != "A" {
		t.Fatalf("title = %v, want A", got[0]["title"])
	}
}
