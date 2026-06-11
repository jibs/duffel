package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"duffel/src/backend/internal/auth"
)

type oauthAuthorizeParams struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

func handleOAuthProtectedResource(authService *auth.Service, resourcePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		issuer := authService.Issuer(requestOrigin(r))
		resource := strings.TrimRight(issuer, "/") + resourcePath
		writeJSON(w, http.StatusOK, map[string]any{
			"resource":                 resource,
			"authorization_servers":    []string{issuer},
			"bearer_methods_supported": []string{"header"},
			"scopes_supported":         []string{authService.Scope()},
		})
	}
}

func handleOAuthAuthorizationServer(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		issuer := authService.Issuer(requestOrigin(r))
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/oauth/authorize",
			"token_endpoint":                        issuer + "/oauth/token",
			"registration_endpoint":                 issuer + "/oauth/register",
			"scopes_supported":                      []string{authService.Scope()},
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"none"},
			"code_challenge_methods_supported":      []string{"S256", "plain"},
		})
	}
}

func handleOAuthSetup(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}

		switch r.Method {
		case http.MethodGet:
			configured, err := authService.IsConfigured(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"configured": configured})
			return
		case http.MethodPost:
			var body struct {
				SetupToken string `json:"setupToken"`
				Username   string `json:"username"`
				Password   string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body", r.URL.Path)
				return
			}
			bootstrapPAT, err := authService.CreateOwner(r.Context(), body.SetupToken, body.Username, body.Password)
			if err != nil {
				switch {
				case errors.Is(err, auth.ErrSetupTokenRequired), errors.Is(err, auth.ErrInvalidSetupToken):
					writeError(w, http.StatusForbidden, err.Error(), r.URL.Path)
				case errors.Is(err, auth.ErrAlreadyConfigured):
					writeError(w, http.StatusConflict, err.Error(), r.URL.Path)
				default:
					writeError(w, http.StatusBadRequest, err.Error(), r.URL.Path)
				}
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{
				"status":          "configured",
				"bootstrap_token": bootstrapPAT,
			})
			return
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleOAuthRegister(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			ClientName              string   `json:"client_name"`
			RedirectURIs            []string `json:"redirect_uris"`
			TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON body")
			return
		}
		if body.TokenEndpointAuthMethod != "" && body.TokenEndpointAuthMethod != "none" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "only token_endpoint_auth_method=none is supported")
			return
		}

		client, err := authService.RegisterPublicClient(r.Context(), strings.TrimSpace(body.ClientName), body.RedirectURIs)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"client_id":                  client.ID,
			"client_name":                client.Name,
			"redirect_uris":              client.RedirectURIs,
			"token_endpoint_auth_method": "none",
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"scope":                      authService.Scope(),
		})
	}
}

func handleOAuthAuthorizeGet(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		configured, err := authService.IsConfigured(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), r.URL.Path)
			return
		}
		if !configured {
			writeOAuthError(w, http.StatusPreconditionFailed, "access_denied", "owner setup is not complete")
			return
		}

		params, err := validateAuthorizeParams(authService, r.Context(), r.URL.Query())
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		renderAuthorizeForm(w, params)
	}
}

func handleOAuthAuthorizePost(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
			return
		}

		params, err := validateAuthorizeParams(authService, r.Context(), r.Form)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		ok, err := authService.AuthenticateOwner(r.Context(), username, password)
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
		if !ok {
			writeOAuthError(w, http.StatusUnauthorized, "access_denied", "invalid credentials")
			return
		}

		code, err := authService.CreateAuthorizationCode(r.Context(), auth.AuthCodeParams{
			ClientID:            params.ClientID,
			RedirectURI:         params.RedirectURI,
			Scope:               params.Scope,
			CodeChallenge:       params.CodeChallenge,
			CodeChallengeMethod: params.CodeChallengeMethod,
		})
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		if params.RedirectURI == auth.BuiltinCLIRedirectURI() {
			renderAuthorizationCodePage(w, code, params.State)
			return
		}
		redirectURL, err := url.Parse(params.RedirectURI)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid redirect URI")
			return
		}
		query := redirectURL.Query()
		query.Set("code", code)
		if params.State != "" {
			query.Set("state", params.State)
		}
		redirectURL.RawQuery = query.Encode()
		http.Redirect(w, r, redirectURL.String(), http.StatusFound)
	}
}

