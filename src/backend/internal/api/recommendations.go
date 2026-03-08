package api

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

const (
	recommendationLimit          = 5
	recommendationFetchLimit     = 20
	recommendationCandidateLimit = 120
	recommendationMaxQueryRunes  = 1800
)

func recommendForFile(store *storage.Store, getSearcher func() *search.Searcher, file *storage.FileInfo) []search.Result {
	if file == nil || file.IsJournal {
		return []search.Result{}
	}
	if getSearcher == nil {
		return []search.Result{}
	}
	searcher := getSearcher()
	if searcher == nil {
		return []search.Result{}
	}

	title, query := buildRecommendationQuery(file.Path, file.Content)
	if strings.TrimSpace(query) == "" {
		return []search.Result{}
	}

	opts := search.SearchOptions{
		Query:          query,
		Collection:     "duffel",
		Limit:          recommendationFetchLimit,
		CandidateLimit: recommendationCandidateLimit,
		Intent:         recommendationIntent(title),
	}

	results, err := searcher.Search(opts)
	if err != nil {
		return []search.Result{}
	}
	results = search.MapPaths(results, store.Root())
	return postProcessRecommendations(file.Path, results, recommendationLimit)
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
