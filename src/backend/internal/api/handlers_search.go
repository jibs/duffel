package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

func handleSearch(store *storage.Store, getSearcher func() *search.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		opts, fields, err := parseSearchRequest(r.URL.Query())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "")
			return
		}

		searcher := getSearcher()
		if searcher == nil {
			writeError(w, http.StatusServiceUnavailable, "search is unavailable — qmd has not indexed yet", "")
			return
		}

		results, err := searcher.Search(opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "search failed: "+err.Error(), "")
			return
		}
		results = search.MapPaths(results, store.Root())

		if results == nil {
			results = []search.Result{}
		}
		if len(fields) == 0 {
			writeJSON(w, http.StatusOK, results)
			return
		}
		writeJSON(w, http.StatusOK, projectSearchResults(results, fields))
	}
}

func parseSearchRequest(params url.Values) (search.SearchOptions, []string, error) {
	if err := rejectLegacySearchParams(params); err != nil {
		return search.SearchOptions{}, nil, err
	}

	query := strings.TrimSpace(params.Get("q"))
	if query == "" {
		return search.SearchOptions{}, nil, fmt.Errorf("query parameter 'q' is required")
	}

	fields, err := parseSearchFields(params.Get("fields"))
	if err != nil {
		return search.SearchOptions{}, nil, err
	}

	limit := 20
	if raw := strings.TrimSpace(params.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return search.SearchOptions{}, nil, fmt.Errorf("invalid limit parameter: must be a positive integer")
		}
		limit = n
	}

	offset := 0
	if raw := strings.TrimSpace(params.Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return search.SearchOptions{}, nil, fmt.Errorf("invalid offset parameter: must be a non-negative integer")
		}
		offset = n
	}

	candidateLimit := 0
	if raw := strings.TrimSpace(params.Get("candidate_limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return search.SearchOptions{}, nil, fmt.Errorf("invalid candidate_limit parameter: must be a positive integer")
		}
		candidateLimit = n
	}

	minScore := 0.0
	if raw := strings.TrimSpace(params.Get("min_score")); raw != "" {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n < 0 {
			return search.SearchOptions{}, nil, fmt.Errorf("invalid min_score parameter: must be a non-negative number")
		}
		minScore = n
	}

	explain := false
	if raw := strings.TrimSpace(params.Get("explain")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return search.SearchOptions{}, nil, fmt.Errorf("invalid explain parameter: must be true or false")
		}
		explain = b
	}

	opts := search.SearchOptions{
		Query:          query,
		Collection:     "duffel",
		Limit:          limit,
		Offset:         offset,
		Intent:         strings.TrimSpace(params.Get("intent")),
		CandidateLimit: candidateLimit,
		MinScore:       minScore,
		Explain:        explain,
	}
	return opts, fields, nil
}

func rejectLegacySearchParams(params url.Values) error {
	legacy := []string{"sort", "prefix", "after", "before"}
	for _, key := range legacy {
		if _, ok := params[key]; ok {
			return fmt.Errorf("unsupported search parameter %q: legacy filters were removed", key)
		}
	}
	return nil
}

func parseSearchFields(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	allowed := map[string]struct{}{
		"path":        {},
		"title":       {},
		"snippet":     {},
		"score":       {},
		"modified_at": {},
		"explain":     {},
	}

	parts := strings.Split(raw, ",")
	fields := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("invalid fields parameter: unknown field %q (allowed: path,title,snippet,score,modified_at,explain)", field)
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}

	if len(fields) == 0 {
		return nil, nil
	}
	return fields, nil
}

func projectSearchResults(results []search.Result, fields []string) []map[string]any {
	projected := make([]map[string]any, 0, len(results))
	for _, result := range results {
		item := make(map[string]any, len(fields))
		for _, field := range fields {
			switch field {
			case "path":
				item[field] = result.Path
			case "title":
				item[field] = result.Title
			case "snippet":
				item[field] = result.Snippet
			case "score":
				item[field] = result.Score
			case "modified_at":
				item[field] = result.ModifiedAt
			case "explain":
				item[field] = result.Explain
			}
		}
		projected = append(projected, item)
	}
	return projected
}
