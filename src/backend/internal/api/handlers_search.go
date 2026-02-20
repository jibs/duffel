package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

func handleSearch(store *storage.Store, getSearcher func() *search.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required", "")
			return
		}
		fields, err := parseSearchFields(r.URL.Query().Get("fields"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "")
			return
		}

		searcher := getSearcher()
		if searcher == nil {
			writeError(w, http.StatusServiceUnavailable, "search is unavailable — qmd has not indexed yet", "")
			return
		}

		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		sort := r.URL.Query().Get("sort")
		if sort != "date" {
			sort = "score"
		}

		opts := search.SearchOptions{
			Query:      query,
			Collection: "duffel",
			Limit:      limit,
			Offset:     offset,
			Prefix:     r.URL.Query().Get("prefix"),
			Sort:       sort,
			After:      r.URL.Query().Get("after"),
			Before:     r.URL.Query().Get("before"),
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
			return nil, fmt.Errorf("invalid fields parameter: unknown field %q (allowed: path,title,snippet,score,modified_at)", field)
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
			}
		}
		projected = append(projected, item)
	}
	return projected
}
