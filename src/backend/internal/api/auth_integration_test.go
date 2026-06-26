package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"duffel/pkg/duffellib/search"
	"duffel/pkg/duffellib/storage"
	"duffel/src/backend/internal/api"
)

func setupAuthServer(t *testing.T) (*httptest.Server, *storage.Store) {
	t.Helper()
	t.Setenv("DUFFEL_AUTH_ENABLED", "1")
	t.Setenv("DUFFEL_SETUP_TOKEN", "setup-secret")
	dir := t.TempDir()
	store, err := storage.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	router := api.NewRouter(store, func() *search.Searcher { return nil }, nil, dir)
	return httptest.NewServer(router), store
}

func TestOAuthSetupAndProtectedAPI(t *testing.T) {
	srv, _ := setupAuthServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/oauth/setup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup status = %d, want 200", resp.StatusCode)
	}

	setupReq := `{"setupToken":"setup-secret","username":"owner","password":"password123"}`
	resp, err = http.Post(srv.URL+"/oauth/setup", "application/json", strings.NewReader(setupReq))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("setup status = %d, want 201; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var setupBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&setupBody); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}

	resp, err = http.Get(srv.URL + "/api/fs/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth fs status = %d, want 401", resp.StatusCode)
	}

	accessToken := oauthLoginOwner(t, srv.URL)

	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/auth-check.md", strings.NewReader(`{"content":"ok"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authed PUT status = %d, want 200; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	resp, err = http.Get(srv.URL + "/api/agent/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent/version status = %d, want 200", resp.StatusCode)
	}
}

func TestMCPAuthChallengeAndInitialize(t *testing.T) {
	srv, _ := setupAuthServer(t)
	defer srv.Close()

	setupReq := `{"setupToken":"setup-secret","username":"owner","password":"password123"}`
	resp, err := http.Post(srv.URL+"/oauth/setup", "application/json", strings.NewReader(setupReq))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth mcp status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatalf("WWW-Authenticate missing resource_metadata: %q", resp.Header.Get("WWW-Authenticate"))
	}

	accessToken := oauthLoginOwner(t, srv.URL)

	req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authed mcp initialize status = %d, want 200; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rpc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		t.Fatalf("decode mcp initialize: %v", err)
	}
	if rpc["result"] == nil {
		t.Fatalf("initialize result missing: %#v", rpc)
	}
}

