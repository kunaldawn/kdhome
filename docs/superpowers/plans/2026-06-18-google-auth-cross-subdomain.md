# Google OAuth & Cross-Subdomain Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gate kunaldawn.com behind Google sign-in, issuing a stateless `.kunaldawn.com`-scoped HS256 session cookie that subdomains verify locally; unauthenticated requests redirect to `/login?redirect=<url>` and back after login.

**Architecture:** A new `token.go` mints/verifies HS256 JWTs (stdlib only). A new `auth.go` holds the env config, the Google OAuth handlers (via `golang.org/x/oauth2`), a gate middleware, and a themed self-contained `/login` page. `main.go` registers the auth routes and installs the gate as `maintenance( securityHeaders( authGate( mux ) ) )`. Auth is env-gated (`AUTH_ENABLED`, default off) and fail-closed when enabled without secrets. Plus content edits removing "no user accounts".

**Tech Stack:** Go 1.22, `net/http`, stdlib `crypto/hmac`+`crypto/sha256` (JWT), `golang.org/x/oauth2` (Google code→token exchange), vanilla HTML for the login page.

## Global Constraints

- Go version floor: **Go 1.22** (Dockerfile `golang:1.22-alpine`). Do NOT let `go get` bump the `go 1.22` directive — use `GOTOOLCHAIN=local` and revert the directive if changed.
- Only ONE new dependency: `golang.org/x/oauth2`. Do NOT import `golang.org/x/oauth2/google` (avoids its transitive deps) — hardcode Google's endpoint instead.
- Run tests with `CGO_ENABLED=1 go test ./...` (the project uses mattn/go-sqlite3 which needs cgo).
- Env truthy parsing for `AUTH_ENABLED`: case-insensitive `1`, `true`, `on`, `yes`. Anything else off.
- Session cookie: HS256 JWT, claims `{sub,email,iat,exp,iss}`, `iss="kunaldawn.com"`; cookie `Domain=AUTH_COOKIE_DOMAIN` (default `.kunaldawn.com`), `Name=AUTH_COOKIE_NAME` (default `kd_session`), `Path=/`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Max-Age=int(SessionTTL.Seconds())`.
- Defaults: `AUTH_BASE_URL=https://kunaldawn.com`, `AUTH_SESSION_TTL=720h`.
- Open-redirect guard: accept only `https` targets whose host equals `kunaldawn.com` or ends with `.kunaldawn.com` (base = `TrimPrefix(AUTH_COOKIE_DOMAIN, ".")`), or root-relative `/path` (not `//`); else fall back to `AUTH_BASE_URL`.
- Public allowlist (bypass gate), EXACT paths: `/login`, `/auth/google/start`, `/auth/google/callback`, `/logout`, `/robots.txt`, `/sitemap.xml`, `/llms.txt`, `/og-image.png`, `/site.webmanifest`, `/favicon.ico`, `/favicon-16x16.png`, `/favicon-32x32.png`, `/favicon-48x48.png`, `/apple-touch-icon.png`, `/android-chrome-192x192.png`, `/android-chrome-512x512.png`.
- Content rule: in the **Terms only**, "no user accounts" → "google login"; everywhere else, just remove the phrase.

---

## Task 1: Session JWT (`token.go`)

**Files:**
- Create: `token.go`
- Test: `token_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  - `var b64 = base64.RawURLEncoding`
  - `const sessionIssuer = "kunaldawn.com"`
  - `type sessionClaims struct { Subject, Email string; IssuedAt, ExpiresAt int64; Issuer string }` (JSON tags `sub,email,iat,exp,iss`)
  - `func sign(input string, secret []byte) string`
  - `func signToken(payload []byte, secret []byte) string`
  - `func verifyToken(token string, secret []byte) ([]byte, error)`
  - `func signSession(email string, ttl time.Duration, secret []byte) (string, error)`
  - `func verifySession(token string, secret []byte) (sessionClaims, error)`

- [ ] **Step 1: Write the failing test**

Create `token_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"
)

