package main

import (
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClientIPPrefersCFHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "/auth/anon/challenge", nil)
	r.RemoteAddr = "127.0.0.1:55000"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7", got)
	}
}

func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/auth/anon/challenge", nil)
	r.RemoteAddr = "198.51.100.9:42000"
	if got := clientIP(r); got != "198.51.100.9" {
		t.Errorf("clientIP = %q, want 198.51.100.9", got)
	}
}

func TestIPHashBindsToIP(t *testing.T) {
	secret := []byte("test-secret-test-secret")
	a := ipHash(secret, "203.0.113.7")
	b := ipHash(secret, "203.0.113.8")
	if a == b {
		t.Error("different IPs must hash differently")
	}
	if a != ipHash(secret, "203.0.113.7") {
		t.Error("same IP must hash stably")
	}
}

func TestLeadingZeroBits(t *testing.T) {
	cases := []struct {
		in   []byte
		want int
	}{
		{[]byte{0xff}, 0},
		{[]byte{0x0f}, 4},
		{[]byte{0x00, 0xff}, 8},
		{[]byte{0x00, 0x0f}, 12},
		{[]byte{0x00, 0x00}, 16},
	}
	for _, c := range cases {
		if got := leadingZeroBits(c.in); got != c.want {
			t.Errorf("leadingZeroBits(%x) = %d, want %d", c.in, got, c.want)
		}
	}
}

// subSolve brute-forces a solution for sub-puzzle j at subBits leading zeros.
func subSolve(t *testing.T, challenge string, j, subBits int) string {
	t.Helper()
	for i := 0; i < 1<<27; i++ {
		s := strconv.Itoa(i)
		sum := sha256.Sum256([]byte(challenge + ":" + strconv.Itoa(j) + ":" + s))
		if leadingZeroBits(sum[:]) >= subBits {
			return s
		}
	}
	t.Fatalf("no sub-solution at %d bits", subBits)
	return ""
}

// solveAnonMulti solves all k sub-puzzles for a challenge.
func solveAnonMulti(t *testing.T, challenge string, subBits, k int) []string {
	t.Helper()
	sols := make([]string, k)
	for j := 0; j < k; j++ {
		sols[j] = subSolve(t, challenge, j, subBits)
	}
	return sols
}

func TestPowMultiSolvedRoundTrip(t *testing.T) {
	challenge := "fixed-challenge-token"
	subBits, k := 12, 3
	sols := solveAnonMulti(t, challenge, subBits, k)
	if !powMultiSolved(challenge, sols, subBits, k) {
		t.Error("found solutions should validate")
	}
	// Wrong count must fail.
	if powMultiSolved(challenge, sols[:k-1], subBits, k) {
		t.Error("short solution set must be rejected")
	}
	// A corrupted entry must fail.
	bad := append([]string(nil), sols...)
	bad[1] = "definitely-not-valid"
	if powMultiSolved(challenge, bad, subBits, k) {
		t.Error("invalid sub-solution must be rejected")
	}
}

func anonTestCfg() authConfig {
	return authConfig{
		Secret:          []byte("test-secret-test-secret"),
		AnonPoWSubBits:  12,
		AnonPoWTargetMS: 7000,
		AnonPoWHashrate: 200000,
		AnonPoWKMin:     2,
		AnonPoWKMax:     8,
	}
}

func TestAnonChallengeRoundTrip(t *testing.T) {
	c := anonTestCfg()
	tok, err := c.signAnonChallenge("203.0.113.7", 12, 5)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	ch, err := c.verifyAnonChallenge(tok, "203.0.113.7")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ch.SubBits != 12 || ch.K != 5 {
		t.Errorf("subBits/k = %d/%d, want 12/5", ch.SubBits, ch.K)
	}
	if ch.Nonce == "" {
		t.Error("Nonce should be set")
	}
}

func TestAnonChallengeRejectsWrongIP(t *testing.T) {
	c := anonTestCfg()
	tok, _ := c.signAnonChallenge("203.0.113.7", 12, 5)
	if _, err := c.verifyAnonChallenge(tok, "203.0.113.8"); err == nil {
		t.Error("challenge bound to one IP must reject another IP")
	}
}

func TestAnonChallengeRejectsTamper(t *testing.T) {
	c := anonTestCfg()
	tok, _ := c.signAnonChallenge("203.0.113.7", 12, 5)
	if _, err := c.verifyAnonChallenge(tok+"x", "203.0.113.7"); err == nil {
		t.Error("tampered challenge must fail signature check")
	}
}

