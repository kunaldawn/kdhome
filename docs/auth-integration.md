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
	hdr, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	var h struct{ Alg string `json:"alg"` }
	if json.Unmarshal(hdr, &h) != nil || h.Alg != "HS256" {
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

The snippet rejects any non-HS256 `alg`, matching the issuer.

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