func TestSignVerifySession(t *testing.T) {
	secret := []byte("test-secret")
	tok, err := signSession("a@b.com", time.Hour, secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	c, err := verifySession(tok, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Email != "a@b.com" {
		t.Fatalf("email = %q", c.Email)
	}
	if c.Issuer != sessionIssuer {
		t.Fatalf("iss = %q, want %q", c.Issuer, sessionIssuer)
	}
}

func TestVerifyRejectsTampered(t *testing.T) {
	secret := []byte("test-secret")
	tok, _ := signSession("a@b.com", time.Hour, secret)
	parts := strings.Split(tok, ".")
	bad := parts[0] + "." + parts[1] + "x." + parts[2] // tamper payload segment
	if _, err := verifySession(bad, secret); err == nil {
		t.Fatal("tampered token must fail")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	tok, _ := signSession("a@b.com", time.Hour, []byte("secret-1"))
	if _, err := verifySession(tok, []byte("secret-2")); err == nil {
		t.Fatal("wrong secret must fail")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	secret := []byte("test-secret")
	tok, _ := signSession("a@b.com", -time.Minute, secret) // already expired
	if _, err := verifySession(tok, secret); err == nil {
		t.Fatal("expired token must fail")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if _, err := verifySession("only-one-part", []byte("s")); err == nil {
		t.Fatal("malformed must fail")
	}
	if _, err := verifySession("a.b.c.d", []byte("s")); err == nil {
		t.Fatal("4-part token must fail")
	}
}

func TestVerifyRejectsNonHS256(t *testing.T) {
	secret := []byte("s")
	header := b64.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := b64.EncodeToString([]byte(`{"email":"x@y.com","exp":99999999999}`))
	signingInput := header + "." + payload
	tok := signingInput + "." + sign(signingInput, secret) // signature is valid, but alg=none
	if _, err := verifySession(tok, secret); err == nil {
		t.Fatal("non-HS256 alg must be rejected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test ./... -run 'Session|Verify' -v`
Expected: FAIL — `undefined: signSession` etc.

- [ ] **Step 3: Write the implementation**

Create `token.go`:

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// b64 is the JWT segment encoding (base64url, no padding).
var b64 = base64.RawURLEncoding

const sessionIssuer = "kunaldawn.com"

// sessionClaims is the session JWT payload. Standard registered claim names
// are used so subdomain apps can verify the cookie with any off-the-shelf JWT
// library (HS256 + the shared AUTH_SECRET).
type sessionClaims struct {
	Subject   string `json:"sub"`
	Email     string `json:"email"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Issuer    string `json:"iss"`
}

// sign returns the base64url HMAC-SHA256 of input under secret.
func sign(input string, secret []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(input))
	return b64.EncodeToString(m.Sum(nil))
}

// signToken frames payload as an HS256 JWT and signs it.
func signToken(payload []byte, secret []byte) string {
	header := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	signingInput := header + "." + b64.EncodeToString(payload)
	return signingInput + "." + sign(signingInput, secret)
}

// verifyToken checks an HS256 JWT's signature (constant-time) and alg header,
// returning the decoded payload. It does NOT check exp — callers do.
func verifyToken(token string, secret []byte) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(sign(signingInput, secret)), []byte(parts[2])) {
		return nil, errors.New("bad signature")
	}
	hdrBytes, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("bad header encoding")
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return nil, errors.New("bad header json")
	}
	if hdr.Alg != "HS256" {
		return nil, errors.New("unexpected alg")
	}
	return b64.DecodeString(parts[1])
}

// signSession mints a session JWT for email, valid for ttl from now.
func signSession(email string, ttl time.Duration, secret []byte) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("empty signing secret")
	}
	now := time.Now()
	payload, err := json.Marshal(sessionClaims{
		Subject:   email,
		Email:     email,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		Issuer:    sessionIssuer,
	})
	if err != nil {
		return "", err
	}
	return signToken(payload, secret), nil
}

// verifySession validates a session JWT (signature, alg, expiry) and returns
// its claims.
func verifySession(token string, secret []byte) (sessionClaims, error) {
	var c sessionClaims
	payload, err := verifyToken(token, secret)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return c, errors.New("bad payload json")
	}
	if c.ExpiresAt <= time.Now().Unix() {
		return c, errors.New("token expired")
	}
	return c, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test ./... -run 'Session|Verify' -v`
Expected: PASS — all six tests green.

- [ ] **Step 5: Commit**

```bash
git add token.go token_test.go
git commit -m "feat: add stdlib HS256 session JWT sign/verify"
```

---

## Task 2: Auth config + OAuth plumbing (`auth.go` part 1)

**Files:**
- Create: `auth.go`
- Modify: `go.mod`, `go.sum` (add `golang.org/x/oauth2`)
- Test: `auth_test.go`

**Interfaces:**
- Consumes: nothing from prior tasks (uses oauth2).
- Produces:
  - `type authConfig struct { Enabled bool; ClientID, ClientSecret string; Secret []byte; BaseURL, CookieDomain, CookieName string; SessionTTL time.Duration; oauth *oauth2.Config; exchanger tokenExchanger }`
  - `func loadAuthConfig() authConfig`
  - `func (c authConfig) valid() bool`
  - `func (c authConfig) baseDomain() string`
  - `func (c authConfig) safeRedirect(raw string) string`
  - `type tokenExchanger interface { exchange(ctx context.Context, code string) (email string, verified bool, err error) }`
  - `type googleExchanger struct { cfg *oauth2.Config }` implementing it
  - `func emailFromIDToken(idToken string) (string, bool, error)`

- [ ] **Step 1: Add the dependency**

Run:
```bash
GOTOOLCHAIN=local go get golang.org/x/oauth2@v0.30.0
```
Then confirm `go.mod` still has `go 1.22` (if `go get` rewrote it to a higher version or added a `toolchain` line, revert that line manually so it reads `go 1.22`).
Expected: `go.mod` gains `require golang.org/x/oauth2 v0.30.0` (and any indirect deps); `go.sum` updated.

- [ ] **Step 2: Write the failing test**

Create `auth_test.go`:

```go
package main

import (
	"os"
	"testing"
	"time"
)

func clearAuthEnv() {
	for _, k := range []string{"AUTH_ENABLED", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
		"AUTH_SECRET", "AUTH_BASE_URL", "AUTH_COOKIE_DOMAIN", "AUTH_COOKIE_NAME", "AUTH_SESSION_TTL"} {
		os.Unsetenv(k)
	}
}

func TestLoadAuthConfigDefaults(t *testing.T) {
	clearAuthEnv()
	c := loadAuthConfig()
	if c.Enabled {
		t.Fatal("should be disabled by default")
	}
	if c.CookieName != "kd_session" {
		t.Fatalf("cookie name = %q", c.CookieName)
	}
	if c.CookieDomain != ".kunaldawn.com" {
		t.Fatalf("cookie domain = %q", c.CookieDomain)
	}
	if c.BaseURL != "https://kunaldawn.com" {
		t.Fatalf("base url = %q", c.BaseURL)
	}
	if c.SessionTTL != 720*time.Hour {
		t.Fatalf("ttl = %v", c.SessionTTL)
	}
}

func TestLoadAuthConfigEnabledRequiresSecrets(t *testing.T) {
	clearAuthEnv()
	os.Setenv("AUTH_ENABLED", "on")
	defer clearAuthEnv()
	c := loadAuthConfig()
	if !c.Enabled {
		t.Fatal("AUTH_ENABLED=on should enable")
	}
	if c.valid() {
		t.Fatal("missing secrets must be invalid")
	}
	os.Setenv("GOOGLE_CLIENT_ID", "cid")
	os.Setenv("GOOGLE_CLIENT_SECRET", "csec")
	os.Setenv("AUTH_SECRET", "shh")
	if !loadAuthConfig().valid() {
		t.Fatal("all secrets present must be valid")
	}
}

func testAuthConfig() authConfig {
	return authConfig{
		Enabled:      true,
		ClientID:     "cid",
		ClientSecret: "csec",
		Secret:       []byte("test-secret"),
		BaseURL:      "https://kunaldawn.com",
		CookieDomain: ".kunaldawn.com",
		CookieName:   "kd_session",
		SessionTTL:   time.Hour,
	}
}

func TestSafeRedirect(t *testing.T) {
	c := testAuthConfig()
	cases := []struct{ in, want string }{
		{"https://kunaldawn.com/x", "https://kunaldawn.com/x"},
		{"https://wiki.kunaldawn.com/a?b=c", "https://wiki.kunaldawn.com/a?b=c"},
		{"https://evil.com/", "https://kunaldawn.com"},
		{"http://kunaldawn.com/x", "https://kunaldawn.com"},
		{"//evil.com", "https://kunaldawn.com"},
		{"", "https://kunaldawn.com"},
		{"/dashboard", "https://kunaldawn.com/dashboard"},
	}
	for _, tc := range cases {
		if got := c.safeRedirect(tc.in); got != tc.want {
			t.Errorf("safeRedirect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEmailFromIDToken(t *testing.T) {
	payload := b64.EncodeToString([]byte(`{"email":"a@b.com","email_verified":true}`))
	email, verified, err := emailFromIDToken("h." + payload + ".s")
	if err != nil || email != "a@b.com" || !verified {
		t.Fatalf("bool form: got %q %v %v", email, verified, err)
	}
	payload2 := b64.EncodeToString([]byte(`{"email":"c@d.com","email_verified":"true"}`))
	email2, v2, _ := emailFromIDToken("h." + payload2 + ".s")
	if email2 != "c@d.com" || !v2 {
		t.Fatalf("string form: got %q %v", email2, v2)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test ./... -run 'AuthConfig|SafeRedirect|EmailFromID' -v`
Expected: FAIL — `undefined: loadAuthConfig` etc.

- [ ] **Step 4: Write the implementation**

Create `auth.go`:

```go
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// googleEndpoint is hardcoded to avoid importing golang.org/x/oauth2/google
// (and its transitive deps). These URLs are stable.
var googleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

// authConfig is the env-driven auth configuration, read once at startup.
type authConfig struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	Secret       []byte
	BaseURL      string
	CookieDomain string
	CookieName   string
	SessionTTL   time.Duration
	oauth        *oauth2.Config
	exchanger    tokenExchanger
}

// loadAuthConfig reads AUTH_* and GOOGLE_* env vars. AUTH_ENABLED is truthy on
// 1/true/on/yes. Missing optional values fall back to defaults.
func loadAuthConfig() authConfig {
	c := authConfig{
		BaseURL:      "https://kunaldawn.com",
		CookieDomain: ".kunaldawn.com",
		CookieName:   "kd_session",
		SessionTTL:   720 * time.Hour,
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_ENABLED"))) {
	case "1", "true", "on", "yes":
		c.Enabled = true
	}
	c.ClientID = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	c.ClientSecret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	if s := strings.TrimSpace(os.Getenv("AUTH_SECRET")); s != "" {
		c.Secret = []byte(s)
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_BASE_URL")); v != "" {
		c.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_COOKIE_DOMAIN")); v != "" {
		c.CookieDomain = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_COOKIE_NAME")); v != "" {
		c.CookieName = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_SESSION_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.SessionTTL = d
		}
	}
	c.oauth = &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  strings.TrimRight(c.BaseURL, "/") + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     googleEndpoint,
	}
	c.exchanger = googleExchanger{cfg: c.oauth}
	return c
}

// valid reports whether an enabled config has the secrets it needs to run.
func (c authConfig) valid() bool {
	return c.ClientID != "" && c.ClientSecret != "" && len(c.Secret) > 0
}

// baseDomain is the cookie domain without its leading dot (e.g. kunaldawn.com).
func (c authConfig) baseDomain() string {
	return strings.TrimPrefix(c.CookieDomain, ".")
}

// safeRedirect returns raw only if it is a same-site https URL or a
// root-relative path; otherwise it returns the configured base URL. Guards
// against open-redirect abuse of the post-login destination.
func (c authConfig) safeRedirect(raw string) string {
	def := c.BaseURL
	if raw == "" {
		return def
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return strings.TrimRight(c.BaseURL, "/") + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return def
	}
	host := u.Hostname()
	base := c.baseDomain()
	if host == base || strings.HasSuffix(host, "."+base) {
		return raw
	}
	return def
}

// tokenExchanger turns an OAuth authorization code into a verified email.
// Abstracted so handlers can be tested without contacting Google.
type tokenExchanger interface {
	exchange(ctx context.Context, code string) (email string, verified bool, err error)
}

type googleExchanger struct {
	cfg *oauth2.Config
}

func (g googleExchanger) exchange(ctx context.Context, code string) (string, bool, error) {
	tok, err := g.cfg.Exchange(ctx, code)
	if err != nil {
		return "", false, err
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", false, errors.New("no id_token in token response")
	}
	return emailFromIDToken(raw)
}

// emailFromIDToken decodes the id_token payload (without re-verifying Google's
// signature — the token arrived directly from Google's token endpoint over
// TLS) and returns the email plus its verified flag. Google sends
// email_verified as either a bool or the string "true".
func emailFromIDToken(idToken string) (string, bool, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", false, errors.New("malformed id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false, err
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false, err
	}
	verified := false
	switch v := claims.EmailVerified.(type) {
	case bool:
		verified = v
	case string:
		verified = v == "true"
	}
	return claims.Email, verified, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test ./... -run 'AuthConfig|SafeRedirect|EmailFromID' -v`
Expected: PASS. Also run `CGO_ENABLED=1 go build .` — expect success (confirms the oauth2 import resolves).

- [ ] **Step 6: Commit**

```bash
git add auth.go auth_test.go go.mod go.sum
git commit -m "feat: add auth config, open-redirect guard, and Google token exchange"
```

---

## Task 3: Gate middleware + login page (`auth.go` part 2)

**Files:**
- Modify: `auth.go` (append)
- Test: `auth_test.go` (append)

**Interfaces:**
- Consumes: `authConfig`, `safeRedirect` (Task 2); `verifySession` (Task 1).
- Produces:
  - `var publicPaths map[string]bool`
  - `func (c authConfig) middleware(next http.Handler) http.Handler`
  - `func (c authConfig) loginPage(redirect string) []byte`
  - `func (c authConfig) handleLogin(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: Write the failing test**

Append to `auth_test.go`:

```go
func TestMiddlewarePublicBypass(t *testing.T) {
	c := testAuthConfig()
	var called bool
	h := c.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	for _, p := range []string{"/login", "/robots.txt", "/auth/google/start", "/og-image.png", "/favicon.ico"} {
		called = false
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if !called {
			t.Fatalf("%s should bypass the gate", p)
		}
	}
}

func TestMiddlewareRedirectsWhenNoCookie(t *testing.T) {
	c := testAuthConfig()
	h := c.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://kunaldawn.com/secret?x=1", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login?redirect=") {
		t.Fatalf("location = %q", loc)
	}
	if !strings.Contains(loc, url.QueryEscape("https://kunaldawn.com/secret?x=1")) {
		t.Fatalf("redirect param missing original URL: %q", loc)
	}
}

func TestMiddlewareAllowsValidCookie(t *testing.T) {
	c := testAuthConfig()
	tok, _ := signSession("a@b.com", time.Hour, c.Secret)
	var called bool
	h := c.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secret", nil)
	req.AddCookie(&http.Cookie{Name: "kd_session", Value: tok})
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("valid cookie should pass through")
	}
}

func TestMiddlewareRejectsBadCookie(t *testing.T) {
	c := testAuthConfig()
	h := c.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secret", nil)
	req.AddCookie(&http.Cookie{Name: "kd_session", Value: "garbage"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("bad cookie should redirect, got %d", rec.Code)
	}
}

func TestLoginPageEmbedsRedirect(t *testing.T) {
	c := testAuthConfig()
	page := string(c.loginPage("https://wiki.kunaldawn.com/x"))
	want := "/auth/google/start?redirect=" + url.QueryEscape("https://wiki.kunaldawn.com/x")
	if !strings.Contains(page, want) {
		t.Fatalf("login page should link to %q", want)
	}
}
```

Add the needed imports to the existing `auth_test.go` import block: `"net/http"`, `"net/http/httptest"`, `"net/url"`, `"strings"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test ./... -run 'Middleware|LoginPage' -v`
Expected: FAIL — `c.middleware undefined` etc.

- [ ] **Step 3: Write the implementation**

Append to `auth.go` (and add `"html"`, `"net/http"` to its import block):

```go
// publicPaths bypass the auth gate: the auth routes themselves plus
// crawler/PWA files that must stay reachable without login.
var publicPaths = map[string]bool{
	"/login":                      true,
	"/auth/google/start":          true,
	"/auth/google/callback":       true,
	"/logout":                     true,
	"/robots.txt":                 true,
	"/sitemap.xml":                true,
	"/llms.txt":                   true,
	"/og-image.png":               true,
	"/site.webmanifest":           true,
	"/favicon.ico":                true,
	"/favicon-16x16.png":          true,
	"/favicon-32x32.png":          true,
	"/favicon-48x48.png":          true,
	"/apple-touch-icon.png":       true,
	"/android-chrome-192x192.png": true,
	"/android-chrome-512x512.png": true,
}

// middleware gates every request behind a valid session cookie, except
// publicPaths. Unauthenticated requests get 302 -> /login with the original
// absolute URL as the redirect-back target.
func (c authConfig) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if ck, err := r.Cookie(c.CookieName); err == nil {
			if _, verr := verifySession(ck.Value, c.Secret); verr == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		current := "https://" + r.Host + r.URL.RequestURI()
		http.Redirect(w, r, "/login?redirect="+url.QueryEscape(c.safeRedirect(current)), http.StatusFound)
	})
}

// handleLogin serves the themed sign-in page, carrying the validated
// redirect-back target through to the Google start endpoint.
func (c authConfig) handleLogin(w http.ResponseWriter, r *http.Request) {
	redirect := c.safeRedirect(r.URL.Query().Get("redirect"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(c.loginPage(redirect))
}

// loginPage renders the self-contained themed login page. redirect is already
// validated by the caller; it is URL-encoded into the sign-in link and
// HTML-escaped for the attribute context.
func (c authConfig) loginPage(redirect string) []byte {
	startHref := html.EscapeString("/auth/google/start?redirect=" + url.QueryEscape(redirect))
	return []byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in — kunaldawn.com</title>
<meta property="og:title" content="KD's Homebrew Digital Archive">
<meta property="og:description" content="A home-grown mirror of the public internet. Sign in to continue.">
<meta property="og:image" content="/og-image.png">
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    background: #05110d; color: #cfeee0;
    font-family: 'Share Tech Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
    padding: 24px;
    background-image: radial-gradient(circle at 50% 0%, rgba(127,209,179,0.08), transparent 60%);
  }
  .box {
    width: 100%; max-width: 460px; text-align: center;
    border: 1px solid rgba(127,209,179,0.35); border-radius: 10px;
    padding: 40px 28px; background: rgba(10,28,22,0.6);
    box-shadow: 0 0 40px rgba(0,0,0,0.5);
  }
  h1 { font-size: 20px; margin: 0 0 6px; color: #7fd1b3; letter-spacing: 1px; }
  .tag { font-size: 12px; opacity: 0.7; margin-bottom: 26px; }
  .btn {
    display: inline-flex; align-items: center; gap: 10px;
    padding: 12px 22px; border-radius: 6px; text-decoration: none;
    background: #7fd1b3; color: #05110d; font-weight: bold;
  }
  .btn:hover { background: #a8d8c4; }
  .footer { margin-top: 28px; font-size: 11px; opacity: 0.5; }
</style>
</head>
<body>
  <div class="box">
    <h1>// SIGN IN</h1>
    <div class="tag">KD's Homebrew Digital Archive</div>
    <a class="btn" href="` + startHref + `">Sign in with Google</a>
    <div class="footer">free for all, free forever</div>
  </div>
</body>
</html>`)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test ./... -run 'Middleware|LoginPage' -v`
Expected: PASS — all five tests green.

- [ ] **Step 5: Commit**

```bash
git add auth.go auth_test.go
git commit -m "feat: add auth gate middleware and themed login page"
```

---

## Task 4: OAuth handlers (`auth.go` part 3)

**Files:**
- Modify: `auth.go` (append)
- Test: `auth_test.go` (append)

**Interfaces:**
- Consumes: `authConfig`, `tokenExchanger`, `signToken`/`verifyToken` (Tasks 1–2), `signSession`.
- Produces:
  - `const oauthStateCookie = "kd_oauth_state"`
  - `type stateClaims struct { Nonce, Redirect string; ExpiresAt int64 }`
  - `func randomNonce() (string, error)`
  - `func (c authConfig) signState(nonce, redirect string) (string, error)`
  - `func (c authConfig) verifyState(token string) (stateClaims, error)`
  - `func (c authConfig) handleGoogleStart(w http.ResponseWriter, r *http.Request)`
  - `func (c authConfig) handleGoogleCallback(w http.ResponseWriter, r *http.Request)`
  - `func (c authConfig) handleLogout(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: Write the failing test**

Append to `auth_test.go` (add `"context"` to the import block):

```go
type fakeExchanger struct {
	email    string
	verified bool
	err      error
}

func (f fakeExchanger) exchange(ctx context.Context, code string) (string, bool, error) {
	return f.email, f.verified, f.err
}

func TestCallbackSuccessSetsCookieAndRedirects(t *testing.T) {
	c := testAuthConfig()
	c.exchanger = fakeExchanger{email: "a@b.com", verified: true}
	nonce := "nonce123"
	state, _ := c.signState(nonce, "https://wiki.kunaldawn.com/x")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state="+nonce, nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: state})
	c.handleGoogleCallback(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://wiki.kunaldawn.com/x" {
		t.Fatalf("location = %q", loc)
	}
	var session *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "kd_session" {
			session = ck
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("session cookie not set")
	}
	if !session.HttpOnly || session.Domain != ".kunaldawn.com" {
		t.Fatalf("session cookie attrs: %+v", session)
	}
	if _, err := verifySession(session.Value, c.Secret); err != nil {
		t.Fatalf("session cookie does not verify: %v", err)
	}
}

func TestCallbackStateMismatch(t *testing.T) {
	c := testAuthConfig()
	c.exchanger = fakeExchanger{email: "a@b.com", verified: true}
	state, _ := c.signState("realnonce", "https://kunaldawn.com/")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=WRONG", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: state})
	c.handleGoogleCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCallbackUnverifiedEmail(t *testing.T) {
	c := testAuthConfig()
	c.exchanger = fakeExchanger{email: "a@b.com", verified: false}
	state, _ := c.signState("n", "https://kunaldawn.com/")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=n", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: state})
	c.handleGoogleCallback(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	c := testAuthConfig()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	c.handleLogout(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	var session *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "kd_session" {
			session = ck
		}
	}
	if session == nil || session.MaxAge >= 0 {
		t.Fatalf("logout should expire the session cookie, got %+v", session)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test ./... -run 'Callback|Logout' -v`
Expected: FAIL — `c.handleGoogleCallback undefined` etc.

- [ ] **Step 3: Write the implementation**

Append to `auth.go` (add `"crypto/rand"` and `"golang.org/x/oauth2"` is already imported; `"net/http"`, `"encoding/json"`, `"errors"`, `"time"`, `"strings"`, `"net/url"` already present):

```go
const oauthStateCookie = "kd_oauth_state"

// stateClaims is the short-lived signed CSRF state carried in a cookie during
// the OAuth round-trip.
type stateClaims struct {
	Nonce     string `json:"nonce"`
	Redirect  string `json:"redirect"`
	ExpiresAt int64  `json:"exp"`
}

// randomNonce returns a 128-bit base64url random string.
func randomNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c authConfig) signState(nonce, redirect string) (string, error) {
	payload, err := json.Marshal(stateClaims{
		Nonce:     nonce,
		Redirect:  redirect,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	return signToken(payload, c.Secret), nil
}

func (c authConfig) verifyState(token string) (stateClaims, error) {
	var s stateClaims
	payload, err := verifyToken(token, c.Secret)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(payload, &s); err != nil {
		return s, errors.New("bad state payload")
	}
	if s.ExpiresAt <= time.Now().Unix() {
		return s, errors.New("state expired")
	}
	return s, nil
}

// handleGoogleStart begins the OAuth flow: it stores a signed state cookie
// (nonce + validated redirect) and redirects to Google's consent screen.
func (c authConfig) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	redirect := c.safeRedirect(r.URL.Query().Get("redirect"))
	nonce, err := randomNonce()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state, err := c.signState(nonce, redirect)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/auth",
		Domain:   c.CookieDomain,
		MaxAge:   600,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, c.oauth.AuthCodeURL(nonce, oauth2.AccessTypeOnline), http.StatusFound)
}

// handleGoogleCallback validates the state, exchanges the code for the user's
// verified email, sets the session cookie, and redirects back.
func (c authConfig) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	st, err := c.verifyState(stateCookie.Value)
	if err != nil {
		http.Error(w, "bad state", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != st.Nonce {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	email, verified, err := c.exchanger.exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "exchange failed", http.StatusBadGateway)
		return
	}
	if !verified || email == "" {
		http.Error(w, "email not verified", http.StatusForbidden)
		return
	}
	tok, err := signSession(email, c.SessionTTL, c.Secret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.CookieName,
		Value:    tok,
		Path:     "/",
		Domain:   c.CookieDomain,
		MaxAge:   int(c.SessionTTL.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: "", Path: "/auth", Domain: c.CookieDomain,
		MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, c.safeRedirect(st.Redirect), http.StatusFound)
}

// handleLogout expires the session cookie and returns to the login page.
func (c authConfig) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: c.CookieName, Value: "", Path: "/", Domain: c.CookieDomain,
		MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test ./... -run 'Callback|Logout' -v`
Expected: PASS — all four tests green.

- [ ] **Step 5: Run the full suite + vet**

Run: `CGO_ENABLED=1 go test ./... && go vet ./...`
Expected: all PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add auth.go auth_test.go
git commit -m "feat: add Google OAuth start/callback/logout handlers with CSRF state"
```

---

## Task 5: Wire auth into `main()`

**Files:**
- Modify: `main.go` (the route registration + handler wiring, around lines 542–560)

**Interfaces:**
- Consumes: `loadAuthConfig`, `authConfig.valid`, `authConfig.middleware`, `authConfig.handleLogin`/`handleGoogleStart`/`handleGoogleCallback`/`handleLogout` (Tasks 2–4).
- Produces: nothing new.

- [ ] **Step 1: Register the auth routes and install the gate**

In `main.go`, the static route is currently registered then the handler is wired:

```go
	mux.Handle("/", staticHandler(http.FileServer(noDirFS{http.Dir(staticDir)})))

	var handler http.Handler = securityHeaders(mux)
```

Replace that with:

```go
	mux.Handle("/", staticHandler(http.FileServer(noDirFS{http.Dir(staticDir)})))

	// Auth (env-gated). When enabled, register the OAuth routes and gate the
	// whole site; when AUTH_ENABLED is on but secrets are missing, refuse to
	// start rather than silently serving an ungated site.
	authCfg := loadAuthConfig()
	if authCfg.Enabled {
		if !authCfg.valid() {
			log.Fatal("[AUTH] AUTH_ENABLED is set but GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, or AUTH_SECRET is missing")
		}
		mux.HandleFunc("/login", authCfg.handleLogin)
		mux.HandleFunc("/auth/google/start", authCfg.handleGoogleStart)
		mux.HandleFunc("/auth/google/callback", authCfg.handleGoogleCallback)
		mux.HandleFunc("/logout", authCfg.handleLogout)
		log.Printf("[AUTH] Google auth ENABLED (cookie domain %s)", authCfg.CookieDomain)
	}

	// Order: maintenance( securityHeaders( authGate( mux ) ) ). securityHeaders
	// stays outermost (after maintenance) so the login page AND the gate's
	// redirect responses both carry the security headers.
	var handler http.Handler = mux
	if authCfg.Enabled {
		handler = authCfg.middleware(handler)
	}
	handler = securityHeaders(handler)
```

The existing maintenance block that follows (`if mcfg := loadMaintenanceConfig(); mcfg.Enabled { handler = maintenanceMiddleware(...)(handler) }`) stays unchanged and remains outermost.

- [ ] **Step 2: Verify build + full suite**

Run: `CGO_ENABLED=1 go build . && CGO_ENABLED=1 go test ./... && go vet ./...`
Expected: build OK; all tests PASS; vet clean.

- [ ] **Step 3: Smoke test — auth OFF (default)**

Run:
```bash
PORT=8099 CGO_ENABLED=1 go run . &
sleep 2
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/
kill %1
```
Expected: `200` (site served normally; auth not installed).

- [ ] **Step 4: Smoke test — auth ON (no Google network needed)**

Run:
```bash
AUTH_ENABLED=on AUTH_SECRET=testsecret GOOGLE_CLIENT_ID=cid GOOGLE_CLIENT_SECRET=csec PORT=8099 CGO_ENABLED=1 go run . &
sleep 2
echo "root (gated):"; curl -s -o /dev/null -w "%{http_code} %{redirect_url}\n" http://localhost:8099/
echo "login (public):"; curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/login
echo "robots (public):"; curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/robots.txt
echo "start (redirects to google):"; curl -s -o /dev/null -w "%{http_code} %{redirect_url}\n" "http://localhost:8099/auth/google/start?redirect=https://kunaldawn.com/"
kill %1
```
Expected: root → `302` with redirect_url containing `/login?redirect=`; login → `200`; robots → `200`; start → `302` with redirect_url beginning `https://accounts.google.com/o/oauth2/auth`.

- [ ] **Step 5: Smoke test — fail-closed misconfig**

Run:
```bash
AUTH_ENABLED=on PORT=8099 CGO_ENABLED=1 go run . ; echo "exit: $?"
```
Expected: process logs `[AUTH] AUTH_ENABLED is set but ... missing` and exits non-zero (does NOT serve).

- [ ] **Step 6: Commit**

```bash
git add main.go
git commit -m "feat: register auth routes and install the gate (fail-closed)"
```

---

## Task 6: Content edits — remove "no user accounts"

**Files:**
- Modify: `static/index.html` (2 edits), `README.md` (2 edits), `static/llms.txt` (2 edits)

**Interfaces:**
- Consumes: nothing. Produces: nothing (content only).

- [ ] **Step 1: Terms list — `static/index.html`**

Replace the line:
```html
          <li><span class="bul">&raquo;</span> no user accounts</li>
```
with:
```html
          <li><span class="bul">&raquo;</span> google login</li>
```

- [ ] **Step 2: Terms blurb — `static/index.html`**

Replace:
```
                  No ads. Basic analytics only. No user accounts. No uptime guarantee. No monetization.
```
with:
```
                  No ads. Basic analytics only. Google login. No uptime guarantee. No monetization.
```

- [ ] **Step 3: README — feature line**

In `README.md`, replace:
```
- **SQLite** (WAL mode, single writer) for the handful of counters worth keeping. No accounts, no third-party analytics.
```
with:
```
- **SQLite** (WAL mode, single writer) for the handful of counters worth keeping. No third-party analytics.
```

- [ ] **Step 4: README — banner footer**

In `README.md`, replace:
```
  no ads · no accounts · no monetization · free for all, free forever
```
with:
```
  no ads · no monetization · free for all, free forever
```

- [ ] **Step 5: llms.txt — intro line**

In `static/llms.txt`, replace:
```
a home-grown archive in a home drawing about 30 watts off-grid. No ads, basic analytics
only, no user accounts, no uptime guarantee, no monetization — free for all, free forever.
```
with:
```
a home-grown archive in a home drawing about 30 watts off-grid. No ads, basic analytics
only, no uptime guarantee, no monetization — free for all, free forever.
```

- [ ] **Step 6: llms.txt — ethos line**

In `static/llms.txt`, replace:
```
Ethos is stark and deliberate: no ads, basic analytics only, no user accounts,
no uptime guarantee, no monetization. Free for all, free forever. Preservation doesn't
```
with:
```
Ethos is stark and deliberate: no ads, basic analytics only,
no uptime guarantee, no monetization. Free for all, free forever. Preservation doesn't
```

- [ ] **Step 7: Verify**

Run: `grep -rni "no user account\|no accounts" README.md static/index.html static/llms.txt`
Expected: **no output** (all removed).
Run: `grep -n "google login" static/index.html`
Expected: 1 match (the terms list line). Run `grep -n "Google login." static/index.html` → 1 match (the blurb).

- [ ] **Step 8: Commit**

```bash
git add static/index.html README.md static/llms.txt
git commit -m "docs: replace 'no user accounts' with google login in terms; drop elsewhere"
```

---

## Task 7: Subdomain integration docs (`docs/auth-integration.md`)

**Files:**
- Create: `docs/auth-integration.md`

**Interfaces:**
- Consumes: the cookie format from Tasks 1–4. Produces: documentation only.

- [ ] **Step 1: Write the doc**

Create `docs/auth-integration.md`:

````markdown
# Cross-Subdomain Auth Integration

The main site (`kunaldawn.com`) issues a stateless session cookie after Google
sign-in. Archive subdomains verify that cookie **locally** with a shared secret
and redirect unauthenticated visitors to the central login page.

## The session cookie

- **Name:** `kd_session` (or `AUTH_COOKIE_NAME`)
- **Domain:** `.kunaldawn.com` (or `AUTH_COOKIE_DOMAIN`) — sent to every subdomain
- **Attributes:** `Secure; HttpOnly; SameSite=Lax; Path=/`
- **Value:** an **HS256 JWT** signed with `AUTH_SECRET`, claims:

```json
{ "sub": "user@example.com", "email": "user@example.com",
  "iat": 1718000000, "exp": 1720592000, "iss": "kunaldawn.com" }
```

Any standard JWT library can verify it: check the **HS256** signature against
`AUTH_SECRET`, then check `exp` is in the future. `AUTH_SECRET` must be the
**exact same value** on the main server and every subdomain app.

## Reference verification (Go, stdlib)

```go
func verify(token string, secret []byte) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var c struct {
		Email string `json:"email"`
		Exp   int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &c) != nil || c.Exp <= time.Now().Unix() {
		return "", false
	}
	return c.Email, true
}
```

(For full parity with the issuer, also reject any header whose `alg` is not
`HS256`.)

## Subdomain gate behavior

On every request, read the `kd_session` cookie and verify it. If it is
missing, malformed, or expired, redirect to the central login page with the
full current URL as the `redirect` parameter:

```
302 Location: https://kunaldawn.com/login?redirect=<url-encoded current URL>
```

After the user signs in, the main site sets the `.kunaldawn.com` cookie and
redirects back to that URL. The redirect target is validated server-side to be
`kunaldawn.com` or a `*.kunaldawn.com` subdomain (open-redirect guard), so only
same-site destinations are honored.

## Main-server environment variables

| Var | Meaning | Default |
|---|---|---|
| `AUTH_ENABLED` | `1`/`true`/`on`/`yes` enables gating + auth | off |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | OAuth Web client credentials | — (required) |
| `AUTH_SECRET` | HS256 signing secret, shared with all subdomains | — (required) |
| `AUTH_BASE_URL` | builds the OAuth callback URL | `https://kunaldawn.com` |
| `AUTH_COOKIE_DOMAIN` | session cookie domain | `.kunaldawn.com` |
| `AUTH_COOKIE_NAME` | session cookie name | `kd_session` |
| `AUTH_SESSION_TTL` | session lifetime (`time.ParseDuration`) | `720h` |

When `AUTH_ENABLED` is on but any required secret is missing, the server
refuses to start (fail-closed).

## Google Cloud Console setup

1. APIs & Services → OAuth consent screen → External; add your email as a test
   user (or publish).
2. Credentials → Create credentials → OAuth client ID → **Web application**.
3. **Authorized redirect URI:** `https://kunaldawn.com/auth/google/callback`
   (i.e. `{AUTH_BASE_URL}/auth/google/callback`).
4. Scopes used: `openid`, `email`, `profile`.
5. Copy the client ID/secret into `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET`.
````

- [ ] **Step 2: Commit**

```bash
git add -f docs/auth-integration.md
git commit -m "docs: add cross-subdomain auth integration guide"
```

Note: `docs/` outside `docs/superpowers/` is not gitignored, but use `-f` only if needed; plain `git add docs/auth-integration.md` should work. Verify with `git status` that the file is staged.

---

## Self-Review Notes

- **Spec coverage:** §1–2 (config, files, fail-closed) → Tasks 2 & 5; §3 (env) → Task 2; §4 (flow, cookie attrs, middleware order) → Tasks 3–5; §5 (safeRedirect, CSRF, id_token trust, allowlist, constant-time) → Tasks 1–4; §6 (content) → Task 6; §7 (tests) → Tasks 1–4; §8 (docs) → Task 7. All mapped.
- **Middleware order refinement:** the spec's prose ("security headers apply to the login page and all gate redirects") governs over its illustrative snippet — Task 5 places `securityHeaders` outermost (after maintenance), wrapping the auth gate, so gate redirects carry headers. This realizes the spec's stated intent.
- **Reduced dependency:** only `golang.org/x/oauth2` is added; Google's endpoint is hardcoded instead of importing `.../google`, consistent with the minimalist single-binary ethos. Documented in Global Constraints.
- **Type consistency:** `authConfig`, `sessionClaims`, `stateClaims`, `tokenExchanger`, `oauthStateCookie`, `publicPaths`, and the handler method names are used identically across Tasks 1–5. The session cookie name/domain assertions in tests match the issuance in Task 4.
- **No placeholders:** every code step contains complete code; every test step has real assertions.