func TestMCPTokensCanBeCreatedListedAndRevoked(t *testing.T) {
	srv, _ := setupAuthServer(t)
	defer srv.Close()

	setupReq := `{"setupToken":"setup-secret","username":"owner","password":"password123"}`
	resp, err := http.Post(srv.URL+"/oauth/setup", "application/json", strings.NewReader(setupReq))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("setup status = %d, want 201; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var setupBody struct {
		BootstrapToken string `json:"bootstrap_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setupBody); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}

	req, _ := http.NewRequest("POST", srv.URL+"/api/mcp/tokens", strings.NewReader(`{"label":"Codex desktop"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+setupBody.BootstrapToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create token status = %d, want 201; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var created struct {
		Token  string `json:"token"`
		Record struct {
			ID      string `json:"id"`
			Label   string `json:"label"`
			Revoked bool   `json:"revoked"`
		} `json:"record"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Token == "" || created.Record.ID == "" || created.Record.Label != "Codex desktop" || created.Record.Revoked {
		t.Fatalf("unexpected created token response: %#v", created)
	}

	req, _ = http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp with created token status = %d, want 200", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", srv.URL+"/api/mcp/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+setupBody.BootstrapToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list token status = %d, want 200; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var listed struct {
		Tokens []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Tokens) < 2 {
		t.Fatalf("listed tokens = %d, want bootstrap plus created token", len(listed.Tokens))
	}

	req, _ = http.NewRequest("DELETE", srv.URL+"/api/mcp/tokens/"+url.PathEscape(created.Record.ID), nil)
	req.Header.Set("Authorization", "Bearer "+setupBody.BootstrapToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("mcp with revoked token status = %d, want 401", resp.StatusCode)
	}
}

func TestTrustedTailscaleDeviceBypassesBearerAuth(t *testing.T) {
	t.Setenv("DUFFEL_AUTH_ENABLED", "1")
	t.Setenv("DUFFEL_SETUP_TOKEN", "setup-secret")
	dir := t.TempDir()
	store, err := storage.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	router := api.NewRouter(store, func() *search.Searcher { return nil }, nil, dir)

	setupReq := `{"setupToken":"setup-secret","username":"owner","password":"password123"}`
	req, _ := http.NewRequest(http.MethodPost, "/oauth/setup", strings.NewReader(setupReq))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want 201; body=%s", resp.Code, strings.TrimSpace(resp.Body.String()))
	}
	var setupBody struct {
		BootstrapToken string `json:"bootstrap_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setupBody); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}

	req, _ = http.NewRequest(http.MethodPost, "/api/mcp/trusted-devices", strings.NewReader(`{"ip":"100.76.168.20","label":"test laptop"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+setupBody.BootstrapToken)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("trust device status = %d, want 201; body=%s", resp.Code, strings.TrimSpace(resp.Body.String()))
	}

	req, _ = http.NewRequest(http.MethodGet, "/api/fs/", nil)
	req.RemoteAddr = "100.76.168.20:51234"
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("trusted device fs status = %d, want 200; body=%s", resp.Code, strings.TrimSpace(resp.Body.String()))
	}

	req, _ = http.NewRequest(http.MethodGet, "/api/fs/", nil)
	req.RemoteAddr = "192.168.1.20:51234"
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("untrusted fs status = %d, want 401; body=%s", resp.Code, strings.TrimSpace(resp.Body.String()))
	}
}

func TestForwardedOriginUsedForOAuthMetadata(t *testing.T) {
	srv, _ := setupAuthServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/.well-known/oauth-authorization-server", nil)
	req.Host = "localhost:4386"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "duffel.tailnet.ts.net")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorization server status = %d, want 200", resp.StatusCode)
	}

	var authz map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&authz); err != nil {
		t.Fatalf("decode authorization server response: %v", err)
	}
	if got := authz["issuer"]; got != "https://duffel.tailnet.ts.net" {
		t.Fatalf("issuer = %v, want %q", got, "https://duffel.tailnet.ts.net")
	}
	if got := authz["authorization_endpoint"]; got != "https://duffel.tailnet.ts.net/oauth/authorize" {
		t.Fatalf("authorization_endpoint = %v, want %q", got, "https://duffel.tailnet.ts.net/oauth/authorize")
	}

	req, _ = http.NewRequest("GET", srv.URL+"/api/fs/", nil)
	req.Host = "localhost:4386"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "duffel.tailnet.ts.net")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected API status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, `resource_metadata="https://duffel.tailnet.ts.net/.well-known/oauth-protected-resource"`) {
		t.Fatalf("WWW-Authenticate = %q, want forwarded resource metadata URL", got)
	}
}

func oauthLoginOwner(t *testing.T, baseURL string) string {
	t.Helper()

	registerBody := `{"client_name":"itest","redirect_uris":["https://example.com/callback"],"token_endpoint_auth_method":"none"}`
	resp, err := http.Post(baseURL+"/oauth/register", "application/json", strings.NewReader(registerBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("register status = %d, want 201; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if reg.ClientID == "" {
		t.Fatal("client_id missing")
	}

	challenge := "plain-verifier"
	form := url.Values{}
	form.Set("response_type", "code")
	form.Set("client_id", reg.ClientID)
	form.Set("redirect_uri", "https://example.com/callback")
	form.Set("scope", "duffel.full_access")
	form.Set("state", "abc")
	form.Set("code_challenge", challenge)
	form.Set("code_challenge_method", "plain")
	form.Set("username", "owner")
	form.Set("password", "password123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err = client.PostForm(baseURL+"/oauth/authorize", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize status = %d, want 302; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("missing Location header")
	}
	locURL, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	code := locURL.Query().Get("code")
	if code == "" {
		t.Fatalf("missing code in redirect location: %s", loc)
	}

	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", code)
	tokenForm.Set("client_id", reg.ClientID)
	tokenForm.Set("redirect_uri", "https://example.com/callback")
	tokenForm.Set("code_verifier", challenge)
	resp, err = http.PostForm(baseURL+"/oauth/token", tokenForm)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status = %d, want 200; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Fatal("missing access token")
	}
	return tokenResp.AccessToken
}
