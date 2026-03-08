package api

import (
	"strings"
	"testing"

	"duffel/src/backend/internal/search"
)

func TestBuildRecommendationQueryStripsFrontMatterAndCapsBody(t *testing.T) {
	longBody := strings.Repeat("token ", recommendationMaxQueryRunes+300)
	content := "---\n" +
		"title: Hello\n" +
		"tags: [a,b]\n" +
		"---\n\n" +
		longBody

	title, query := buildRecommendationQuery("notes/hello.md", content)
	if title != "hello" {
		t.Fatalf("title = %q, want %q", title, "hello")
	}
	if strings.Contains(query, "tags: [a,b]") {
		t.Fatalf("query should not include front matter: %q", query)
	}
	if !strings.HasPrefix(query, "hello token") {
		t.Fatalf("query prefix = %q, want to start with title+body", query[:min(len(query), 20)])
	}

	bodyOnly := strings.TrimPrefix(query, "hello ")
	if got := len([]rune(bodyOnly)); got != recommendationMaxQueryRunes {
		t.Fatalf("body rune count = %d, want %d", got, recommendationMaxQueryRunes)
	}
}

func TestPostProcessRecommendationsFiltersSelfDedupeAndLimit(t *testing.T) {
	results := []search.Result{
		{Path: "notes/self.md", Title: "self"},
		{Path: "notes/a.md", Title: "A"},
		{Path: "./notes/a.md", Title: "A dup"},
		{Path: "notes/b.md", Title: "B"},
		{Path: "notes/c.md", Title: "C"},
		{Path: "notes/d.md", Title: "D"},
		{Path: "notes/e.md", Title: "E"},
		{Path: "notes/f.md", Title: "F"},
	}

	filtered := postProcessRecommendations("notes/self.md", results, 5)
	if len(filtered) != 5 {
		t.Fatalf("len(filtered) = %d, want 5", len(filtered))
	}

	want := []string{"notes/a.md", "notes/b.md", "notes/c.md", "notes/d.md", "notes/e.md"}
	for i, got := range filtered {
		if got.Path != want[i] {
			t.Fatalf("filtered[%d].Path = %q, want %q", i, got.Path, want[i])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