func handleOAuthToken(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authService == nil || !authService.Enabled() {
			writeError(w, http.StatusNotFound, "auth disabled", r.URL.Path)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
			return
		}

		grantType := strings.TrimSpace(r.FormValue("grant_type"))
		switch grantType {
		case "authorization_code":
			resp, err := authService.ExchangeAuthorizationCode(
				r.Context(),
				r.FormValue("code"),
				r.FormValue("client_id"),
				r.FormValue("redirect_uri"),
				r.FormValue("code_verifier"),
			)
			if err != nil {
				writeOAuthTokenError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, resp)
		case "refresh_token":
			resp, err := authService.RefreshToken(
				r.Context(),
				r.FormValue("refresh_token"),
				r.FormValue("client_id"),
			)
			if err != nil {
				writeOAuthTokenError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, resp)
		default:
			writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
		}
	}
}

func validateAuthorizeParams(authService *auth.Service, ctx context.Context, values url.Values) (*oauthAuthorizeParams, error) {
	params := &oauthAuthorizeParams{
		ResponseType:        strings.TrimSpace(values.Get("response_type")),
		ClientID:            strings.TrimSpace(values.Get("client_id")),
		RedirectURI:         strings.TrimSpace(values.Get("redirect_uri")),
		Scope:               strings.TrimSpace(values.Get("scope")),
		State:               strings.TrimSpace(values.Get("state")),
		CodeChallenge:       strings.TrimSpace(values.Get("code_challenge")),
		CodeChallengeMethod: strings.TrimSpace(values.Get("code_challenge_method")),
	}

	if params.ResponseType != "code" {
		return nil, fmt.Errorf("response_type must be code")
	}
	if params.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if params.RedirectURI == "" {
		return nil, fmt.Errorf("redirect_uri is required")
	}
	if params.CodeChallenge == "" {
		return nil, fmt.Errorf("code_challenge is required")
	}
	if params.CodeChallengeMethod == "" {
		params.CodeChallengeMethod = "plain"
	}
	if params.CodeChallengeMethod != "plain" && params.CodeChallengeMethod != "S256" {
		return nil, fmt.Errorf("unsupported code_challenge_method")
	}
	if params.Scope == "" {
		params.Scope = authService.Scope()
	}
	if err := authService.ValidateClientRedirectURI(ctx, params.ClientID, params.RedirectURI); err != nil {
		return nil, err
	}
	return params, nil
}

func writeOAuthError(w http.ResponseWriter, status int, code string, description string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

func writeOAuthTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidGrant):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
	case errors.Is(err, auth.ErrInvalidClient):
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
	default:
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
	}
}

func renderAuthorizeForm(w http.ResponseWriter, params *oauthAuthorizeParams) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Duffel Login</title></head>
<body>
<h1>Duffel Authorization</h1>
<p>Sign in as the owner account to approve this client.</p>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="response_type" value="%s">
<input type="hidden" name="client_id" value="%s">
<input type="hidden" name="redirect_uri" value="%s">
<input type="hidden" name="scope" value="%s">
<input type="hidden" name="state" value="%s">
<input type="hidden" name="code_challenge" value="%s">
<input type="hidden" name="code_challenge_method" value="%s">
<label>Username <input name="username" autocomplete="username"></label><br>
<label>Password <input name="password" type="password" autocomplete="current-password"></label><br>
<button type="submit">Authorize</button>
</form>
</body>
</html>`,
		html.EscapeString(params.ResponseType),
		html.EscapeString(params.ClientID),
		html.EscapeString(params.RedirectURI),
		html.EscapeString(params.Scope),
		html.EscapeString(params.State),
		html.EscapeString(params.CodeChallenge),
		html.EscapeString(params.CodeChallengeMethod),
	)
}

func renderAuthorizationCodePage(w http.ResponseWriter, code, state string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Duffel Authorization Code</title></head>
<body>
<h1>Authorization Complete</h1>
<p>Copy this code back to your terminal:</p>
<pre>%s</pre>
<p>State: %s</p>
</body>
</html>`, html.EscapeString(code), html.EscapeString(state))
}
