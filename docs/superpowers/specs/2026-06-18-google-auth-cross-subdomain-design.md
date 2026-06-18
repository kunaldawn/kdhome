# Design: Google OAuth & Cross-Subdomain Authentication

**Date:** 2026-06-18
**Status:** Approved

Add Google sign-in to kunaldawn.com. The whole site is gated behind login (except a small public allowlist of auth + crawler/SEO paths). A successful login sets a stateless, HMAC-signed session cookie scoped to `.kunaldawn.com`, so every archive subdomain receives it and can verify it **locally** with a shared secret. Unauthenticated requests are redirected to `/login?redirect=<original-url>`; after login the user is sent back to where they came from. Also: replace the "no user accounts" wording in the Terms with "google login", and remove the "no user accounts" phrasing everywhere else it appears.

---

## 1. Goals & Access Model

- **Identity provider:** Google OAuth 2.0 (Authorization Code flow).
- **Who may log in:** any Google account with a **verified** email (`email_verified == true`). No allowlist, no domain restriction.
- **Session:** a stateless **HS256 JWT** carried in a cookie on domain `.kunaldawn.com`. Signed with a shared secret (`AUTH_SECRET`). No server-side session store and no DB writes for auth. Revocation is handled by expiry + logout (acceptable given the single-operator, personal-archive context).
- **Gating scope:** the entire site requires login **except** the public allowlist in §5. Unauthenticated requests to any other path get `302 → /login?redirect=<original-url>`.
- **Subdomains:** the archive subdomains are separate apps that live **outside this repo**. They verify the same cookie **locally** using `AUTH_SECRET` (standard JWT, so any JWT library works) and redirect to `https://kunaldawn.com/login?redirect=<their-full-url>` on a missing/invalid/expired cookie. This repo issues the cookie and ships `docs/auth-integration.md` documenting the exact format + a reference verification snippet so those apps can implement local checks.
- **Toggle:** `AUTH_ENABLED` (default **off**). When off, no gate and no auth routes are installed — the site behaves exactly as it does today. Mirrors the existing maintenance-mode pattern. When on but `AUTH_SECRET` / `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` are missing, the server logs a fatal error and refuses to start (fail-closed, never silently serve an open site that was meant to be gated).

## 2. Files & Structure

- **Create `token.go`** — the security-critical core, stdlib only:
  - `func signSession(email string, ttl time.Duration, secret []byte) (string, error)` — builds an HS256 JWT with claims `{sub, email, iat, exp, iss}`.
  - `func verifySession(token string, secret []byte) (sessionClaims, error)` — verifies signature (constant-time compare), `alg == HS256`, and `exp`; returns claims or an error.
  - `type sessionClaims struct { Email string; IssuedAt int64; ExpiresAt int64; Issuer string }`
- **Create `auth.go`** — config, gate, handlers, login page:
  - `type authConfig struct { Enabled bool; ClientID, ClientSecret string; Secret []byte; BaseURL, CookieDomain, CookieName string; SessionTTL time.Duration; oauth *oauth2.Config; exchanger tokenExchanger }`
  - `func loadAuthConfig() authConfig`
  - `func (c authConfig) middleware(next http.Handler) http.Handler` — the gate.
  - `func (c authConfig) handleLogin(w, r)` / `handleGoogleStart` / `handleGoogleCallback` / `handleLogout`.
  - `func (c authConfig) safeRedirect(raw string) string` — open-redirect guard.
  - `func (c authConfig) loginPage(redirect string) []byte` — themed self-contained page.
  - `type tokenExchanger interface { exchange(ctx, code string) (email string, verified bool, err error) }` with a real Google implementation; lets tests inject a fake.
- **Modify `main.go`** — load config, register routes, install the gate (see §4 ordering).
- **Modify `go.mod` / `go.sum`** — add `golang.org/x/oauth2` and `golang.org/x/oauth2/google`.
- **Create `docs/auth-integration.md`** — cookie format, `AUTH_SECRET` env, a reference local-verification snippet (Go), the subdomain redirect-to-login pattern, and Google Cloud OAuth client setup (authorized redirect URI = `{AUTH_BASE_URL}/auth/google/callback`).
- **Modify `static/index.html`, `README.md`, `static/llms.txt`** — content edits in §6.

## 3. Configuration (env, read once at startup in `main()`)

