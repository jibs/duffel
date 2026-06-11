package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"duffel/src/backend/internal/auth"
)

func handleTrustedDevices(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}

		switch r.Method {
		case http.MethodGet:
			devices, err := authService.ListTrustedDevices(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"devices": devices,
				"current": currentTailscaleIP(r),
			})
		case http.MethodPost:
			var body struct {
				IP    string `json:"ip"`
				Label string `json:"label"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body", r.URL.Path)
				return
			}
			ip := strings.TrimSpace(body.IP)
			if ip == "" {
				ip = currentTailscaleIP(r)
			}
			if !isTailscaleIP(ip) {
				writeError(w, http.StatusBadRequest, "trusted device must be a Tailscale 100.64.0.0/10 address", r.URL.Path)
				return
			}
			device, err := authService.TrustDevice(r.Context(), ip, body.Label)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"device": device})
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleTrustedDevice(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		ip := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/mcp/trusted-devices/"))
		if !isTailscaleIP(ip) {
			writeError(w, http.StatusBadRequest, "trusted device must be a Tailscale 100.64.0.0/10 address", r.URL.Path)
			return
		}
		if err := authService.RevokeTrustedDevice(r.Context(), ip); err != nil {
			if errors.Is(err, auth.ErrUnauthorized) {
				writeError(w, http.StatusNotFound, "trusted device not found", r.URL.Path)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
	}
}

func currentTailscaleIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if !isTailscaleIP(addr.String()) {
		return ""
	}
	return addr.String()
}

func isTailscaleIP(raw string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
}
