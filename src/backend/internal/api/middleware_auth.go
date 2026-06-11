package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"duffel/src/backend/internal/auth"
)

func requireAuthMiddleware(authService *auth.Service, resourceMetadataPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authService == nil || !authService.Enabled() || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			trusted, err := isTrustedTailscaleRequest(r, authService)
			if err != nil {
				writeAuthUnauthorized(w, r, resourceMetadataPath, "invalid_request", "trusted device check failed")
				return
			}
			if trusted {
				next.ServeHTTP(w, r)
				return
			}

			authz := strings.TrimSpace(r.Header.Get("Authorization"))
			if authz == "" {
				writeAuthUnauthorized(w, r, resourceMetadataPath, "invalid_request", "missing bearer token")
				return
			}
			parts := strings.SplitN(authz, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
				writeAuthUnauthorized(w, r, resourceMetadataPath, "invalid_request", "invalid authorization header")
				return
			}

			if _, err := authService.ValidateBearerToken(r.Context(), strings.TrimSpace(parts[1])); err != nil {
				writeAuthUnauthorized(w, r, resourceMetadataPath, "invalid_token", "bearer token is invalid or expired")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isTrustedTailscaleRequest(r *http.Request, authService *auth.Service) (bool, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false, nil
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	tailscalePrefix := netip.MustParsePrefix("100.64.0.0/10")
	if !tailscalePrefix.Contains(addr) {
		return false, nil
	}
	return authService.IsTrustedDevice(r.Context(), addr.String())
}

func writeAuthUnauthorized(w http.ResponseWriter, r *http.Request, resourceMetadataPath, oauthErr, description string) {
	base := requestOrigin(r)
	resourceMetaURL := strings.TrimRight(base, "/") + resourceMetadataPath
	challenge := fmt.Sprintf(`Bearer realm="Duffel", resource_metadata="%s", error="%s", error_description="%s"`,
		resourceMetaURL,
		escapeChallengeValue(oauthErr),
		escapeChallengeValue(description),
	)
	w.Header().Set("WWW-Authenticate", challenge)
	writeError(w, http.StatusUnauthorized, description, r.URL.Path)
}

func escapeChallengeValue(value string) string {
	value = strings.ReplaceAll(value, `\\`, ``)
	value = strings.ReplaceAll(value, `"`, "")
	return value
}