func TestAnonChallengeRejectsExpired(t *testing.T) {
	c := anonTestCfg()
	payload, _ := json.Marshal(anonChallenge{
		Nonce: "n", IPHash: ipHash(c.Secret, "203.0.113.7"), SubBits: 12, K: 5,
		Purpose: "anon-pow", IssuedAt: time.Now().Add(-10 * time.Minute).Unix(),
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	// Sign with the SAME derived key verifyAnonChallenge uses, so the signature
	// passes and the expiry branch is actually exercised (not short-circuited by
	// a signature mismatch).
	tok := signToken(payload, anonChallengeKey(c.Secret))
	if _, err := c.verifyAnonChallenge(tok, "203.0.113.7"); err == nil {
		t.Error("expired challenge must be rejected")
	}
}

func TestAnonGuardConsumeSingleUse(t *testing.T) {
	g := newAnonGuard()
	exp := time.Now().Add(time.Minute).Unix()
	if !g.consume("nonce-1", exp) {
		t.Error("first consume should succeed")
	}
	if g.consume("nonce-1", exp) {
		t.Error("second consume of same nonce must fail (replay)")
	}
}

func TestAnonGuardConsumeEvictsExpired(t *testing.T) {
	g := newAnonGuard()
	past := time.Now().Add(-time.Minute).Unix()
	g.consume("old", past) // inserted but already expired
	// A fresh consume triggers eviction; reusing "old" should be allowed again.
	if !g.consume("old", time.Now().Add(time.Minute).Unix()) {
		t.Error("expired nonce should have been evicted and reusable")
	}
}

func TestAnonGuardKEscalate(t *testing.T) {
	g := newAnonGuard()
	baseK := 8
	kMax := baseK + 5*anonKEscalationStep
	if got := g.kFor("203.0.113.7", baseK, kMax); got != baseK {
		t.Errorf("first request k = %d, want %d", got, baseK)
	}
	for i := 0; i < 3; i++ {
		g.recordMint("203.0.113.7")
	}
	want := baseK + 3*anonKEscalationStep
	if got := g.kFor("203.0.113.7", baseK, kMax); got != want {
		t.Errorf("after 3 mints k = %d, want %d", got, want)
	}
	for i := 0; i < 50; i++ {
		g.recordMint("203.0.113.7")
	}
	if got := g.kFor("203.0.113.7", baseK, kMax); got != kMax {
		t.Errorf("k should cap at kMax %d, got %d", kMax, got)
	}
}

func TestAnonGuardKPerIP(t *testing.T) {
	g := newAnonGuard()
	g.recordMint("203.0.113.7")
	g.recordMint("203.0.113.7")
	if got := g.kFor("198.51.100.9", 8, 64); got != 8 {
		t.Errorf("untouched IP should be base 8, got %d", got)
	}
}

func TestHandleAnonChallengeReturnsToken(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	r := httptest.NewRequest("POST", "/auth/anon/challenge", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	r.Header.Set("X-KD-Anon", "1")
	w := httptest.NewRecorder()
	c.handleAnonChallenge(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Challenge string `json:"challenge"`
		SubBits   int    `json:"subBits"`
		K         int    `json:"k"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Challenge == "" || resp.SubBits != 12 || resp.K < c.AnonPoWKMin {
		t.Errorf("resp = %+v, want challenge set, subBits 12, k>=%d", resp, c.AnonPoWKMin)
	}
}

// redeemBody solves a challenge and returns a JSON redeem body.
func redeemBody(t *testing.T, chTok string, subBits, k int, redirect string) string {
	t.Helper()
	sols := solveAnonMulti(t, chTok, subBits, k)
	b, _ := json.Marshal(map[string]any{"challenge": chTok, "solutions": sols, "redirect": redirect})
	return string(b)
}

func TestHandleAnonRedeemHappyPath(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	c.CookieName = "kd_session"
	c.CookieDomain = ".kunaldawn.com"
	c.AnonTTL = 30 * time.Minute
	c.BaseURL = "https://kunaldawn.com"
	// Mint a low-difficulty challenge directly for a fast test.
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12, 3)
	body := redeemBody(t, chTok, 12, 3, "/wiki")
	r := httptest.NewRequest("POST", "/auth/anon/redeem", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	r.Header.Set("X-KD-Anon", "1")
	w := httptest.NewRecorder()
	c.handleAnonRedeem(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var set bool
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "kd_session" {
			set = true
			if claims, err := verifySession(ck.Value, c.Secret); err != nil || !claims.Anon {
				t.Errorf("cookie not a valid anon session: %v", err)
			}
		}
	}
	if !set {
		t.Error("kd_session cookie not set")
	}
}

func TestHandleAnonRedeemRejectsReplay(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	c.CookieName = "kd_session"
	c.AnonTTL = 30 * time.Minute
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12, 3)
	body := redeemBody(t, chTok, 12, 3, "/")
	mk := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/auth/anon/redeem", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("CF-Connecting-IP", "203.0.113.7")
		r.Header.Set("X-KD-Anon", "1")
		w := httptest.NewRecorder()
		c.handleAnonRedeem(w, r)
		return w
	}
	if mk().Code != 200 {
		t.Fatal("first redeem should succeed")
	}
	if mk().Code == 200 {
		t.Error("replayed challenge must be rejected")
	}
}

func TestHandleAnonRedeemRejectsWrongIP(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	c.CookieName = "kd_session"
	c.AnonTTL = 30 * time.Minute
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12, 3)
	body := redeemBody(t, chTok, 12, 3, "/")
	r := httptest.NewRequest("POST", "/auth/anon/redeem", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.99") // different IP
	r.Header.Set("X-KD-Anon", "1")
	w := httptest.NewRecorder()
	c.handleAnonRedeem(w, r)
	if w.Code == 200 {
		t.Error("solution from a different IP must be rejected")
	}
}

func TestHandleAnonRedeemRejectsBadSolution(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	c.CookieName = "kd_session"
	c.AnonTTL = 30 * time.Minute
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12, 3)
	body := `{"challenge":"` + chTok + `","solutions":["bad","bad","bad"],"redirect":"/"}`
	r := httptest.NewRequest("POST", "/auth/anon/redeem", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	r.Header.Set("X-KD-Anon", "1")
	w := httptest.NewRecorder()
	c.handleAnonRedeem(w, r)
	if w.Code == 200 {
		t.Error("invalid PoW solution must be rejected")
	}
}
