package api

import (
	"strings"
	"testing"

	"duffel/pkg/duffellib/search"
	"duffel/pkg/duffellib/storage"
)

func TestIllustrationPathRejectsTraversalAndInternalPaths(t *testing.T) {
	for _, candidate := range []string{"", "../note.md", "notes/../note.md", ".duffel/secret.md"} {
		if _, err := illustrationPath(candidate); err == nil {
			t.Errorf("illustrationPath(%q) succeeded, want error", candidate)
		}
	}
}

func TestMCPDiagramSetAndRead(t *testing.T) {
	store, err := storage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write("notes/plan.md", []byte("# Plan")); err != nil {
		t.Fatal(err)
	}
	var changed []string
	notify := func(paths ...string) { changed = append(changed, paths...) }
	mermaid := "flowchart LR\n  Human --> Duffel\n  Agent --> Duffel"

	result, err := runMCPTool(store, func() *search.Searcher { return nil }, notify, NewRecommendationCache(), mcpToolCallParams{
		Name: "duffel_diagram_set",
		Arguments: map[string]any{
			"path":    "notes/plan.md",
			"mermaid": mermaid,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	written, ok := result.(*illustrationResponse)
	if !ok || !written.Exists {
		t.Fatalf("result = %#v, want existing illustration", result)
	}
	if written.Document == nil || strings.TrimSpace(written.Document.Mermaid) != mermaid {
		t.Fatalf("document = %#v, want Mermaid source", written.Document)
	}
	if len(changed) != 1 || changed[0] != "notes/plan.md" {
		t.Fatalf("changed paths = %#v", changed)
	}

	result, err = runMCPTool(store, func() *search.Searcher { return nil }, notify, NewRecommendationCache(), mcpToolCallParams{
		Name:      "duffel_diagram_read",
		Arguments: map[string]any{"path": "notes/plan.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	read, ok := result.(*illustrationResponse)
	if !ok || read.Document == nil || read.Document.Mermaid != mermaid {
		t.Fatalf("read result = %#v", result)
	}
}