| Var | Meaning | Default |
|---|---|---|
| `AUTH_ENABLED` | truthy (`1`/`true`/`on`/`yes`, case-insensitive) enables gating + auth routes | off |
| `GOOGLE_CLIENT_ID` | OAuth client id | — (required when enabled) |
| `GOOGLE_CLIENT_SECRET` | OAuth client secret | — (required when enabled) |
| `AUTH_SECRET` | HMAC-SHA256 signing secret (shared with subdomains) | — (required when enabled) |
| `AUTH_BASE_URL` | builds the OAuth redirect URI `{BASE}/auth/google/callback` and the default post-login destination | `https://kunaldawn.com` |
| `AUTH_COOKIE_DOMAIN` | session cookie `Domain` | `.kunaldawn.com` |
| `AUTH_COOKIE_NAME` | session cookie name | `kd_session` |
| `AUTH_SESSION_TTL` | session lifetime, parsed via `time.ParseDuration` | `720h` (30 days) |

When `AUTH_ENABLED` is on and any of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `AUTH_SECRET` is empty, `loadAuthConfig` returns a config that `main()` treats as a fatal misconfiguration (`log.Fatal`).

## 4. Request Flow & Middleware

**Cookie attributes (session):** `Domain=AUTH_COOKIE_DOMAIN`, `Path=/`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Max-Age=int(SessionTTL.Seconds())`.

**Login flow:**
1. Gate sees no/invalid/expired session cookie on a gated path → `302 /login?redirect=<current-absolute-url>`.
2. `GET /login` → renders the themed page (self-contained inline HTML/CSS, like the maintenance page) carrying a **Sign in with Google** link to `/auth/google/start?redirect=<validated-redirect>`. The page also includes basic `<title>` + OpenGraph meta so link unfurls of `kunaldawn.com` still render something (the homepage HTML is now gated).
3. `GET /auth/google/start`:
   - `safeRedirect` the `redirect` param (§5).
   - Generate a random nonce via `crypto/rand`.
   - Set a short-lived signed `kd_oauth_state` cookie (`HttpOnly`, `Secure`, `SameSite=Lax`, `Max-Age≈600s`, `Path=/auth`) holding `{nonce, redirect}` (itself an HS256 JWT signed with `AUTH_SECRET`, short exp).
   - `302` to `oauth.AuthCodeURL(nonce, oauth2.AccessTypeOnline)`.
4. `GET /auth/google/callback?code=&state=`:
   - Read `kd_oauth_state`; verify it parses and `state` param == its nonce (CSRF). On mismatch → `400`.
   - `email, verified, err := exchanger.exchange(ctx, code)` (real impl: `oauth.Exchange`, then read `id_token` from `token.Extra("id_token")`, base64url-decode its payload, read `email` + `email_verified`). On error → `502`/`400`.
   - If `!verified` → `403` "email not verified".
   - `signSession(email, SessionTTL, secret)`; set the session cookie; clear `kd_oauth_state`; `302` to the redirect captured in the state cookie.
5. `GET /logout`: overwrite the session cookie with an expired one (same Domain/Path); `302 /login`.

**Middleware order (in `main()`):**
```
handler := authCfg.middleware(securityHeaders(mux))   // gate wraps headers+mux when enabled
... maintenance still wraps outermost if enabled ...
srv.Handler = maintenanceMiddleware?(handler)
```
Concretely: `var h http.Handler = mux; h = securityHeaders(h); if authCfg.Enabled { h = authCfg.middleware(h) }; if maint.Enabled { h = maintenanceMiddleware(...)(h) }`. Maintenance wins over auth; security headers apply to the login page and all gate redirects. The existing CSP already permits the inline login page (`script-src`/`style-src` include `'unsafe-inline'`) and same-origin navigation; the Google redirect is a top-level `Location` navigation not subject to CSP — **no CSP change required**.

Auth routes (`/login`, `/auth/google/start`, `/auth/google/callback`, `/logout`) are registered on `mux` and listed in the gate's public allowlist so they're reachable without a session.

## 5. Security Details

- **Open-redirect guard (`safeRedirect(raw) string`):** parse `raw`; accept only when scheme is `https` (or a root-relative path beginning `/` and not `//`) AND host equals the base domain or ends with `"." + base`, where base = `strings.TrimPrefix(AUTH_COOKIE_DOMAIN, ".")` (e.g. `kunaldawn.com`). Root-relative paths are resolved against `AUTH_BASE_URL`. Anything else → return `AUTH_BASE_URL`. Applied when rendering `/login` and at `/auth/google/start`.
- **CSRF:** signed short-lived state cookie nonce must equal the `state` query param at callback.
- **id_token trust:** the token is received directly from Google's token endpoint over TLS, so its payload is decoded without re-verifying Google's RSA signature (per Google's documented guidance for server-side code-exchange). Only `email` and `email_verified` are read.
- **Constant-time signature compare** in `verifySession` (`hmac.Equal`).
- **Public allowlist (bypass the gate), exact paths:**
  `/login`, `/auth/google/start`, `/auth/google/callback`, `/logout`,
  `/robots.txt`, `/sitemap.xml`, `/llms.txt`, `/og-image.png`, `/site.webmanifest`,
  `/favicon.ico`, `/favicon-16x16.png`, `/favicon-32x32.png`, `/favicon-48x48.png`,
  `/apple-touch-icon.png`, `/android-chrome-192x192.png`, `/android-chrome-512x512.png`.
  All other paths require a valid session.

## 6. Content Edits ("no user accounts")

- **Terms list** — `static/index.html` (`<li>...no user accounts</li>`): replace text `no user accounts` → `google login`.
- **Terms blurb** — `static/index.html` ("...No user accounts. No uptime guarantee..."): replace `No user accounts.` → `Google login.`
- **Remove** (no replacement) everywhere else:
  - `README.md` "No accounts, no third-party analytics." → "No third-party analytics."
  - `README.md` banner footer "no ads · no accounts · no monetization · free for all, free forever" → "no ads · no monetization · free for all, free forever"
  - `static/llms.txt` (line ~22) "...only, no user accounts, no uptime guarantee..." → drop "no user accounts, "
  - `static/llms.txt` (lines ~151–152) "no ads, basic analytics only, no user accounts, no uptime guarantee, no monetization." → drop "no user accounts, "

## 7. Testing

- **`token_test.go`:** sign→verify round-trip returns the email; reject a tampered payload, an expired token, a wrong-secret token, a malformed token, and a token with a non-HS256 `alg`.
- **`auth_test.go`:**
  - `safeRedirect`: same-domain `https` allowed; subdomain (`https://wiki.kunaldawn.com/x`) allowed; external host (`https://evil.com`) → default; `http://` scheme → default; protocol-relative `//evil.com` → default; empty → default; root-relative `/foo` → `{BASE}/foo`.
  - `loadAuthConfig`: off by default; truthy values enable; defaults for cookie name/domain/TTL/base; TTL parse; (enabled + missing secret) flagged so `main` can fatal.
  - `middleware`: a public-allowlist path passes through without a cookie; a gated path with no cookie → `302` to `/login` with a `redirect` query equal to the original URL; a gated path with a valid session cookie → passes to next; an invalid/expired cookie → `302`.
  - `handleGoogleCallback` with an injected fake `tokenExchanger`: state mismatch → `400`; unverified email → `403`; success → sets the session cookie (correct Domain/HttpOnly/Secure) and `302`s to the state's redirect.
  - `loginPage` embeds the validated redirect in the Google sign-in link.
- The real Google network exchange is not unit-tested; it sits behind `tokenExchanger`.

## 8. Documentation Deliverable (`docs/auth-integration.md`)

- Cookie name/domain/attributes and the **JWT claim shape** (`{sub,email,iat,exp,iss}`, HS256).
- The `AUTH_SECRET` env var and that it must be shared verbatim with every subdomain app.
- A reference Go verification snippet (mirrors `verifySession`) and a note that any standard JWT library validating HS256 + `exp` works.
- The subdomain pattern: on missing/invalid/expired cookie, `302` to `https://kunaldawn.com/login?redirect=<full current URL>`.
- Google Cloud Console setup: OAuth consent screen + Web client, authorized redirect URI `{AUTH_BASE_URL}/auth/google/callback`, scopes `openid email profile`.

## 9. Known Behavior Changes / Notes

- **Visit counter:** with the homepage gated, `recordVisit` (in `staticHandler`) only fires for authenticated views — unauthenticated visitors are redirected before reaching the static handler. Accepted; arguably more meaningful.
- **`llms.txt` accuracy:** only the "no user accounts" phrase is removed; the file is not otherwise rewritten to describe the new login requirement (per the content-edit instruction).
- **SEO:** crawler files stay public, but the homepage HTML is gated, so search engines cannot index page content; the `/login` page's OG meta is what link unfurlers will see for `kunaldawn.com`.

## 10. Out of Scope

- The subdomain apps' own code (they live outside this repo) — only documented, not modified.
- Reverse-proxy / `auth_request` wiring (chosen model is local cookie verification in each subdomain app).
- Multi-provider login, account management, roles/permissions, refresh tokens, server-side revocation lists.
