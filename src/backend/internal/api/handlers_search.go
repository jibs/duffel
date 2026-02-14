package api

import (
	"net/http"
	"strconv"

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
		writeJSON(w, http.StatusOK, results)
	}
}
