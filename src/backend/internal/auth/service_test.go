package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(t.TempDir(), Config{
		Enabled:         true,
		SetupToken:      "setup-secret",
		Scope:           "duffel.full_access",
		AccessTokenTTL:  5 * time.Minute,
		RefreshTokenTTL: 24 * time.Hour,
		AuthCodeTTL:     5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

func TestOwnerSetupAndLogin(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateOwner(ctx, "bad", "owner", "password123"); err == nil {
		t.Fatal("expected setup token validation error")
	}
	pat, err := svc.CreateOwner(ctx, "setup-secret", "owner", "password123")
	if err != nil {
		t.Fatalf("CreateOwner: %v", err)
	}
	if pat == "" {
		t.Fatal("expected bootstrap token")
	}
	ok, err := svc.AuthenticateOwner(ctx, "owner", "password123")
	if err != nil {
		t.Fatalf("AuthenticateOwner: %v", err)
	}
	if !ok {
		t.Fatal("expected credentials to validate")
	}
	if _, err := svc.ValidateBearerToken(ctx, pat); err != nil {
		t.Fatalf("ValidateBearerToken(PAT): %v", err)
	}
}

func TestOAuthCodeAndRefreshFlow(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if _, err := svc.CreateOwner(ctx, "setup-secret", "owner", "password123"); err != nil {
		t.Fatalf("CreateOwner: %v", err)
	}

	client, err := svc.RegisterPublicClient(ctx, "ChatGPT", []string{"https://example.com/callback"})
	if err != nil {
		t.Fatalf("RegisterPublicClient: %v", err)
	}

	verifier := "my-code-verifier-123"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	code, err := svc.CreateAuthorizationCode(ctx, AuthCodeParams{
		ClientID:            client.ID,
		RedirectURI:         "https://example.com/callback",
		Scope:               svc.Scope(),
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("CreateAuthorizationCode: %v", err)
	}

	tokens, err := svc.ExchangeAuthorizationCode(ctx, code, client.ID, "https://example.com/callback", verifier)
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens")
	}
	if _, err := svc.ValidateBearerToken(ctx, tokens.AccessToken); err != nil {
		t.Fatalf("ValidateBearerToken(access): %v", err)
	}

	refreshed, err := svc.RefreshToken(ctx, tokens.RefreshToken, client.ID)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatal("expected refreshed token pair")
	}
	if refreshed.AccessToken == tokens.AccessToken {
		t.Fatal("expected access token rotation")
	}
}
