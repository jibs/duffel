package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultScope                = "duffel.full_access"
	defaultAccessTokenTTL       = time.Hour
	defaultRefreshTokenTTL      = 30 * 24 * time.Hour
	defaultAuthorizationCodeTTL = 5 * time.Minute

	builtinCLIClientID    = "duffel-cli"
	builtinCLIClientName  = "Duffel CLI"
	builtinCLIRedirectURI = "urn:ietf:wg:oauth:2.0:oob"

	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
	tokenTypePAT     = "pat"
)

var (
	ErrAuthDisabled       = errors.New("auth is disabled")
	ErrAlreadyConfigured  = errors.New("owner account already configured")
	ErrSetupTokenRequired = errors.New("setup token is required")
	ErrInvalidSetupToken  = errors.New("invalid setup token")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidClient      = errors.New("invalid client")
	ErrInvalidGrant       = errors.New("invalid grant")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidRedirectURI = errors.New("invalid redirect URI")
)

type Config struct {
	Enabled         bool
	SetupToken      string
	Issuer          string
	Scope           string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AuthCodeTTL     time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		Enabled:         parseBoolEnvDefault("DUFFEL_AUTH_ENABLED", true),
		SetupToken:      strings.TrimSpace(os.Getenv("DUFFEL_SETUP_TOKEN")),
		Issuer:          strings.TrimSpace(os.Getenv("DUFFEL_AUTH_ISSUER")),
		Scope:           envOr("DUFFEL_AUTH_SCOPE", defaultScope),
		AccessTokenTTL:  parseDurationEnvDefault("DUFFEL_AUTH_ACCESS_TTL", defaultAccessTokenTTL),
		RefreshTokenTTL: parseDurationEnvDefault("DUFFEL_AUTH_REFRESH_TTL", defaultRefreshTokenTTL),
		AuthCodeTTL:     parseDurationEnvDefault("DUFFEL_AUTH_CODE_TTL", defaultAuthorizationCodeTTL),
	}
	if cfg.Scope == "" {
		cfg.Scope = defaultScope
	}
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = defaultAccessTokenTTL
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = defaultRefreshTokenTTL
	}
	if cfg.AuthCodeTTL <= 0 {
		cfg.AuthCodeTTL = defaultAuthorizationCodeTTL
	}
	return cfg
}

type Service struct {
	db  *sql.DB
	cfg Config
}

type Client struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	AuthMethod   string   `json:"token_endpoint_auth_method"`
}

type AuthCodeParams struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

type TokenInfo struct {
	Type    string
	Scope   string
	ValidTo time.Time
}

type PATRecord struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	ClientID  string    `json:"client_id"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Revoked   bool      `json:"revoked"`
}

type TrustedDevice struct {
	IP        string    `json:"ip"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
}

