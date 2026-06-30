package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ─── Token type confusion (the CRITICAL fix) ───

// A PoW challenge token must NOT be accepted as a session cookie. Before the fix
// it was: same key, and verifySession checked only signature + exp.
func TestChallengeTokenRejectedAsSession(t *testing.T) {
	c := anonTestCfg()
	tok, err := c.signAnonChallenge("203.0.113.7", 12, 3)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}
	if _, err := verifySession(tok, c.Secret); err == nil {
		t.Fatal("challenge token was accepted as a session — type confusion bypass")
	}
}

// An OAuth-state token must NOT be accepted as a session cookie.
func TestStateTokenRejectedAsSession(t *testing.T) {
	c := anonTestCfg()
	tok, err := c.signState("nonce123", "https://kunaldawn.com/")
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}
	if _, err := verifySession(tok, c.Secret); err == nil {
		t.Fatal("state token was accepted as a session — type confusion bypass")
	}
}

// A real session still verifies, and one with the wrong issuer is rejected.
func TestVerifySessionEnforcesIssuer(t *testing.T) {
	secret := []byte("test-secret-test-secret")
	good, err := signSession("u@kunaldawn.com", time.Hour, secret)
	if err != nil {
		t.Fatalf("sign session: %v", err)
	}
	if _, err := verifySession(good, secret); err != nil {
		t.Fatalf("legit session rejected: %v", err)
	}
	// Same key + future exp but no/blank issuer (shape of a foreign token).
	payload := []byte(`{"sub":"x","email":"","iat":1,"exp":` +
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`)
	foreign := signToken(payload, secret)
	if _, err := verifySession(foreign, secret); err == nil {
		t.Fatal("raw-key token without the session issuer was accepted")
	}
}

// ─── IPv6 /64 normalisation ───

func TestRateKeyIPv6Slash64(t *testing.T) {
	a := rateKey("2001:db8:1:2:aaaa:bbbb:cccc:dddd")
	b := rateKey("2001:db8:1:2:1111:2222:3333:4444") // same /64
	d := rateKey("2001:db8:1:3::1")                  // different /64
	if a != b {
		t.Errorf("same /64 should share a key: %q vs %q", a, b)
	}
	if a == d {
		t.Errorf("different /64 must differ: %q == %q", a, d)
	}
	if !strings.HasSuffix(a, "/64") {
		t.Errorf("ipv6 key should be a /64: %q", a)
	}
	if got := rateKey("203.0.113.7"); got != "203.0.113.7" {
		t.Errorf("ipv4 key = %q, want full address", got)
	}
}

// ─── CF-Connecting-IP trust (only from a private/loopback peer) ───

func TestClientIPIgnoresCFFromPublicPeer(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", nil)
	r.RemoteAddr = "198.51.100.9:5000" // public peer (direct hit)
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Errorf("clientIP = %q, want the real peer (CF header must be ignored)", got)
	}
}

func TestClientIPTrustsCFFromPrivatePeer(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", nil)
	r.RemoteAddr = "10.1.2.3:5000" // private peer (tunnel sidecar)
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7", got)
	}
}

// ─── Challenge issue rate limit ───

func TestAllowChallengeRateLimit(t *testing.T) {
	g := newAnonGuard()
	key := "203.0.113.7"
	for i := 0; i < anonChallengeRateMax; i++ {
		if !g.allowChallenge(key) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if g.allowChallenge(key) {
		t.Fatal("request over the limit should be blocked")
	}
	// A different key is unaffected.
	if !g.allowChallenge("198.51.100.1") {
		t.Fatal("a different key must have its own budget")
	}
}

func TestHandleAnonChallengeRateLimited(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	var last int
	for i := 0; i < anonChallengeRateMax+1; i++ {
		r := httptest.NewRequest("POST", "/auth/anon/challenge", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("CF-Connecting-IP", "203.0.113.7")
		r.Header.Set("X-KD-Anon", "1")
		w := httptest.NewRecorder()
		c.handleAnonChallenge(w, r)
		last = w.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last status = %d, want 429", last)
	}
}

// ─── CSRF header required ───

func TestAnonEndpointsRequireCSRFHeader(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	for _, path := range []string{"/auth/anon/challenge", "/auth/anon/redeem"} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.RemoteAddr = "127.0.0.1:1234"
		// no X-KD-Anon header
		w := httptest.NewRecorder()
		if strings.HasSuffix(path, "challenge") {
			c.handleAnonChallenge(w, r)
		} else {
			c.handleAnonRedeem(w, r)
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("%s without CSRF header = %d, want 403", path, w.Code)
		}
	}
}

// ─── Global eviction (unbounded-growth fix) ───

// ─── consume() expiry-boundary double-spend fix ───

func TestConsumeRejectsExpiredChallenge(t *testing.T) {
	g := newAnonGuard()
	now := time.Now().Unix()
	if g.consume("n1", now-1) {
		t.Fatal("consume must reject an already-expired challenge (boundary straddle)")
	}
	if !g.consume("n2", now+60) {
		t.Fatal("consume must accept a live challenge")
	}
	if g.consume("n2", now+60) {
		t.Fatal("consume must reject a replay of a live nonce")
	}
}

// ─── adaptive difficulty: escalate on mints, NOT on challenge-issue bursts ───

// Regression guard: the old scheme escalated at challenge-ISSUE time, so a user
// whose grind ran long and who simply refreshed re-issued challenges and ratcheted
// their own difficulty into a self-DoS. kFor must ignore issue bursts entirely;
// only completed mints (real logins) raise k.
func TestKForIgnoresChallengeBurst(t *testing.T) {
	g := newAnonGuard()
	key := "203.0.113.7"
	baseK := 8
	if k := g.kFor(key, baseK, 999); k != baseK {
		t.Fatalf("fresh kFor = %d, want %d (base)", k, baseK)
	}
	for i := 0; i < 10; i++ {
		g.allowChallenge(key) // a 10-challenge burst (refreshes/retries)
	}
	if k := g.kFor(key, baseK, 999); k != baseK {
		t.Fatalf("after a challenge burst kFor = %d, want %d — issue bursts must NOT escalate", k, baseK)
	}
	// A completed mint, however, does raise k.
	g.recordMint(key)
	if k := g.kFor(key, baseK, 999); k != baseK+anonKEscalationStep {
		t.Fatalf("after a mint kFor = %d, want %d", k, baseK+anonKEscalationStep)
	}
}

// ─── redeem rate limit ───

func TestAllowRedeemRateLimit(t *testing.T) {
	g := newAnonGuard()
	key := "203.0.113.7"
	for i := 0; i < anonRedeemRateMax; i++ {
		if !g.allowRedeem(key) {
			t.Fatalf("redeem %d should be allowed", i)
		}
	}
	if g.allowRedeem(key) {
		t.Fatal("redeem over the limit must be blocked")
	}
}

// ─── real-user vs anon session (footgun hardening) ───

func TestRealUserEmailRejectsAnon(t *testing.T) {
	c := authConfig{Secret: []byte("test-secret-test-secret"), CookieName: "kd_session"}
	good, _ := signSession("u@kunaldawn.com", time.Hour, c.Secret)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "kd_session", Value: good})
	if e, ok := c.realUserEmail(r); !ok || e != "u@kunaldawn.com" {
		t.Errorf("real user = (%q,%v), want (u@kunaldawn.com,true)", e, ok)
	}
	anon, _ := signAnonSession(time.Hour, c.Secret)
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&http.Cookie{Name: "kd_session", Value: anon})
	if _, ok := c.realUserEmail(r2); ok {
		t.Error("anon session must NOT be treated as a real user")
	}
	if _, ok := c.sessionEmail(r2); !ok {
		t.Error("sessionEmail should still accept the anon session as a valid session")
	}
}

func TestGuardSweepEvictsStale(t *testing.T) {
	g := newAnonGuard()
	old := time.Now().Add(-2 * anonRateWindow).Unix()
	// Inject a stale recent entry + a stale challenge-window entry directly.
	g.recent["stale-ip"] = []int64{old}
	g.chReq["stale-ip"] = []int64{old}
	g.used["stale-nonce"] = old
	g.lastSweep = 0 // force the next sweep to run
	g.allowChallenge("trigger")
	g.mu.Lock()
	_, recLeft := g.recent["stale-ip"]
	_, chLeft := g.chReq["stale-ip"]
	_, usedLeft := g.used["stale-nonce"]
	g.mu.Unlock()
	if recLeft || chLeft || usedLeft {
		t.Errorf("sweep left stale entries: recent=%v chReq=%v used=%v", recLeft, chLeft, usedLeft)
	}
}
