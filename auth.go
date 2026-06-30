package main

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

//go:embed login.html
var loginHTML string

var loginTmpl = template.Must(template.New("login").Parse(loginHTML))

// googleEndpoint is hardcoded to avoid importing golang.org/x/oauth2/google
// (and its transitive deps). These URLs are stable.
var googleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

// authConfig is the env-driven auth configuration, read once at startup.
type authConfig struct {
	Enabled         bool
	ClientID        string
	ClientSecret    string
	Secret          []byte
	BaseURL         string
	CookieDomain    string
	CookieName      string
	SessionTTL      time.Duration
	SuperAdmin      string
	AnonEnabled     bool
	AnonTTL         time.Duration
	AnonPoWSubBits  int     // leading-zero bits required per sub-puzzle
	AnonPoWTargetMS int     // target total solve time (ms) for an honest device
	AnonPoWHashrate float64 // assumed browser SHA-256 H/s when the client sends no benchmark
	AnonPoWKMin     int     // sub-puzzle floor (security/bot-cost minimum)
	AnonPoWKMax     int     // sub-puzzle ceiling (bounds verify cost + slow-device worst case)
	oauth           *oauth2.Config
	exchanger       tokenExchanger
	anonGuard       *anonGuard
}

// PoW is k independent sub-puzzles of subBits leading-zero bits each. Total
// expected work is k·2^subBits hashes; the Erlang-k solve-time distribution keeps
// wall-clock in a tight band around the target (variance ∝ 1/k), unlike a single
// big puzzle whose geometric distribution swings from sub-second to "never".
//   - defaultAnonPoWSubBits: bits per sub-puzzle (~2^14 = 16384 hashes each). Big
//     enough that per-puzzle overhead is amortised, small enough for fine k steps.
//   - defaultAnonPoWHashrate: assumed browser WebCrypto SHA-256 H/s when the
//     client sends no benchmark. Measured mainstream value; the client normally
//     reports its own rate so the server can size k to that device.
//   - defaultAnonPoWTargetMS: target total solve time on an honest device.
//   - anonPoWHashrateCap: clamp on a client-reported hashrate (bounds k math).
//   - anonKEscalationStep: extra sub-puzzles added per recent successful mint
//     (linear anti-abuse; see kFor).
const (
	defaultAnonPoWSubBits  = 14
	defaultAnonPoWHashrate = 200000
	defaultAnonPoWTargetMS = 7000
	anonPoWHashrateCap     = 5000000
	anonKEscalationStep    = 24
	// anonPoWKMaxFactor caps k at this multiple of KMin when KMax isn't set, so a
	// very fast device (or escalation) can't blow up server-side verify cost.
	anonPoWKMaxFactor = 8
)

// powKForHashrate sizes the sub-puzzle count to spend ~targetMS solving on a
// device of the given hashrate: target work = hashrate·targetMS, divided by the
// per-puzzle cost 2^subBits, clamped to [kMin, kMax]. kMin is the security floor
// (the minimum work anyone pays, even a client that under-reports its speed);
// kMax bounds verification cost and the slow-device worst case.
func powKForHashrate(hashrate float64, targetMS, subBits, kMin, kMax int) int {
	if hashrate < 1 {
		hashrate = 1
	}
	work := hashrate * float64(targetMS) / 1000.0
	k := int(math.Round(work / math.Exp2(float64(subBits))))
	if k < kMin {
		k = kMin
	}
	if k > kMax {
		k = kMax
	}
	return k
}

// humanizeDuration renders a duration as a friendly "N unit(s)" string for the
// sign-in page, picking the largest whole unit (days/hours/minutes/seconds)
// that divides the duration evenly. 720h -> "30 days", 30m -> "30 minutes".
func humanizeDuration(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.FormatInt(n, 10) + " " + unit + "s"
	}
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return plural(int64(d/(24*time.Hour)), "day")
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int64(d/time.Hour), "hour")
	case d >= time.Minute && d%time.Minute == 0:
		return plural(int64(d/time.Minute), "minute")
	default:
		return plural(int64(d/time.Second), "second")
	}
}