func New(dataRoot string, cfg Config) (*Service, error) {
	svc := &Service{cfg: cfg}
	if !cfg.Enabled {
		return svc, nil
	}
	authDir := filepath.Join(dataRoot, ".auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		return nil, fmt.Errorf("create auth dir: %w", err)
	}

	dbPath := filepath.Join(authDir, "auth.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}
	svc.db = db

	if err := svc.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := svc.ensureBuiltinCLIClient(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return svc, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

func (s *Service) Scope() string {
	if s == nil || strings.TrimSpace(s.cfg.Scope) == "" {
		return defaultScope
	}
	return strings.TrimSpace(s.cfg.Scope)
}

func BuiltinCLIRedirectURI() string {
	return builtinCLIRedirectURI
}

func (s *Service) Issuer(baseURL string) string {
	if s != nil && strings.TrimSpace(s.cfg.Issuer) != "" {
		return strings.TrimRight(strings.TrimSpace(s.cfg.Issuer), "/")
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (s *Service) IsConfigured(ctx context.Context) (bool, error) {
	if !s.Enabled() {
		return false, ErrAuthDisabled
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM owners`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Service) CreateOwner(ctx context.Context, setupToken, username, password string) (string, error) {
	if !s.Enabled() {
		return "", ErrAuthDisabled
	}
	if strings.TrimSpace(s.cfg.SetupToken) == "" {
		return "", ErrSetupTokenRequired
	}
	if strings.TrimSpace(setupToken) == "" {
		return "", ErrSetupTokenRequired
	}
	if subtleCompare(strings.TrimSpace(setupToken), strings.TrimSpace(s.cfg.SetupToken)) == 0 {
		return "", ErrInvalidSetupToken
	}

	username = strings.TrimSpace(username)
	if username == "" {
		return "", fmt.Errorf("username is required")
	}
	if len(password) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}

	configured, err := s.IsConfigured(ctx)
	if err != nil {
		return "", err
	}
	if configured {
		return "", ErrAlreadyConfigured
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return "", err
	}
	bootstrapToken, err := randomToken("dpat_")
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO owners (id, username, password_hash, created_at) VALUES (1, ?, ?, ?)`,
		username, passwordHash, now,
	); err != nil {
		return "", err
	}

	if err := s.insertTokenTx(ctx, tx, bootstrapToken, tokenTypePAT, builtinCLIClientID, s.Scope(), time.Time{}); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return bootstrapToken, nil
}

func (s *Service) AuthenticateOwner(ctx context.Context, username, password string) (bool, error) {
	if !s.Enabled() {
		return false, ErrAuthDisabled
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return false, nil
	}

	var storedHash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM owners WHERE username = ?`, username).Scan(&storedHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return verifyPassword(password, storedHash), nil
}

func (s *Service) RegisterPublicClient(ctx context.Context, name string, redirectURIs []string) (*Client, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	redirects := make([]string, 0, len(redirectURIs))
	seen := make(map[string]struct{}, len(redirectURIs))
	for _, raw := range redirectURIs {
		r := strings.TrimSpace(raw)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		if err := validateRedirectURI(r); err != nil {
			return nil, err
		}
		seen[r] = struct{}{}
		redirects = append(redirects, r)
	}
	if len(redirects) == 0 {
		return nil, ErrInvalidRedirectURI
	}
	clientID, err := randomToken("dc_")
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "Duffel Dynamic Client"
	}
	redirectJSON, err := json.Marshal(redirects)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Unix()

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris, auth_method, created_at) VALUES (?, ?, ?, 'none', ?)`,
		clientID, name, string(redirectJSON), now,
	); err != nil {
		return nil, err
	}

	return &Client{ID: clientID, Name: name, RedirectURIs: redirects, AuthMethod: "none"}, nil
}

func (s *Service) GetClient(ctx context.Context, clientID string) (*Client, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, ErrInvalidClient
	}

	var name string
	var redirectJSON string
	var authMethod string
	err := s.db.QueryRowContext(ctx,
		`SELECT client_name, redirect_uris, auth_method FROM oauth_clients WHERE client_id = ?`,
		clientID,
	).Scan(&name, &redirectJSON, &authMethod)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidClient
		}
		return nil, err
	}

	var redirects []string
	if err := json.Unmarshal([]byte(redirectJSON), &redirects); err != nil {
		return nil, err
	}
	return &Client{ID: clientID, Name: name, RedirectURIs: redirects, AuthMethod: authMethod}, nil
}

func (s *Service) ValidateClientRedirectURI(ctx context.Context, clientID, redirectURI string) error {
	client, err := s.GetClient(ctx, clientID)
	if err != nil {
		return err
	}
	redirectURI = strings.TrimSpace(redirectURI)
	for _, allowed := range client.RedirectURIs {
		if subtleCompare(allowed, redirectURI) == 1 {
			return nil
		}
	}
	return ErrInvalidRedirectURI
}

func (s *Service) CreateAuthorizationCode(ctx context.Context, p AuthCodeParams) (string, error) {
	if !s.Enabled() {
		return "", ErrAuthDisabled
	}
	if err := s.ValidateClientRedirectURI(ctx, p.ClientID, p.RedirectURI); err != nil {
		return "", err
	}
	p.CodeChallenge = strings.TrimSpace(p.CodeChallenge)
	if p.CodeChallenge == "" {
		return "", ErrInvalidGrant
	}
	p.CodeChallengeMethod = strings.TrimSpace(p.CodeChallengeMethod)
	if p.CodeChallengeMethod == "" {
		p.CodeChallengeMethod = "plain"
	}
	if p.CodeChallengeMethod != "plain" && p.CodeChallengeMethod != "S256" {
		return "", ErrInvalidGrant
	}
	if strings.TrimSpace(p.Scope) == "" {
		p.Scope = s.Scope()
	}

	code, err := randomToken("dcode_")
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.AuthCodeTTL).Unix()

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_codes (
			code_hash, client_id, redirect_uri, scope,
			code_challenge, code_challenge_method,
			expires_at, used, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		hashToken(code), strings.TrimSpace(p.ClientID), strings.TrimSpace(p.RedirectURI), strings.TrimSpace(p.Scope),
		p.CodeChallenge, p.CodeChallengeMethod,
		expiresAt, now.Unix(),
	); err != nil {
		return "", err
	}

	return code, nil
}

