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
	"sync"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

const (
	defaultCollection           = "duffel"
	defaultLimit                = 20
	maxLimit                    = 100
	defaultQueryTimeout         = 20 * time.Second
	defaultHybridQueryQueueSize = 1
	collectionMask              = "**/*.{md,html}"
)

// Result represents a single search result from qmd.
type Result struct {
	Path       string          `json:"path"`
	Title      string          `json:"title"`
	Snippet    string          `json:"snippet"`
	Score      float64         `json:"score"`
	ModifiedAt string          `json:"modified_at"`
	Line       int             `json:"line,omitempty"`
	Context    string          `json:"context,omitempty"`
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
	DisableHybrid  bool
	RequireHybrid  bool
	SkipRerank     bool
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
	defaultsOnce sync.Once
	hybridSlots  chan struct{}
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
	s.defaultsOnce.Do(func() {
		if s.findQmd == nil {
			s.findQmd = findQmd
		}
		if s.runQmd == nil {
			s.runQmd = runQmdCommand
		}
		if s.queryTimeout <= 0 {
			s.queryTimeout = defaultQueryTimeout
		}
		if s.hybridSlots == nil {
			s.hybridSlots = make(chan struct{}, defaultHybridQueryQueueSize)
		}
	})
}

// Search runs hybrid qmd search by default and falls back to BM25 on failure.
func (s *Searcher) Search(opts SearchOptions) ([]Result, error) {
	s.ensureDefaults()
	opts = normalizeSearchOptions(opts)
	bm25Opts := opts
	bm25Opts.Query = buildBM25MatchQuery(opts.Query)

	if opts.DisableHybrid || shouldBypassHybrid(opts.Query) {
		return s.searchBM25(bm25Opts)
	}

	hybridResults, err := s.searchHybrid(opts)
	if err == nil {
		return hybridResults, nil
	}
	if opts.RequireHybrid {
		return nil, err
	}

	bm25Results, bm25Err := s.searchBM25(bm25Opts)
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

func shouldBypassHybrid(query string) bool {
	query = strings.TrimSpace(query)
	if len(splitPipeAlternatives(query)) > 1 {
		return true
	}
	return shouldPreferBM25(query)
}

func buildBM25MatchQuery(raw string) string {
	query := strings.TrimSpace(raw)
	if query == "" {
		return ""
	}

	parts := splitPipeAlternatives(query)
	if len(parts) > 1 {
		phrases := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			phrases = append(phrases, quoteFTSPhrase(part))
		}
		if len(phrases) == 0 {
			return query
		}
		if len(phrases) == 1 {
			return phrases[0]
		}
		return strings.Join(phrases, " OR ")
	}

	tokens := tokenizeBM25Query(query)
	if len(tokens) == 0 {
		return query
	}

	sanitized := make([]string, 0, len(tokens))
	for _, token := range tokens {
		sanitized = append(sanitized, sanitizeBM25Token(token))
	}
	return strings.Join(sanitized, " ")
}