// loadAuthConfig reads AUTH_* and GOOGLE_* env vars. AUTH_ENABLED is truthy on
// 1/true/on/yes. Missing optional values fall back to defaults.
func loadAuthConfig() authConfig {
	c := authConfig{
		BaseURL:         "https://kunaldawn.com",
		CookieDomain:    ".kunaldawn.com",
		CookieName:      "kd_session",
		SessionTTL:      720 * time.Hour,
		AnonTTL:         30 * time.Minute,
		AnonPoWSubBits:  defaultAnonPoWSubBits,
		AnonPoWTargetMS: defaultAnonPoWTargetMS,
		AnonPoWHashrate: defaultAnonPoWHashrate,
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
	c.SuperAdmin = strings.TrimSpace(os.Getenv("SUPER_ADMIN_EMAIL"))
	if v := strings.TrimSpace(os.Getenv("AUTH_SESSION_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.SessionTTL = d
		} else {
			log.Printf("[AUTH] invalid AUTH_SESSION_TTL %q, using default %s", v, c.SessionTTL)
		}
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_ANON_ENABLED"))) {
	case "1", "true", "on", "yes":
		c.AnonEnabled = true
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_ANON_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.AnonTTL = d
		} else {
			log.Printf("[AUTH] invalid AUTH_ANON_TTL %q, using default %s", v, c.AnonTTL)
		}
	}
	// PoW is k sub-puzzles of AnonPoWSubBits bits each. Tunables:
	//   AUTH_ANON_POW_SUBBITS   — bits per sub-puzzle (granularity of k).
	//   AUTH_ANON_POW_MS        — target total solve time (ms) on an honest device.
	//   AUTH_ANON_POW_HASHRATE  — assumed browser H/s when a client sends no
	//                             benchmark; also anchors the default KMin floor.
	//   AUTH_ANON_POW_KMIN      — sub-puzzle floor (security/bot cost). Default:
	//                             the work to hit the target at the assumed rate.
	//   AUTH_ANON_POW_KMAX      — sub-puzzle ceiling. Default: KMin·anonPoWKMaxFactor.
	// Unlike the old bits scheme this hits a real wall-clock target: the client
	// reports its measured rate and the server sizes k to it (clamped to the floor).
	if v := strings.TrimSpace(os.Getenv("AUTH_ANON_POW_SUBBITS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 32 {
			c.AnonPoWSubBits = n
		} else {
			log.Printf("[AUTH] invalid AUTH_ANON_POW_SUBBITS %q, using default %d", v, c.AnonPoWSubBits)
		}
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_ANON_POW_HASHRATE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.AnonPoWHashrate = float64(n)
		} else {
			log.Printf("[AUTH] invalid AUTH_ANON_POW_HASHRATE %q, using default %.0f", v, c.AnonPoWHashrate)
		}
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_ANON_POW_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			c.AnonPoWTargetMS = ms
		} else {
			log.Printf("[AUTH] invalid AUTH_ANON_POW_MS %q, using default %d", v, c.AnonPoWTargetMS)
		}
	}
	// KMin defaults to the work needed to hit the target at the assumed hashrate —
	// i.e. a mainstream device solves in ~target, and that is also the bot floor.
	c.AnonPoWKMin = powKForHashrate(c.AnonPoWHashrate, c.AnonPoWTargetMS, c.AnonPoWSubBits, 1, 1<<30)
	if c.AnonPoWKMin < 1 {
		c.AnonPoWKMin = 1
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_ANON_POW_KMIN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			c.AnonPoWKMin = n
		} else {
			log.Printf("[AUTH] invalid AUTH_ANON_POW_KMIN %q, using default %d", v, c.AnonPoWKMin)
		}
	}
	c.AnonPoWKMax = c.AnonPoWKMin * anonPoWKMaxFactor
	if v := strings.TrimSpace(os.Getenv("AUTH_ANON_POW_KMAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			c.AnonPoWKMax = n
		} else {
			log.Printf("[AUTH] invalid AUTH_ANON_POW_KMAX %q, using default %d", v, c.AnonPoWKMax)
		}
	}
	if c.AnonPoWKMax < c.AnonPoWKMin {
		log.Printf("[AUTH] AUTH_ANON_POW_KMAX (%d) < KMin (%d); raising to KMin", c.AnonPoWKMax, c.AnonPoWKMin)
		c.AnonPoWKMax = c.AnonPoWKMin
	}
	log.Printf("[AUTH] anon PoW: %d sub-puzzles ×%d bits floor (target %dms @ %.0f H/s), k∈[%d,%d]",
		c.AnonPoWKMin, c.AnonPoWSubBits, c.AnonPoWTargetMS, c.AnonPoWHashrate, c.AnonPoWKMin, c.AnonPoWKMax)
	c.oauth = &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  strings.TrimRight(c.BaseURL, "/") + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     googleEndpoint,
	}
	c.exchanger = googleExchanger{cfg: c.oauth}
	c.anonGuard = newAnonGuard()
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

// publicPaths bypass the auth gate: the auth routes themselves plus
// crawler/PWA files that must stay reachable without login.
var publicPaths = map[string]bool{
	"/login":                      true,
	"/auth/google/start":          true,
	"/auth/google/callback":       true,
	"/logout":                     true,
	"/auth/anon/challenge":        true,
	"/auth/anon/redeem":           true,
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
	"/fx.js":                      true,
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
			if claims, verr := verifySession(ck.Value, c.Secret); verr == nil {
				// Per-user visit tracking: count an authenticated load of the
				// site root, mirroring the site-wide counter in staticHandler.
				if r.Method == http.MethodGet &&
					(r.URL.Path == "/" || r.URL.Path == "/index.html") {
					recordUserVisit(claims.Email)
				}
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
	return signToken(payload, purposeKey(c.Secret, "oauth-state")), nil
}

func (c authConfig) verifyState(token string) (stateClaims, error) {
	var s stateClaims
	payload, err := verifyToken(token, purposeKey(c.Secret, "oauth-state"))
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

// loginPage renders the self-contained themed demoscene sign-in page. redirect
// is already validated by the caller; it is URL-encoded into the Google start
// link, and html/template auto-escapes it for the attribute context.
func (c authConfig) loginPage(redirect string) []byte {
	startHref := "/auth/google/start?redirect=" + url.QueryEscape(redirect)
	var buf bytes.Buffer
	data := struct {
		StartHref       string
		AnonEnabled     bool
		Redirect        string
		AnonTTLHuman    string
		SessionTTLHuman string
	}{startHref, c.AnonEnabled, redirect, humanizeDuration(c.AnonTTL), humanizeDuration(c.SessionTTL)}
	if err := loginTmpl.Execute(&buf, data); err != nil {
		return []byte("sign-in temporarily unavailable")
	}
	return buf.Bytes()
}
