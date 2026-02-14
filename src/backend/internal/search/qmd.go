package search

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Result represents a single search result from the qmd FTS index.
type Result struct {
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
	ModifiedAt string  `json:"modified_at"`
}

// SearchOptions configures a search query.
type SearchOptions struct {
	Query      string
	Collection string
	Limit      int
	Offset     int
	Prefix     string
	Sort       string // "score" or "date"
	After      string // ISO date, e.g. "2026-01-01"
	Before     string // ISO date, e.g. "2026-12-31"
}

// Searcher queries the qmd SQLite/FTS5 database directly.
type Searcher struct {
	db *sql.DB
}

// NewSearcher opens the qmd index database read-only.
// Returns an error if the database file doesn't exist (qmd hasn't indexed yet).
func NewSearcher() (*Searcher, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	dbPath := filepath.Join(home, ".cache", "qmd", "index.sqlite")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("qmd database not found at %s — run qmd to index your files first", dbPath)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open qmd database: %w", err)
	}

	// Verify the DB is readable
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("qmd database not accessible: %w", err)
	}

	return &Searcher{db: db}, nil
}

// Search runs an FTS5 BM25 query against the qmd index.
func (s *Searcher) Search(opts SearchOptions) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}
	if opts.Sort == "" {
		opts.Sort = "score"
	}

	query := `
		SELECT d.path, d.title,
		       snippet(documents_fts, 2, '<mark>', '</mark>', '...', 32) as snippet,
		       bm25(documents_fts, 5.0, 10.0, 1.0) as score,
		       COALESCE(d.modified_at, '') as modified_at
		FROM documents_fts
		JOIN documents d ON d.rowid = documents_fts.rowid
		WHERE documents_fts MATCH ?
		  AND d.collection = ?
		  AND d.active = 1`
	args := []any{opts.Query, opts.Collection}

	if opts.Prefix != "" {
		query += "\n		  AND d.path LIKE ? || '%'"
		args = append(args, opts.Prefix)
	}
	if opts.After != "" {
		query += "\n		  AND d.modified_at >= ?"
		args = append(args, opts.After)
	}
	if opts.Before != "" {
		query += "\n		  AND d.modified_at < ?"
		args = append(args, opts.Before)
	}

	if opts.Sort == "date" {
		query += "\n		ORDER BY d.modified_at DESC"
	} else {
		query += "\n		ORDER BY score ASC"
	}

	query += "\n		LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Path, &r.Title, &r.Snippet, &r.Score, &r.ModifiedAt); err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return results, nil
}

// findQmd locates the qmd binary, checking ./node_modules/qmd/qmd first,
// then falling back to PATH lookup.
func findQmd() (string, error) {
	// Check node_modules (installed via npm/pnpm)
	local := filepath.Join("node_modules", "qmd", "qmd")
	if abs, err := filepath.Abs(local); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	// Fall back to PATH
	return exec.LookPath("qmd")
}

// StartIndexing runs `qmd update` in the background to re-index all collections.
// It returns immediately. The onDone callback is called when indexing finishes.
func StartIndexing(collection string, onDone func(error)) error {
	qmdPath, err := findQmd()
	if err != nil {
		return fmt.Errorf("qmd not found (checked ./node_modules/qmd/qmd and PATH): %w", err)
	}

	cmd := exec.Command(qmdPath, "update")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start qmd update: %w", err)
	}

	go func() {
		onDone(cmd.Wait())
	}()

	return nil
}

// Close closes the database connection.
func (s *Searcher) Close() error {
	return s.db.Close()
}

// MapPaths strips the storeRoot prefix from each result's Path to produce
// duffel-relative URL paths.
func MapPaths(results []Result, storeRoot string) []Result {
	root := filepath.Clean(storeRoot)
	mapped := make([]Result, len(results))
	for i, r := range results {
		mapped[i] = r
		clean := filepath.Clean(r.Path)
		if rel, ok := strings.CutPrefix(clean, root); ok {
			mapped[i].Path = strings.TrimPrefix(rel, string(filepath.Separator))
		}
	}
	return mapped
}

// EnsureCollection uses `qmd collection add` to ensure a collection exists
// pointing at dataDir with pattern "**/*.md". If the collection already exists,
// qmd handles it as a no-op or update.
func EnsureCollection(name, dataDir string) error {
	qmdPath, err := findQmd()
	if err != nil {
		return fmt.Errorf("qmd not found (checked ./node_modules/qmd/qmd and PATH): %w", err)
	}

	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("failed to resolve data dir: %w", err)
	}

	cmd := exec.Command(qmdPath, "collection", "add", absDataDir, "--name", name, "--mask", "**/*.md")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "already exists" — the collection is already configured correctly
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("qmd collection add failed: %w\n%s", err, out)
	}

	return nil
}