func splitPipeAlternatives(query string) []string {
	if !strings.Contains(query, "|") {
		return []string{query}
	}

	parts := make([]string, 0, 4)
	var current strings.Builder
	escaped := false
	for _, r := range query {
		if escaped {
			if r == '|' {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune('\\')
				current.WriteRune(r)
			}
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if escaped {
		current.WriteRune('\\')
	}
	parts = append(parts, current.String())
	return parts
}

func quoteFTSPhrase(term string) string {
	if term == "" {
		return term
	}
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}

func shouldPreferBM25(query string) bool {
	tokens := tokenizeBM25Query(query)
	if len(tokens) != 1 {
		return false
	}

	token := tokens[0]
	if token == "" {
		return false
	}
	if isQuotedFTSPhrase(token) {
		return true
	}
	if isBM25Operator(token) {
		return false
	}
	return !isSafeBM25Term(token)
}

func tokenizeBM25Query(query string) []string {
	tokens := make([]string, 0, 8)
	var current strings.Builder
	var quoted strings.Builder
	inQuote := false

	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range query {
		if inQuote {
			quoted.WriteRune(r)
			if r == '"' {
				tokens = append(tokens, quoted.String())
				quoted.Reset()
				inQuote = false
			}
			continue
		}

		switch {
		case unicode.IsSpace(r):
			flushCurrent()
		case r == '(' || r == ')':
			flushCurrent()
			tokens = append(tokens, string(r))
		case r == '"':
			flushCurrent()
			quoted.WriteRune(r)
			inQuote = true
		default:
			current.WriteRune(r)
		}
	}

	if inQuote {
		tokens = append(tokens, quoteFTSPhrase(quoted.String()[1:]))
	}
	flushCurrent()
	return tokens
}

func sanitizeBM25Token(token string) string {
	if token == "" {
		return token
	}
	if token == "(" || token == ")" {
		return token
	}
	if isQuotedFTSPhrase(token) || isBM25Operator(token) || isSafeBM25Term(token) {
		return token
	}
	return quoteFTSPhrase(token)
}

func isQuotedFTSPhrase(token string) bool {
	return len(token) >= 2 && strings.HasPrefix(token, `"`) && strings.HasSuffix(token, `"`)
}

func isBM25Operator(token string) bool {
	switch strings.ToUpper(token) {
	case "AND", "OR", "NOT":
		return true
	default:
		return false
	}
}

func isSafeBM25Term(token string) bool {
	if token == "" {
		return false
	}

	prefix := strings.TrimSuffix(token, "*")
	if prefix == "" {
		return false
	}
	if prefix != token && strings.Count(token, "*") != 1 {
		return false
	}

	for _, r := range prefix {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return false
	}
	return true
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

	if err := s.acquireHybridSlot(ctx); err != nil {
		reason := "busy"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "timeout"
		}
		return nil, &HybridSearchError{Reason: reason, Err: err}
	}
	defer s.releaseHybridSlot()

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

// buildHybridArgs assembles the `qmd query` invocation.
//
// qmd's default single-line query runs LLM query expansion (a 1.7B generation
// model) and cross-encoder reranking. On CPU-only hosts both are far too slow
// to finish within queryTimeout — measured at 80s+ per query — so the server
// would always time out and silently degrade to BM25. To get genuine semantic
// results at interactive speed (~1-2s) we instead issue a structured lex+vec
// query document (which bypasses expansion) and always pass --no-rerank, giving
// RRF-fused vector+BM25 hybrid ranking.
func buildHybridArgs(opts SearchOptions, fetchLimit int) []string {
	args := []string{"query", buildHybridQueryDocument(opts), "--json", "-n", strconv.Itoa(fetchLimit)}
	if opts.Collection != "" {
		args = append(args, "-c", opts.Collection)
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
	args = append(args, "--no-rerank")
	return args
}

// buildHybridQueryDocument converts a free-text query into a qmd structured
// query document with explicit lex and vec lines. Providing the lines directly
// skips qmd's LLM query-expansion step. An optional intent line is included
// when set. Embedded newlines are flattened so each typed line stays intact.
func buildHybridQueryDocument(opts SearchOptions) string {
	flatten := func(s string) string {
		s = strings.ReplaceAll(s, "\r", " ")
		s = strings.ReplaceAll(s, "\n", " ")
		return strings.TrimSpace(s)
	}
	query := flatten(opts.Query)

	var b strings.Builder
	if intent := flatten(opts.Intent); intent != "" {
		b.WriteString("intent: ")
		b.WriteString(intent)
		b.WriteByte('\n')
	}
	b.WriteString("lex: ")
	b.WriteString(query)
	b.WriteString("\nvec: ")
	b.WriteString(query)
	return b.String()
}

func (s *Searcher) acquireHybridSlot(ctx context.Context) error {
	if s.hybridSlots == nil {
		return nil
	}

	select {
	case s.hybridSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Searcher) releaseHybridSlot() {
	if s.hybridSlots == nil {
		return
	}

	select {
	case <-s.hybridSlots:
	default:
	}
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
	Line    int             `json:"line"`
	Context string          `json:"context"`
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
			Line:    row.Line,
			Context: row.Context,
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

// findQmd locates the vendored qmd binary in this repository.
// It checks DUFFEL_QMD_PATH first, then local node_modules locations.
func findQmd() (string, error) {
	if override := strings.TrimSpace(os.Getenv("DUFFEL_QMD_PATH")); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("failed to resolve DUFFEL_QMD_PATH: %w", err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("DUFFEL_QMD_PATH does not point to an existing file: %w", err)
		}
		return abs, nil
	}

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
	return "", fmt.Errorf("qmd not found in vendored paths; run `pnpm install` or set DUFFEL_QMD_PATH")
}

// StartIndexing runs `qmd update` followed by `qmd embed` in the background to
// re-index all collections and refresh vector embeddings for hybrid search.
// It returns immediately once `qmd update` has started; the onDone callback is
// called when both steps finish, or with the first error encountered.
//
// qmd update only maintains the BM25 index — without the subsequent embed step
// newly written or edited documents never become available to semantic (hybrid)
// search. qmd embed is incremental, so it only embeds documents that changed
// since the last run.
func StartIndexing(collection string, onDone func(error)) error {
	qmdPath, err := findQmd()
	if err != nil {
		return fmt.Errorf("qmd not found (checked DUFFEL_QMD_PATH and vendored node_modules): %w", err)
	}

	updateCmd := exec.Command(qmdPath, "update")
	if err := updateCmd.Start(); err != nil {
		return fmt.Errorf("failed to start qmd update: %w", err)
	}

	go func() {
		if err := updateCmd.Wait(); err != nil {
			onDone(fmt.Errorf("qmd update failed: %w", err))
			return
		}
		if out, err := exec.Command(qmdPath, "embed").CombinedOutput(); err != nil {
			onDone(fmt.Errorf("qmd embed failed: %w\n%s", err, out))
			return
		}
		onDone(nil)
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
// pointing at dataDir with markdown and HTML files. If the collection already exists,
// qmd handles it as a no-op or update.
func EnsureCollection(name, dataDir string) error {
	qmdPath, err := findQmd()
	if err != nil {
		return fmt.Errorf("qmd not found (checked DUFFEL_QMD_PATH and vendored node_modules): %w", err)
	}

	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("failed to resolve data dir: %w", err)
	}

	if err := ensureQmdConfigCollection(name, absDataDir, collectionMask); err != nil {
		return err
	}
	cmd := exec.Command(qmdPath, "collection", "add", absDataDir, "--name", name, "--mask", collectionMask)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// qmd may report an existing path/pattern under a stale generated name.
		// Repair that state so Duffel's fixed collection filter keeps working.
		if strings.Contains(string(out), "already exists") {
			if repairErr := repairQmdCollectionName(name, absDataDir, collectionMask); repairErr != nil {
				return fmt.Errorf("qmd collection already exists but repair failed: %w\n%s", repairErr, out)
			}
			return nil
		}
		return fmt.Errorf("qmd collection add failed: %w\n%s", err, out)
	}

	if repairErr := repairQmdCollectionName(name, absDataDir, collectionMask); repairErr != nil {
		return fmt.Errorf("qmd collection repair failed: %w", repairErr)
	}
	return nil
}

func ensureQmdConfigCollection(name, absDataDir, pattern string) error {
	configPath, err := qmdConfigPath()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return writeQmdConfigCollection(configPath, name, absDataDir, pattern)
	}
	if err != nil {
		return fmt.Errorf("read qmd config: %w", err)
	}

	text := string(content)
	if strings.Contains(text, "\n  "+name+":") {
		updated, changed := replaceQmdConfigCollectionBlock(text, name, absDataDir, pattern)
		if !changed {
			return nil
		}
		if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("write qmd config: %w", err)
		}
		return nil
	}
	if strings.Contains(text, "\n  - ") || strings.Contains(text, "\n  - null") || strings.TrimSpace(text) == "collections:" {
		return writeQmdConfigCollection(configPath, name, absDataDir, pattern)
	}
	return nil
}

func replaceQmdConfigCollectionBlock(text, name, absDataDir, pattern string) (string, bool) {
	lines := strings.SplitAfter(text, "\n")
	start := -1
	prefix := "  " + name + ":"
	for i, line := range lines {
		if strings.TrimRight(line, "\r\n") == prefix {
			start = i
			break
		}
	}
	if start == -1 {
		return text, false
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimRight(lines[i], "\r\n")
		if strings.HasPrefix(trimmed, "  ") && !strings.HasPrefix(trimmed, "    ") {
			end = i
			break
		}
	}

	replacement := fmt.Sprintf("  %s:\n    path: %s\n    pattern: %q\n", name, absDataDir, pattern)
	current := strings.Join(lines[start:end], "")
	if current == replacement {
		return text, false
	}

	next := make([]string, 0, len(lines)-(end-start)+1)
	next = append(next, lines[:start]...)
	next = append(next, replacement)
	next = append(next, lines[end:]...)
	return strings.Join(next, ""), true
}

func qmdConfigPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("QMD_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "index.yml"), nil
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "qmd", "index.yml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "qmd", "index.yml"), nil
}

func writeQmdConfigCollection(configPath, name, absDataDir, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create qmd config dir: %w", err)
	}
	content := fmt.Sprintf("collections:\n  %s:\n    path: %s\n    pattern: %q\n", name, absDataDir, pattern)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write qmd config: %w", err)
	}
	return nil
}