func (s *Service) ExchangeAuthorizationCode(ctx context.Context, code, clientID, redirectURI, codeVerifier string) (*TokenResponse, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	code = strings.TrimSpace(code)
	clientID = strings.TrimSpace(clientID)
	redirectURI = strings.TrimSpace(redirectURI)
	if code == "" || clientID == "" || redirectURI == "" || codeVerifier == "" {
		return nil, ErrInvalidGrant
	}

	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var rowClientID string
	var rowRedirect string
	var scope string
	var challenge string
	var challengeMethod string
	var expiresAt int64
	var used int
	err = tx.QueryRowContext(ctx,
		`SELECT client_id, redirect_uri, scope, code_challenge, code_challenge_method, expires_at, used
		 FROM auth_codes WHERE code_hash = ?`,
		hashToken(code),
	).Scan(&rowClientID, &rowRedirect, &scope, &challenge, &challengeMethod, &expiresAt, &used)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidGrant
		}
		return nil, err
	}
	if used != 0 || expiresAt < now {
		return nil, ErrInvalidGrant
	}
	if subtleCompare(rowClientID, clientID) == 0 || subtleCompare(rowRedirect, redirectURI) == 0 {
		return nil, ErrInvalidGrant
	}
	if !verifyPKCE(codeVerifier, challenge, challengeMethod) {
		return nil, ErrInvalidGrant
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_codes SET used = 1 WHERE code_hash = ?`, hashToken(code)); err != nil {
		return nil, err
	}

	resp, err := s.issueTokenPairTx(ctx, tx, rowClientID, scope)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *Service) RefreshToken(ctx context.Context, refreshToken, clientID string) (*TokenResponse, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	refreshToken = strings.TrimSpace(refreshToken)
	clientID = strings.TrimSpace(clientID)
	if refreshToken == "" {
		return nil, ErrInvalidGrant
	}

	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var tokenClientID string
	var scope string
	var expiresAt int64
	var revoked int
	err = tx.QueryRowContext(ctx,
		`SELECT client_id, scope, expires_at, revoked FROM oauth_tokens WHERE token_hash = ? AND token_type = ?`,
		hashToken(refreshToken), tokenTypeRefresh,
	).Scan(&tokenClientID, &scope, &expiresAt, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidGrant
		}
		return nil, err
	}
	if revoked != 0 || expiresAt < now {
		return nil, ErrInvalidGrant
	}
	if clientID != "" && subtleCompare(clientID, tokenClientID) == 0 {
		return nil, ErrInvalidGrant
	}

	if _, err := tx.ExecContext(ctx, `UPDATE oauth_tokens SET revoked = 1 WHERE token_hash = ? AND token_type = ?`, hashToken(refreshToken), tokenTypeRefresh); err != nil {
		return nil, err
	}

	resp, err := s.issueTokenPairTx(ctx, tx, tokenClientID, scope)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *Service) ValidateBearerToken(ctx context.Context, bearerToken string) (*TokenInfo, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	bearerToken = strings.TrimSpace(bearerToken)
	if bearerToken == "" {
		return nil, ErrUnauthorized
	}

	var tokenType string
	var scope string
	var expiresAt int64
	var revoked int
	err := s.db.QueryRowContext(ctx,
		`SELECT token_type, scope, expires_at, revoked
		 FROM oauth_tokens WHERE token_hash = ?`,
		hashToken(bearerToken),
	).Scan(&tokenType, &scope, &expiresAt, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if revoked != 0 {
		return nil, ErrUnauthorized
	}
	if tokenType != tokenTypePAT {
		if expiresAt > 0 && time.Unix(expiresAt, 0).UTC().Before(time.Now().UTC()) {
			return nil, ErrUnauthorized
		}
	}

	validTo := time.Time{}
	if expiresAt > 0 {
		validTo = time.Unix(expiresAt, 0).UTC()
	}
	return &TokenInfo{Type: tokenType, Scope: scope, ValidTo: validTo}, nil
}

func (s *Service) CreatePAT(ctx context.Context, label string) (string, *PATRecord, error) {
	if !s.Enabled() {
		return "", nil, ErrAuthDisabled
	}
	token, err := randomToken("dpat_")
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	hash := hashToken(token)
	label = strings.TrimSpace(label)
	if label == "" {
		label = "MCP connector"
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (token_hash, token_type, client_id, scope, expires_at, revoked, created_at, label)
		 VALUES (?, ?, ?, ?, 0, 0, ?, ?)`,
		hash, tokenTypePAT, "duffel-mcp", s.Scope(), now.Unix(), label,
	); err != nil {
		return "", nil, err
	}

	return token, &PATRecord{
		ID:        hash,
		Label:     label,
		ClientID:  "duffel-mcp",
		Scope:     s.Scope(),
		CreatedAt: now,
		Revoked:   false,
	}, nil
}

