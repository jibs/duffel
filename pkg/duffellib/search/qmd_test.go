package search

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMapPaths(t *testing.T) {
	storeRoot := "/tmp/duffel-data"

	results := []Result{
		{Path: "/tmp/duffel-data/notes/hello.md", Title: "Hello", Snippet: "world", Score: -1.5, ModifiedAt: "2026-01-01T00:00:00Z"},
		{Path: "/tmp/duffel-data/journal/2024-01-01.md", Title: "Journal", Snippet: "entry", Score: -1.2},
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
		SkipRerank:     true,
	}
	got := buildHybridArgs(opts, 24)
	want := []string{"query", "intent: software runtime\nlex: performance tuning\nvec: performance tuning", "--json", "-n", "24", "-c", "duffel", "-C", "60", "--min-score", "0.2", "--explain", "--no-rerank"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildHybridQueryDocumentDefaults(t *testing.T) {
	got := buildHybridQueryDocument(SearchOptions{Query: "  open   tasks\n"})
	want := "lex: open   tasks\nvec: open   tasks"
	if got != want {
		t.Fatalf("query document = %q, want %q", got, want)
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
		{
			name: "hyphenated identifier",
			in:   "duffel-mcp-update-check-20260327",
			want: `"duffel-mcp-update-check-20260327"`,
		},
		{
			name: "mixed operator and literal token",
			in:   "status OR duffel-mcp-update-check-20260327",
			want: `status OR "duffel-mcp-update-check-20260327"`,
		},
		{
			name: "quoted phrase is preserved",
			in:   `"exact phrase" marker`,
			want: `"exact phrase" marker`,
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

func TestShouldBypassHybridForSingleLiteralIdentifier(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{query: "duffel-mcp-update-check-20260327", want: true},
		{query: `"exact phrase"`, want: true},
		{query: "performance", want: false},
		{query: "status update", want: false},
	}

	for _, tt := range tests {
		if got := shouldBypassHybrid(tt.query); got != tt.want {
			t.Fatalf("shouldBypassHybrid(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestParseHybridResults(t *testing.T) {
	raw := []byte(`[
	  {
	    "file": "qmd://duffel/notes/alpha.md",
	    "title": "Alpha",
	    "snippet": "...",
	    "score": 0.91,
	    "line": 42,
	    "context": "Project notes",
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
	if results[0].Line != 42 {
		t.Fatalf("line = %d, want 42", results[0].Line)
	}
	if results[0].Context != "Project notes" {
		t.Fatalf("context = %q, want Project notes", results[0].Context)
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

func TestSearchRequireHybridReturnsHybridError(t *testing.T) {
	db := mustOpenTestDB(t)
	defer db.Close()

	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
			return nil, []byte("boom"), errors.New("hybrid failed")
		},
		queryTimeout: time.Second,
	}

	_, err := s.Search(SearchOptions{
		Query:         "performance",
		Collection:    "duffel",
		Limit:         5,
		RequireHybrid: true,
	})
	if err == nil {
		t.Fatal("Search error = nil, want hybrid error")
	}
	var hybridErr *HybridSearchError
	if !errors.As(err, &hybridErr) {
		t.Fatalf("error type = %T, want *HybridSearchError", err)
	}
}

func TestRepairQmdCollectionNameRenamesStaleCollection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbDir := filepath.Join(home, ".cache", "qmd")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dbDir, "index.sqlite"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	mustExec(t, db, `
		CREATE TABLE store_collections (
			name TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			pattern TEXT NOT NULL DEFAULT '**/*.md'
		);
	`)
	mustExec(t, db, `
		CREATE TABLE documents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection TEXT NOT NULL,
			path TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			UNIQUE(collection, path)
		);
	`)
	mustExec(t, db, `
		INSERT INTO store_collections(name, path, pattern)
		VALUES ('0', '/data', '**/*.md');
	`)
	mustExec(t, db, `
		INSERT INTO documents(collection, path, active) VALUES
		('0', 'notes/a.md', 1),
		('0', 'notes/b.md', 1),
		('duffel', 'notes/b.md', 1);
	`)

	if err := repairQmdCollectionName("duffel", "/data", "**/*.md"); err != nil {
		t.Fatalf("repairQmdCollectionName error = %v", err)
	}

	var collectionName string
	if err := db.QueryRow(`SELECT name FROM store_collections WHERE path = '/data'`).Scan(&collectionName); err != nil {
		t.Fatalf("select collection name: %v", err)
	}
	if collectionName != "duffel" {
		t.Fatalf("collection name = %q, want duffel", collectionName)
	}

	var staleCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents WHERE collection = '0'`).Scan(&staleCount); err != nil {
		t.Fatalf("select stale count: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("stale document count = %d, want 0", staleCount)
	}

	var activeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents WHERE collection = 'duffel'`).Scan(&activeCount); err != nil {
		t.Fatalf("select active count: %v", err)
	}
	if activeCount != 2 {
		t.Fatalf("duffel document count = %d, want 2", activeCount)
	}
}

func TestEnsureQmdConfigCollectionRepairsLegacyListConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".config", "qmd", "index.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("collections:\n  - null\n"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	if err := ensureQmdConfigCollection("duffel", "/data", collectionMask); err != nil {
		t.Fatalf("ensureQmdConfigCollection error = %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	want := "collections:\n  duffel:\n    path: /data\n    pattern: \"**/*.{md,html}\"\n"
	if string(got) != want {
		t.Fatalf("config = %q, want %q", string(got), want)
	}
}

func TestEnsureQmdConfigCollectionUpdatesExistingMask(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".config", "qmd", "index.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	original := "collections:\n  other:\n    path: /other\n    pattern: \"**/*.md\"\n  duffel:\n    path: /data\n    pattern: \"**/*.md\"\n  archive:\n    path: /archive\n    pattern: \"**/*.md\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := ensureQmdConfigCollection("duffel", "/data", collectionMask); err != nil {
		t.Fatalf("ensureQmdConfigCollection error = %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	want := "collections:\n  other:\n    path: /other\n    pattern: \"**/*.md\"\n  duffel:\n    path: /data\n    pattern: \"**/*.{md,html}\"\n  archive:\n    path: /archive\n    pattern: \"**/*.md\"\n"
	if string(got) != want {
		t.Fatalf("config = %q, want %q", string(got), want)
	}
}

func TestSearchFallsBackToBM25ForHyphenatedLiteralQuery(t *testing.T) {
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
		VALUES (1, 'duffel', 'travel/mcp-smoke-test.md', 'Smoke Test', '2026-03-28T00:00:00Z', 1);
	`)
	mustExec(t, db, `
		INSERT INTO documents_fts(rowid, filepath, title, body)
		VALUES (
			1,
			'duffel/travel/mcp-smoke-test.md',
			'Smoke Test',
			'updated_marker: duffel-mcp-update-check-20260327'
		);
	`)

	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
			return nil, []byte("boom"), errors.New("hybrid failed")
		},
		queryTimeout: time.Second,
	}

	results, err := s.Search(SearchOptions{
		Query:      "duffel-mcp-update-check-20260327",
		Collection: "duffel",
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Path != "travel/mcp-smoke-test.md" {
		t.Fatalf("path = %q, want travel/mcp-smoke-test.md", results[0].Path)
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

func TestSearchFallsBackToBM25WhenHybridQueueIsBusy(t *testing.T) {
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

	releaseHybrid := make(chan struct{})
	firstHybridStarted := make(chan struct{})
	var hybridCalls atomic.Int32

	s := &Searcher{
		db:      db,
		findQmd: func() (string, error) { return "/usr/bin/qmd", nil },
		runQmd: func(_ context.Context, _ string, _ []string) ([]byte, []byte, error) {
			if hybridCalls.Add(1) == 1 {
				close(firstHybridStarted)
			}
			<-releaseHybrid
			return []byte(`[
				{"file":"qmd://duffel/notes/perf.md","title":"Perf","snippet":"hybrid result","score":0.9}
			]`), nil, nil
		},
		queryTimeout: 25 * time.Millisecond,
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		if _, err := s.Search(SearchOptions{Query: "performance", Collection: "duffel", Limit: 1}); err != nil {
			t.Errorf("first Search error = %v", err)
		}
	}()

	select {
	case <-firstHybridStarted:
	case <-time.After(time.Second):
		t.Fatal("first hybrid search did not start")
	}

	start := time.Now()
	results, err := s.Search(SearchOptions{Query: "performance", Collection: "duffel", Limit: 1})
	if err != nil {
		t.Fatalf("second Search error = %v", err)
	}
	if hybridCalls.Load() != 1 {
		t.Fatalf("hybridCalls = %d, want 1 while queue is busy", hybridCalls.Load())
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Path != "notes/perf.md" {
		t.Fatalf("path = %q, want notes/perf.md", results[0].Path)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("busy fallback took %v, want under 1s", elapsed)
	}

	close(releaseHybrid)

	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first Search did not finish after releasing hybrid query")
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
