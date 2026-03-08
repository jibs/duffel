package search

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultCollection   = "duffel"
	defaultLimit        = 20
	maxLimit            = 100
	defaultQueryTimeout = 12 * time.Second
)

// Result represents a single search result from qmd.
type Result struct {
	Path       string          `json:"path"`
	Title      string          `json:"title"`
	Snippet    string          `json:"snippet"`
	Score      float64         `json:"score"`
	ModifiedAt string          `json:"modified_at"`
	Explain    json.RawMessage `json:"explain,omitempty"`
}

// SearchOptions configures a search query.
type SearchOptions struct {
	Query          string
	Collection     string
	Limit          int
	Offset         int
	Intent         string
	CandidateLimit int
	MinScore       float64
	Explain        bool
}

// HybridSearchError indicates why hybrid qmd query execution failed.
type HybridSearchError struct {
	Reason string
	Err    error
	Stderr string
}

func (e *HybridSearchError) Error() string {
	if e == nil {
		return "hybrid search failed"
	}
	msg := "hybrid search failed"
	if e.Reason != "" {
		msg += " (" + e.Reason + ")"
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if e.Stderr != "" {
		msg += "; stderr: " + e.Stderr
	}
	return msg
}

func (e *HybridSearchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Searcher queries qmd via CLI (hybrid) and falls back to direct SQLite BM25.
type Searcher struct {
	db           *sql.DB
	findQmd      func() (string, error)
	runQmd       func(ctx context.Context, qmdPath string, args []string) ([]byte, []byte, error)
	queryTimeout time.Duration
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

	return &Searcher{
		db:           db,
		findQmd:      findQmd,
		runQmd:       runQmdCommand,
		queryTimeout: defaultQueryTimeout,
	}, nil
}

func (s *Searcher) ensureDefaults() {
	if s.findQmd == nil {
		s.findQmd = findQmd
	}
	if s.runQmd == nil {
		s.runQmd = runQmdCommand
	}
	if s.queryTimeout <= 0 {
		s.queryTimeout = defaultQueryTimeout
	}
}

// Search runs hybrid qmd search by default and falls back to BM25 on failure.
func (s *Searcher) Search(opts SearchOptions) ([]Result, error) {
	s.ensureDefaults()
	opts = normalizeSearchOptions(opts)

	hybridResults, err := s.searchHybrid(opts)
	if err == nil {
		return hybridResults, nil
	}

	bm25Results, bm25Err := s.searchBM25(opts)
	if bm25Err == nil {
		return bm25Results, nil
	}

	return nil, fmt.Errorf("search failed: hybrid error: %w; bm25 error: %v", err, bm25Err)
}

func normalizeSearchOptions(opts SearchOptions) SearchOptions {
	opts.Query = strings.TrimSpace(opts.Query)
	if opts.Collection == "" {
		opts.Collection = defaultCollection
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}
	if opts.Limit > maxLimit {
		opts.Limit = maxLimit
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	if opts.CandidateLimit < 0 {
		opts.CandidateLimit = 0
	}
	if opts.MinScore < 0 {
		opts.MinScore = 0
	}
	return opts
}

func (s *Searcher) searchHybrid(opts SearchOptions) ([]Result, error) {
	if opts.Query == "" {
		return nil, &HybridSearchError{Reason: "invalid_query", Err: errors.New("query is required")}
	}

	qmdPath, err := s.findQmd()
	if err != nil {
		return nil, &HybridSearchError{Reason: "qmd_not_found", Err: err}
	}

	fetchLimit := opts.Offset + opts.Limit
	if fetchLimit < opts.Limit {
		fetchLimit = opts.Limit
	}
	args := buildHybridArgs(opts, fetchLimit)

	ctx, cancel := context.WithTimeout(context.Background(), s.queryTimeout)
	defer cancel()

	stdout, stderr, err := s.runQmd(ctx, qmdPath, args)
	if err != nil {
		reason := "qmd_exec_failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		return nil, &HybridSearchError{Reason: reason, Err: err, Stderr: strings.TrimSpace(string(stderr))}
	}

	results, err := parseHybridResults(stdout, opts.Collection)
	if err != nil {
		return nil, &HybridSearchError{Reason: "parse_failed", Err: err}
	}

	if err := s.populateModifiedAt(opts.Collection, results); err != nil {
		return nil, &HybridSearchError{Reason: "metadata_lookup_failed", Err: err}
	}

	start := opts.Offset
	if start >= len(results) {
		return []Result{}, nil
	}
	end := start + opts.Limit
	if end > len(results) {
		end = len(results)
	}
	return results[start:end], nil
}

func buildHybridArgs(opts SearchOptions, fetchLimit int) []string {
	args := []string{"query", opts.Query, "--json", "-n", strconv.Itoa(fetchLimit)}
	if opts.Collection != "" {
		args = append(args, "-c", opts.Collection)
	}
	if strings.TrimSpace(opts.Intent) != "" {
		args = append(args, "--intent", strings.TrimSpace(opts.Intent))
	}
	if opts.CandidateLimit > 0 {
		args = append(args, "-C", strconv.Itoa(opts.CandidateLimit))
	}
	if opts.MinScore > 0 {
		args = append(args, "--min-score", strconv.FormatFloat(opts.MinScore, 'f', -1, 64))
	}
	if opts.Explain {
		args = append(args, "--explain")
	}
	return args
}

func runQmdCommand(ctx context.Context, qmdPath string, args []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, qmdPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	return stdout, stderr.Bytes(), err
}

type qmdHybridRow struct {
	File    string          `json:"file"`
	Title   string          `json:"title"`
	Snippet string          `json:"snippet"`
	Score   float64         `json:"score"`
	Explain json.RawMessage `json:"explain"`
}

func parseHybridResults(raw []byte, collection string) ([]Result, error) {
	var rows []qmdHybridRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("failed to parse qmd JSON output: %w", err)
	}

	results := make([]Result, 0, len(rows))
	for _, row := range rows {
		path := normalizeQmdPath(row.File, collection)
		explain := compactJSON(row.Explain)
		results = append(results, Result{
			Path:    path,
			Title:   row.Title,
			Snippet: row.Snippet,
			Score:   row.Score,
			Explain: explain,
		})
	}
	return results, nil
}

func normalizeQmdPath(rawPath, collection string) string {
	path := strings.TrimSpace(rawPath)
	const qmdPrefix = "qmd://"
	if !strings.HasPrefix(path, qmdPrefix) {
		return path
	}
	rest := strings.TrimPrefix(path, qmdPrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return path
	}
	coll := parts[0]
	rel := strings.TrimPrefix(parts[1], "/")
	if rel == "" {
		return path
	}
	if collection == "" || coll == collection {
		return rel
	}
	return rel
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		cloned := make([]byte, len(raw))
		copy(cloned, raw)
		return json.RawMessage(cloned)
	}
	cloned := make([]byte, compacted.Len())
	copy(cloned, compacted.Bytes())
	return json.RawMessage(cloned)
}

func (s *Searcher) populateModifiedAt(collection string, results []Result) error {
	if len(results) == 0 {
		return nil
	}

	paths := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		if r.Path == "" {
			continue
		}
		if strings.HasPrefix(r.Path, "qmd://") {
			continue
		}
		if _, ok := seen[r.Path]; ok {
			continue
		}
		seen[r.Path] = struct{}{}
		paths = append(paths, r.Path)
	}
	if len(paths) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(paths))
	args := make([]any, 0, len(paths)+1)
	args = append(args, collection)
	for _, path := range paths {
		placeholders = append(placeholders, "?")
		args = append(args, path)
	}

	query := `
		SELECT path, COALESCE(modified_at, '') as modified_at
		FROM documents
		WHERE collection = ?
		  AND active = 1
		  AND path IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to look up modified timestamps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	modifiedByPath := make(map[string]string, len(paths))
	for rows.Next() {
		var path, modifiedAt string
		if err := rows.Scan(&path, &modifiedAt); err != nil {
			return fmt.Errorf("failed to scan modified timestamp: %w", err)
		}
		modifiedByPath[path] = modifiedAt
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed while reading modified timestamps: %w", err)
	}

	for i := range results {
		if modifiedAt, ok := modifiedByPath[results[i].Path]; ok {
			results[i].ModifiedAt = modifiedAt
		}
	}

	return nil
}

// searchBM25 runs an FTS5 BM25 query against the qmd index.
func (s *Searcher) searchBM25(opts SearchOptions) ([]Result, error) {
	query := `
		SELECT d.path, d.title,
		       snippet(documents_fts, 2, '<mark>', '</mark>', '...', 32) as snippet,
		       bm25(documents_fts, 5.0, 10.0, 1.0) as score,
		       COALESCE(d.modified_at, '') as modified_at
		FROM documents_fts
		JOIN documents d ON d.rowid = documents_fts.rowid
		WHERE documents_fts MATCH ?
		  AND d.collection = ?
		  AND d.active = 1
		ORDER BY score ASC
		LIMIT ? OFFSET ?`

	rows, err := s.db.Query(query, opts.Query, opts.Collection, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]Result, 0, opts.Limit)
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

// findQmd locates the qmd binary, checking node_modules paths first,
// then falling back to PATH lookup.
func findQmd() (string, error) {
	// Check node_modules (installed via npm/pnpm)
	candidates := []string{
		filepath.Join("node_modules", ".bin", "qmd"),
		filepath.Join("node_modules", "@tobilu", "qmd", "qmd"),
		filepath.Join("node_modules", "qmd", "qmd"),
	}
	for _, local := range candidates {
		if abs, err := filepath.Abs(local); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs, nil
			}
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
		return fmt.Errorf("qmd not found (checked node_modules and PATH): %w", err)
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

		if strings.HasPrefix(r.Path, "qmd://") {
			mapped[i].Path = normalizeQmdPath(r.Path, "")
			continue
		}

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
		return fmt.Errorf("qmd not found (checked node_modules and PATH): %w", err)
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