func repairQmdCollectionName(name, absDataDir, pattern string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	dbPath := filepath.Join(home, ".cache", "qmd", "index.sqlite")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open qmd database: %w", err)
	}
	defer func() { _ = db.Close() }()

	var currentName string
	err = db.QueryRow(`
		SELECT name
		FROM store_collections
		WHERE path = ? AND pattern = ?
		LIMIT 1`, absDataDir, pattern).Scan(&currentName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up qmd collection: %w", err)
	}
	if currentName == name {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin qmd repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		UPDATE documents
		SET collection = ?
		WHERE collection = ?
		  AND NOT EXISTS (
		    SELECT 1
		    FROM documents AS existing
		    WHERE existing.collection = ?
		      AND existing.path = documents.path
		  )`, name, currentName, name); err != nil {
		return fmt.Errorf("move qmd documents to collection %q: %w", name, err)
	}
	if _, err := tx.Exec(`DELETE FROM documents WHERE collection = ?`, currentName); err != nil {
		return fmt.Errorf("delete stale qmd documents for collection %q: %w", currentName, err)
	}
	if _, err := tx.Exec(`
		UPDATE store_collections
		SET name = ?
		WHERE name = ? AND path = ? AND pattern = ?`, name, currentName, absDataDir, pattern); err != nil {
		return fmt.Errorf("rename qmd collection %q to %q: %w", currentName, name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit qmd repair: %w", err)
	}
	return nil
}
