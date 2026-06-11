package api

import (
	"net/http/httptest"
	"testing"
)

func TestRequestOriginUsesForwardedHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost:4386/api/agent/version", nil)
	req.Header.Set("Forwarded", `for=192.0.2.60;proto=https;host="duffel.tailnet.ts.net:8443"`)

	if got := requestOrigin(req); got != "https://duffel.tailnet.ts.net:8443" {
		t.Fatalf("requestOrigin = %q, want %q", got, "https://duffel.tailnet.ts.net:8443")
	}
}

func TestRequestOriginAppendsForwardedPort(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost:4386/api/agent/version", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "duffel.tailnet.ts.net")
	req.Header.Set("X-Forwarded-Port", "8443")

	if got := requestOrigin(req); got != "https://duffel.tailnet.ts.net:8443" {
		t.Fatalf("requestOrigin = %q, want %q", got, "https://duffel.tailnet.ts.net:8443")
	}
}
