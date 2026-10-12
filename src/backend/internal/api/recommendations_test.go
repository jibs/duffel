package api

import (
	"strings"
	"testing"
	"time"

	"duffel/pkg/duffellib/search"
	"duffel/pkg/duffellib/storage"
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

func TestRecommendationSearchOptionsRequireFastQmdHybrid(t *testing.T) {
	title, query := buildRecommendationQuery("notes/perf.md", "# Perf\n\nRuntime tuning")
	opts := search.SearchOptions{
		Query:          buildRecommendationQmdQuery(query),
		Collection:     "duffel",
		Limit:          recommendationFetchLimit,
		CandidateLimit: recommendationCandidateLimit,
		Intent:         recommendationIntent(title),
		RequireHybrid:  true,
		SkipRerank:     true,
	}

	if !opts.RequireHybrid {
		t.Fatal("RequireHybrid = false, recommendations would silently fall back to BM25")
	}
	if !opts.SkipRerank {
		t.Fatal("SkipRerank = false, recommendations would block on reranking")
	}
	if opts.CandidateLimit != recommendationCandidateLimit {
		t.Fatalf("CandidateLimit = %d, want %d", opts.CandidateLimit, recommendationCandidateLimit)
	}
	if !strings.HasPrefix(opts.Query, "lex: ") {
		t.Fatalf("Query = %q, want qmd lex query document", opts.Query)
	}
}

func TestRecommendationCacheReturnsMaterializedRecommendations(t *testing.T) {
	modTime := time.Now()
	cache := NewRecommendationCache()
	cache.entries["notes/current.md"] = recommendationCacheEntry{
		modTime: modTime,
		results: []search.Result{
			{Path: "notes/related.md", Title: "Related"},
		},
	}

	got := cache.ForFile(&storage.FileInfo{
		Path:    "notes/current.md",
		ModTime: modTime,
	})
	if len(got) != 1 || got[0].Path != "notes/related.md" {
		t.Fatalf("ForFile returned %#v, want cached recommendation", got)
	}

	got[0].Path = "mutated.md"
	again := cache.ForFile(&storage.FileInfo{
		Path:    "notes/current.md",
		ModTime: modTime,
	})
	if again[0].Path != "notes/related.md" {
		t.Fatalf("ForFile exposed mutable cache result: %#v", again)
	}
}

func TestRecommendationCacheNormalizesAbsolutePaths(t *testing.T) {
	modTime := time.Now()
	cache := NewRecommendationCache()
	cache.entries["notes/current.md"] = recommendationCacheEntry{
		modTime: modTime,
		results: []search.Result{
			{Path: "notes/related.md", Title: "Related"},
		},
	}

	got := cache.ForFile(&storage.FileInfo{
		Path:    "/notes/current.md",
		ModTime: modTime,
	})
	if len(got) != 1 || got[0].Path != "notes/related.md" {
		t.Fatalf("ForFile returned %#v, want cached recommendation for absolute path", got)
	}

	cache.Delete("/notes/current.md")
	got = cache.ForFile(&storage.FileInfo{
		Path:    "notes/current.md",
		ModTime: modTime,
	})
	if len(got) != 0 {
		t.Fatalf("ForFile returned %#v after absolute delete, want empty", got)
	}
}

func TestRecommendationCacheReturnsEmptyForStaleEntry(t *testing.T) {
	cache := NewRecommendationCache()
	cache.entries["notes/current.md"] = recommendationCacheEntry{
		modTime: time.Now().Add(-time.Minute),
		results: []search.Result{
			{Path: "notes/related.md", Title: "Related"},
		},
	}

	got := cache.ForFile(&storage.FileInfo{
		Path:    "notes/current.md",
		ModTime: time.Now(),
	})
	if len(got) != 0 {
		t.Fatalf("ForFile returned %#v, want empty stale result", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
