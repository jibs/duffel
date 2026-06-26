package api

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"duffel/pkg/duffellib/search"
	"duffel/pkg/duffellib/storage"
)

const (
	recommendationLimit          = 5
	recommendationFetchLimit     = 20
	recommendationCandidateLimit = 120
	recommendationMaxQueryRunes  = 1800
)

type recommendationCacheEntry struct {
	modTime time.Time
	results []search.Result
}

// RecommendationCache keeps related-content lookups off the read path. It is
// intentionally eventually consistent; reads return the most recently
// materialized recommendations, or an empty slice while the cache is warming.
type RecommendationCache struct {
	mu      sync.RWMutex
	entries map[string]recommendationCacheEntry
}

func NewRecommendationCache() *RecommendationCache {
	return &RecommendationCache{
		entries: make(map[string]recommendationCacheEntry),
	}
}

func (c *RecommendationCache) ForFile(file *storage.FileInfo) []search.Result {
	if c == nil || file == nil || file.IsJournal {
		return []search.Result{}
	}
	path := normalizeRecommendationPath(file.Path)
	if path == "" {
		return []search.Result{}
	}

	c.mu.RLock()
	entry, ok := c.entries[path]
	c.mu.RUnlock()
	if !ok || !entry.modTime.Equal(file.ModTime) {
		return []search.Result{}
	}
	return cloneSearchResults(entry.results)
}

func (c *RecommendationCache) MaterializePaths(store *storage.Store, searcher *search.Searcher, paths []string) {
	if c == nil || store == nil || searcher == nil {
		return
	}

	for _, path := range paths {
		key := normalizeRecommendationPath(path)
		if key == "" {
			continue
		}
		if storage.IsImagePath(key) {
			c.Delete(key)
			continue
		}

		file, err := store.Read(key)
		if err != nil {
			c.Delete(key)
			continue
		}
		if file.IsJournal {
			c.Delete(key)
			continue
		}

		results := computeRecommendations(store, searcher, file)
		c.Set(file, results)
	}
}

func (c *RecommendationCache) Set(file *storage.FileInfo, results []search.Result) {
	if c == nil || file == nil {
		return
	}
	key := normalizeRecommendationPath(file.Path)
	if key == "" {
		return
	}
	c.mu.Lock()
	c.entries[key] = recommendationCacheEntry{
		modTime: file.ModTime,
		results: cloneSearchResults(results),
	}
	c.mu.Unlock()
}

func (c *RecommendationCache) Delete(path string) {
	if c == nil {
		return
	}
	key := normalizeRecommendationPath(path)
	if key == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func recommendForFile(cache *RecommendationCache, file *storage.FileInfo) []search.Result {
	return cache.ForFile(file)
}

func computeRecommendations(store *storage.Store, searcher *search.Searcher, file *storage.FileInfo) []search.Result {
	if file == nil || file.IsJournal {
		return []search.Result{}
	}
	if searcher == nil {
		return []search.Result{}
	}

	title, query := buildRecommendationQuery(file.Path, file.Content)
	if strings.TrimSpace(query) == "" {
		return []search.Result{}
	}

	opts := search.SearchOptions{
		Query:          buildRecommendationQmdQuery(query),
		Collection:     "duffel",
		Limit:          recommendationFetchLimit,
		CandidateLimit: recommendationCandidateLimit,
		Intent:         recommendationIntent(title),
		RequireHybrid:  true,
		SkipRerank:     true,
	}

	results, err := searcher.Search(opts)
	if err != nil {
		return []search.Result{}
	}
	results = search.MapPaths(results, store.Root())
	return postProcessRecommendations(file.Path, results, recommendationLimit)
}

func cloneSearchResults(results []search.Result) []search.Result {
	if len(results) == 0 {
		return []search.Result{}
	}
	cloned := make([]search.Result, len(results))
	copy(cloned, results)
	return cloned
}

func normalizeRecommendationPath(path string) string {
	path = normalizePathForComparison(path)
	path = strings.TrimPrefix(path, "/")
	if path == "." {
		return ""
	}
	return path
}

func buildRecommendationQmdQuery(query string) string {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return ""
	}
	return "lex: " + query
}

func buildRecommendationQuery(path string, content string) (string, string) {
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	body := stripFrontMatter(content)
	body = strings.Join(strings.Fields(body), " ")
	body = truncateRunes(body, recommendationMaxQueryRunes)

	switch {
	case title == "" && body == "":
		return "", ""
	case body == "":
		return title, title
	case title == "":
		return "", body
	default:
		return title, title + " " + body
	}
}

func recommendationIntent(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "related notes for this note"
	}
	return fmt.Sprintf("related notes for %s", title)
}

func stripFrontMatter(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimPrefix(normalized, "\ufeff")
	if !strings.HasPrefix(normalized, "---\n") {
		return normalized
	}

	rest := strings.TrimPrefix(normalized, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		return normalized
	}
	return rest[end+len("\n---\n"):]
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit])
}

func postProcessRecommendations(currentPath string, results []search.Result, limit int) []search.Result {
	currentPath = normalizePathForComparison(currentPath)
	filtered := make([]search.Result, 0, limit)
	seen := make(map[string]struct{}, len(results))

	for _, result := range results {
		p := normalizePathForComparison(result.Path)
		if p == "" || p == currentPath {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		filtered = append(filtered, result)
		if len(filtered) == limit {
			break
		}
	}
	return filtered
}

func normalizePathForComparison(path string) string {
	if path == "" {
		return ""
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	normalized = strings.TrimPrefix(normalized, "./")
	return normalized
}
