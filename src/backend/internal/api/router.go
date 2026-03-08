package api

import (
	"net/http"
	"os"
	"strings"

	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(store *storage.Store, getSearcher func() *search.Searcher, onContentChanged func(), frontendDir string) http.Handler {
	r := chi.NewRouter()
	allowedOrigins := parseAllowedOrigins(os.Getenv("DUFFEL_ALLOWED_ORIGINS"))
	if onContentChanged == nil {
		onContentChanged = func() {}
	}

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(allowedOrigins))
	r.Use(writeOriginGuardMiddleware(allowedOrigins))

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/fs/*", handleFSGet(store, getSearcher))
		r.Put("/fs/*", handleFSPut(store, onContentChanged))
		r.Delete("/fs/*", handleFSDelete(store, onContentChanged))
		r.Post("/fs/*", handleFSPost(store))

		r.Post("/move/*", handleFSMove(store, onContentChanged))

		r.Post("/archive/*", handleArchive(store, onContentChanged))
		r.Post("/unarchive/*", handleUnarchive(store, onContentChanged))

		r.Post("/journal/*", handleJournal(store, onContentChanged))

		r.Get("/search", handleSearch(store, getSearcher))

		r.Route("/agent", func(r chi.Router) {
			r.Get("/script", handleAgentScript())
			r.Get("/snippet", handleAgentSnippet())
			r.Get("/version", handleAgentVersion())
		})
	})

	// Serve frontend static files
	fileServer := http.FileServer(http.Dir(frontendDir))
	r.Handle("/*", fileServer)

	return r
}

func corsMiddleware(allowedOrigins map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := normalizeOrigin(r.Header.Get("Origin"))
			originAllowed := origin != "" && isOriginAllowed(r, origin, allowedOrigins)

			if originAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == http.MethodOptions {
				if origin != "" && !originAllowed {
					writeError(w, http.StatusForbidden, "origin not allowed", r.URL.Path)
					return
				}
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeOriginGuardMiddleware(allowedOrigins map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutatingAPIRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			origin := normalizeOrigin(r.Header.Get("Origin"))
			if origin != "" && !isOriginAllowed(r, origin, allowedOrigins) {
				writeError(w, http.StatusForbidden, "cross-origin write blocked", r.URL.Path)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isMutatingAPIRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}

	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	default:
		return false
	}
}

func parseAllowedOrigins(raw string) map[string]struct{} {
	origins := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		origin := normalizeOrigin(part)
		if origin == "" {
			continue
		}
		origins[origin] = struct{}{}
	}
	return origins
}

func normalizeOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	origin = strings.TrimSuffix(origin, "/")
	return origin
}

func isOriginAllowed(r *http.Request, origin string, allowedOrigins map[string]struct{}) bool {
	origin = normalizeOrigin(origin)
	if origin == "" {
		return false
	}

	if origin == requestOrigin(r) {
		return true
	}

	_, ok := allowedOrigins[origin]
	return ok
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}
