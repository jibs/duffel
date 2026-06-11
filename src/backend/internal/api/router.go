package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"duffel/src/backend/internal/auth"
	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(store *storage.Store, getSearcher func() *search.Searcher, onContentChanged func(), frontendDir string) http.Handler {
	r := chi.NewRouter()
	allowedOrigins := parseAllowedOrigins(os.Getenv("DUFFEL_ALLOWED_ORIGINS"))
	eventBroker := newEventBroker()
	authService, err := auth.New(store.Root(), auth.ConfigFromEnv())
	if err != nil {
		panic(fmt.Sprintf("initialize auth service: %v", err))
	}
	if onContentChanged == nil {
		onContentChanged = func() {}
	}
	notifyContentChanged := func() {
		onContentChanged()
		eventBroker.Broadcast()
	}
	startFilesystemChangeWatcher(store.Root(), notifyContentChanged)

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(noCacheMiddleware)
	r.Use(corsMiddleware(allowedOrigins))
	r.Use(writeOriginGuardMiddleware(allowedOrigins))

	r.Get("/.well-known/oauth-protected-resource", handleOAuthProtectedResource(authService, "/api"))
	r.Get("/.well-known/oauth-protected-resource/mcp", handleOAuthProtectedResource(authService, "/mcp"))
	r.Get("/.well-known/oauth-authorization-server", handleOAuthAuthorizationServer(authService))

	r.Route("/oauth", func(r chi.Router) {
		r.Method(http.MethodGet, "/setup", handleOAuthSetup(authService))
		r.Method(http.MethodPost, "/setup", handleOAuthSetup(authService))
		r.Post("/register", handleOAuthRegister(authService))
		r.Get("/authorize", handleOAuthAuthorizeGet(authService))
		r.Post("/authorize", handleOAuthAuthorizePost(authService))
		r.Post("/token", handleOAuthToken(authService))
	})

	r.Group(func(r chi.Router) {
		r.Use(requireAuthMiddleware(authService, "/.well-known/oauth-protected-resource/mcp"))
		r.Get("/mcp", handleMCP(store, getSearcher, notifyContentChanged))
		r.Post("/mcp", handleMCP(store, getSearcher, notifyContentChanged))
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Route("/agent", func(r chi.Router) {
			r.Get("/script", handleAgentScript(authService))
			r.Get("/snippet", handleAgentSnippet(authService))
			r.Get("/version", handleAgentVersion())
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAuthMiddleware(authService, "/.well-known/oauth-protected-resource"))

			r.Get("/fs/*", handleFSGet(store, getSearcher))
			r.Put("/fs/*", handleFSPut(store, notifyContentChanged))
			r.Delete("/fs/*", handleFSDelete(store, notifyContentChanged))
			r.Post("/fs/*", handleFSPost(store, notifyContentChanged))

			r.Post("/move/*", handleFSMove(store, notifyContentChanged))

			r.Post("/archive/*", handleArchive(store, notifyContentChanged))
			r.Post("/unarchive/*", handleUnarchive(store, notifyContentChanged))

			r.Post("/journal/*", handleJournal(store, notifyContentChanged))

			r.Get("/search", handleSearch(store, getSearcher))
			r.Get("/events", handleEvents(eventBroker))
			r.Method(http.MethodGet, "/mcp/tokens", handleMCPTokens(authService))
			r.Method(http.MethodPost, "/mcp/tokens", handleMCPTokens(authService))
			r.Method(http.MethodDelete, "/mcp/tokens/*", handleMCPToken(authService))
			r.Method(http.MethodGet, "/mcp/trusted-devices", handleTrustedDevices(authService))
			r.Method(http.MethodPost, "/mcp/trusted-devices", handleTrustedDevices(authService))
			r.Method(http.MethodDelete, "/mcp/trusted-devices/*", handleTrustedDevice(authService))
		})
	})

	// Serve frontend static files
	fileServer := http.FileServer(http.Dir(frontendDir))
	r.Handle("/*", fileServer)

	return r
}

func noCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
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
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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
	return requestScheme(r) + "://" + requestHost(r)
}

func requestScheme(r *http.Request) string {
	if proto := forwardedHeaderValue(r.Header.Get("Forwarded"), "proto"); proto != "" {
		return strings.ToLower(proto)
	}
	if proto := firstProxyHeaderValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return strings.ToLower(proto)
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHost(r *http.Request) string {
	if host := forwardedHost(r); host != "" {
		return host
	}
	return r.Host
}

func forwardedHost(r *http.Request) string {
	host := forwardedHeaderValue(r.Header.Get("Forwarded"), "host")
	if host == "" {
		host = firstProxyHeaderValue(r.Header.Get("X-Forwarded-Host"))
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	port := firstProxyHeaderValue(r.Header.Get("X-Forwarded-Port"))
	if port != "" && !hostIncludesPort(host) && port != defaultPortForScheme(requestScheme(r)) {
		return host + ":" + port
	}
	return host
}

func firstProxyHeaderValue(raw string) string {
	return strings.TrimSpace(strings.Split(raw, ",")[0])
}

func forwardedHeaderValue(raw, key string) string {
	entry := firstProxyHeaderValue(raw)
	if entry == "" {
		return ""
	}
	for _, part := range strings.Split(entry, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), key) {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

func hostIncludesPort(host string) bool {
	if strings.HasPrefix(host, "[") {
		return strings.Contains(host, "]:")
	}
	return strings.Count(host, ":") == 1
}

func defaultPortForScheme(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
