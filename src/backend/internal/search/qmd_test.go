package search

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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

func TestMapPathsQmdURI(t *testing.T) {
	results := []Result{{Path: "qmd://duffel/notes/a.md", Title: "A"}}
	mapped := MapPaths(results, "/unused")
	if mapped[0].Path != "notes/a.md" {
		t.Fatalf("got %q, want %q", mapped[0].Path, "notes/a.md")
	}
}

func TestMapPathsEmpty(t *testing.T) {
	mapped := MapPaths(nil, "/data")
	if len(mapped) != 0 {
		t.Errorf("got %d results, want 0", len(mapped))
	}
}

func TestBuildHybridArgs(t *testing.T) {
	opts := SearchOptions{
		Query:          "performance tuning",
		Collection:     "duffel",
		Intent:         "software runtime",
		CandidateLimit: 60,
		MinScore:       0.2,
		Explain:        true,
	}
	got := buildHybridArgs(opts, 24)
	want := []string{"query", "performance tuning", "--json", "-n", "24", "-c", "duffel", "--intent", "software runtime", "-C", "60", "--min-score", "0.2", "--explain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildBM25MatchQueryPipeAlternatives(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "escaped pipe",
			in:   `cheque-security-features\|cheque-date-validity`,
			want: `"cheque-security-features" OR "cheque-date-validity"`,
		},
		{
			name: "plain pipe",
			in:   "alpha|beta",
			want: `"alpha" OR "beta"`,
		},
		{
			name: "no pipe",
			in:   "single query",
			want: "single query",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildBM25MatchQuery(tt.in); got != tt.want {
				t.Fatalf("buildBM25MatchQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseHybridResults(t *testing.T) {
	raw := []byte(`[
	  {
	    "file": "qmd://duffel/notes/alpha.md",
	    "title": "Alpha",
	    "snippet": "...",
	    "score": 0.91,
	    "explain": {"rrf": {"rank": 1}}
	  }
	]`)

	results, err := parseHybridResults(raw, "duffel")
	if err != nil {
		t.Fatalf("parseHybridResults error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Path != "notes/alpha.md" {
		t.Fatalf("path = %q, want notes/alpha.md", results[0].Path)
	}
	if string(results[0].Explain) != `{"rrf":{"rank":1}}` {
		t.Fatalf("explain = %s, want compact JSON", string(results[0].Explain))
	}
}

func TestSearchHybridOffsetAndModifiedAt(t *testing.T) {
	db := mustOpenTestDB(t)
	defer db.Close()

	mustExec(t, db, `
		CREATE TABLE documents (
			rowid INTEGER PRIMARY KEY,
			collection TEXT,
			path TEXT,
			title TEXT,
			modified_at TEXT,
			active INTEGER
		);
	`)
	mustExec(t, db, `
		INSERT INTO documents(rowid, collection, path, title, modified_at, active) VALUES
		(1, 'duffel', 'notes/a.md', 'A', '2026-03-07T00:00:00Z', 1),
		(2, 'duffel', 'notes/b.md', 'B', '2026-03-08T00:00:00Z', 1);
	`)

	var capturedArgs []string
	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, args []string) ([]byte, []byte, error) {
			capturedArgs = append([]string(nil), args...)
			return []byte(`[
				{"file":"qmd://duffel/notes/a.md","title":"A","snippet":"first","score":0.9},
				{"file":"qmd://duffel/notes/b.md","title":"B","snippet":"second","score":0.8}
			]`), nil, nil
		},
		queryTimeout: time.Second,
	}

	results, err := s.Search(SearchOptions{Query: "notes", Collection: "duffel", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Path != "notes/b.md" {
		t.Fatalf("path = %q, want notes/b.md", results[0].Path)
	}
	if results[0].ModifiedAt != "2026-03-08T00:00:00Z" {
		t.Fatalf("modified_at = %q, want 2026-03-08T00:00:00Z", results[0].ModifiedAt)
	}

	if !containsArgPair(capturedArgs, "-n", "2") {
		t.Fatalf("expected over-fetch '-n 2' in args, got %#v", capturedArgs)
	}
}

func TestSearchFallsBackToBM25WhenHybridFails(t *testing.T) {
	db := mustOpenTestDB(t)
	defer db.Close()

	mustExec(t, db, `
		CREATE TABLE documents (
			rowid INTEGER PRIMARY KEY,
			collection TEXT,
			path TEXT,
			title TEXT,
			modified_at TEXT,
			active INTEGER
		);
	`)
	mustExec(t, db, `
		CREATE VIRTUAL TABLE documents_fts USING fts5(filepath, title, body);
	`)
	mustExec(t, db, `
		INSERT INTO documents(rowid, collection, path, title, modified_at, active)
		VALUES (1, 'duffel', 'notes/perf.md', 'Perf', '2026-03-08T00:00:00Z', 1);
	`)
	mustExec(t, db, `
		INSERT INTO documents_fts(rowid, filepath, title, body)
		VALUES (1, 'duffel/notes/perf.md', 'Perf', 'performance tuning guide');
	`)

	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
			return nil, []byte("boom"), errors.New("hybrid failed")
		},
		queryTimeout: time.Second,
	}

	results, err := s.Search(SearchOptions{Query: "performance", Collection: "duffel", Limit: 5})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Path != "notes/perf.md" {
		t.Fatalf("path = %q, want notes/perf.md", results[0].Path)
	}
}

func TestSearchBypassesHybridForPipeAlternatives(t *testing.T) {
	db := mustOpenTestDB(t)
	defer db.Close()

	mustExec(t, db, `
		CREATE TABLE documents (
			rowid INTEGER PRIMARY KEY,
			collection TEXT,
			path TEXT,
			title TEXT,
			modified_at TEXT,
			active INTEGER
		);
	`)
	mustExec(t, db, `
		CREATE VIRTUAL TABLE documents_fts USING fts5(filepath, title, body);
	`)
	mustExec(t, db, `
		INSERT INTO documents(rowid, collection, path, title, modified_at, active) VALUES
		(1, 'duffel', 'notes/a.md', 'A', '2026-03-08T00:00:00Z', 1),
		(2, 'duffel', 'notes/b.md', 'B', '2026-03-08T00:00:00Z', 1);
	`)
	mustExec(t, db, `
		INSERT INTO documents_fts(rowid, filepath, title, body) VALUES
		(1, 'duffel/notes/a.md', 'A', 'alpha token'),
		(2, 'duffel/notes/b.md', 'B', 'beta token');
	`)

	hybridCalled := false
	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
			hybridCalled = true
			return nil, nil, errors.New("hybrid should be bypassed")
		},
		queryTimeout: time.Second,
	}

	results, err := s.Search(SearchOptions{Query: `alpha\|beta`, Collection: "duffel", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if hybridCalled {
		t.Fatalf("expected hybrid to be bypassed for pipe alternation query")
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
}

func TestSearchDisableHybridSkipsHybrid(t *testing.T) {
	db := mustOpenTestDB(t)
	defer db.Close()

	mustExec(t, db, `
		CREATE TABLE documents (
			rowid INTEGER PRIMARY KEY,
			collection TEXT,
			path TEXT,
			title TEXT,
			modified_at TEXT,
			active INTEGER
		);
	`)
	mustExec(t, db, `
		CREATE VIRTUAL TABLE documents_fts USING fts5(filepath, title, body);
	`)
	mustExec(t, db, `
		INSERT INTO documents(rowid, collection, path, title, modified_at, active)
		VALUES (1, 'duffel', 'notes/perf.md', 'Perf', '2026-03-08T00:00:00Z', 1);
	`)
	mustExec(t, db, `
		INSERT INTO documents_fts(rowid, filepath, title, body)
		VALUES (1, 'duffel/notes/perf.md', 'Perf', 'performance tuning guide');
	`)

	hybridCalled := false
	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
			hybridCalled = true
			return nil, nil, errors.New("hybrid should be bypassed")
		},
		queryTimeout: time.Second,
	}

	results, err := s.Search(SearchOptions{
		Query:         "performance",
		Collection:    "duffel",
		Limit:         5,
		DisableHybrid: true,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if hybridCalled {
		t.Fatalf("expected hybrid to be disabled")
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Path != "notes/perf.md" {
		t.Fatalf("path = %q, want notes/perf.md", results[0].Path)
	}
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func mustOpenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("exec failed: %v\nquery:\n%s", err, query)
	}
}