func (s *Service) ListPATs(ctx context.Context) ([]PATRecord, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT token_hash, COALESCE(label, ''), client_id, scope, expires_at, revoked, created_at
		 FROM oauth_tokens
		 WHERE token_type = ?
		 ORDER BY created_at DESC`,
		tokenTypePAT,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	records := []PATRecord{}
	for rows.Next() {
		var rec PATRecord
		var expiresAt int64
		var createdAt int64
		var revoked int
		if err := rows.Scan(&rec.ID, &rec.Label, &rec.ClientID, &rec.Scope, &expiresAt, &revoked, &createdAt); err != nil {
			return nil, err
		}
		rec.CreatedAt = time.Unix(createdAt, 0).UTC()
		if expiresAt > 0 {
			rec.ExpiresAt = time.Unix(expiresAt, 0).UTC()
		}
		rec.Revoked = revoked != 0
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Service) RevokeToken(ctx context.Context, id string) error {
	if !s.Enabled() {
		return ErrAuthDisabled
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrUnauthorized
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE oauth_tokens SET revoked = 1 WHERE token_hash = ? AND token_type = ?`,
		id, tokenTypePAT,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) ListTrustedDevices(ctx context.Context) ([]TrustedDevice, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ip, label, created_at, revoked
		 FROM trusted_tailscale_devices
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	devices := []TrustedDevice{}
	for rows.Next() {
		var device TrustedDevice
		var createdAt int64
		var revoked int
		if err := rows.Scan(&device.IP, &device.Label, &createdAt, &revoked); err != nil {
			return nil, err
		}
		device.CreatedAt = time.Unix(createdAt, 0).UTC()
		device.Revoked = revoked != 0
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return devices, nil
}

func (s *Service) TrustDevice(ctx context.Context, ip, label string) (*TrustedDevice, error) {
	if !s.Enabled() {
		return nil, ErrAuthDisabled
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, fmt.Errorf("ip is required")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = ip
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO trusted_tailscale_devices (ip, label, created_at, revoked)
		 VALUES (?, ?, ?, 0)
		 ON CONFLICT(ip) DO UPDATE SET label = excluded.label, revoked = 0`,
		ip, label, now.Unix(),
	); err != nil {
		return nil, err
	}
	return &TrustedDevice{IP: ip, Label: label, CreatedAt: now, Revoked: false}, nil
}

func (s *Service) RevokeTrustedDevice(ctx context.Context, ip string) error {
	if !s.Enabled() {
		return ErrAuthDisabled
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ErrUnauthorized
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE trusted_tailscale_devices SET revoked = 1 WHERE ip = ?`,
		ip,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) IsTrustedDevice(ctx context.Context, ip string) (bool, error) {
	if !s.Enabled() {
		return false, ErrAuthDisabled
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false, nil
	}
	var revoked int
	err := s.db.QueryRowContext(ctx,
		`SELECT revoked FROM trusted_tailscale_devices WHERE ip = ?`,
		ip,
	).Scan(&revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return revoked == 0, nil
}

func (s *Service) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS owners (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_clients (
	client_id TEXT PRIMARY KEY,
	client_name TEXT NOT NULL,
	redirect_uris TEXT NOT NULL,
	auth_method TEXT NOT NULL DEFAULT 'none',
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_codes (
	code_hash TEXT PRIMARY KEY,
	client_id TEXT NOT NULL,
	redirect_uri TEXT NOT NULL,
	scope TEXT NOT NULL,
	code_challenge TEXT NOT NULL,
	code_challenge_method TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	used INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_tokens (
	token_hash TEXT PRIMARY KEY,
	token_type TEXT NOT NULL,
	client_id TEXT NOT NULL,
	scope TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	revoked INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	label TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS trusted_tailscale_devices (
	ip TEXT PRIMARY KEY,
	label TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	revoked INTEGER NOT NULL DEFAULT 0
);
`); err != nil {
		return fmt.Errorf("init auth schema: %w", err)
	}
	if err := s.ensureTokenLabelColumn(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) ensureTokenLabelColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(oauth_tokens)`)
	if err != nil {
		return fmt.Errorf("inspect oauth_tokens schema: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan oauth_tokens schema: %w", err)
		}
		if name == "label" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect oauth_tokens schema: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `ALTER TABLE oauth_tokens ADD COLUMN label TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add oauth_tokens label column: %w", err)
	}
	return nil
}

func (s *Service) ensureBuiltinCLIClient(ctx context.Context) error {
	redirectJSON, err := json.Marshal([]string{builtinCLIRedirectURI})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO oauth_clients (client_id, client_name, redirect_uris, auth_method, created_at) VALUES (?, ?, ?, 'none', ?)`,
		builtinCLIClientID, builtinCLIClientName, string(redirectJSON), time.Now().UTC().Unix(),
	)
	return err
}

func (s *Service) issueTokenPairTx(ctx context.Context, tx *sql.Tx, clientID, scope string) (*TokenResponse, error) {
	if strings.TrimSpace(scope) == "" {
		scope = s.Scope()
	}
	accessToken, err := randomToken("dat_")
	if err != nil {
		return nil, err
	}
	refreshToken, err := randomToken("drt_")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	accessExpiry := now.Add(s.cfg.AccessTokenTTL)
	refreshExpiry := now.Add(s.cfg.RefreshTokenTTL)

	if err := s.insertTokenTx(ctx, tx, accessToken, tokenTypeAccess, clientID, scope, accessExpiry); err != nil {
		return nil, err
	}
	if err := s.insertTokenTx(ctx, tx, refreshToken, tokenTypeRefresh, clientID, scope, refreshExpiry); err != nil {
		return nil, err
	}

	return &TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scope,
	}, nil
}

func (s *Service) insertTokenTx(ctx context.Context, tx *sql.Tx, token, tokenType, clientID, scope string, expiresAt time.Time) error {
	expiryUnix := int64(0)
	if !expiresAt.IsZero() {
		expiryUnix = expiresAt.UTC().Unix()
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO oauth_tokens (token_hash, token_type, client_id, scope, expires_at, revoked, created_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		hashToken(token), tokenType, strings.TrimSpace(clientID), strings.TrimSpace(scope), expiryUnix, time.Now().UTC().Unix(),
	)
	return err
}

func validateRedirectURI(redirectURI string) error {
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		return ErrInvalidRedirectURI
	}
	if redirectURI == builtinCLIRedirectURI {
		return nil
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		return ErrInvalidRedirectURI
	}
	if u.Scheme == "" || u.Host == "" {
		return ErrInvalidRedirectURI
	}
	return nil
}

func verifyPKCE(verifier, challenge, method string) bool {
	verifier = strings.TrimSpace(verifier)
	challenge = strings.TrimSpace(challenge)
	if verifier == "" || challenge == "" {
		return false
	}
	switch strings.TrimSpace(method) {
	case "", "plain":
		return subtleCompare(verifier, challenge) == 1
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		expected := base64.RawURLEncoding.EncodeToString(sum[:])
		return subtleCompare(expected, challenge) == 1
	default:
		return false
	}
}

func randomToken(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func parseBoolEnvDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseDurationEnvDefault(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}
