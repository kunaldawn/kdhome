package main

import (
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

func TestPowSolvedRoundTrip(t *testing.T) {
	challenge := "fixed-challenge-token"
	// Brute-force a 12-bit solution (cheap, deterministic in test).
	var solution string
	for i := 0; i < 1<<24; i++ {
		s := strconv.Itoa(i)
		if powSolved(challenge, s, 12) {
			solution = s
			break
		}
	}
	if solution == "" {
		t.Fatal("no 12-bit solution found")
	}
	if !powSolved(challenge, solution, 12) {
		t.Error("found solution should validate at 12 bits")
	}
	if powSolved(challenge, "0", 12) && solution != "0" {
		t.Error("solution '0' should almost never satisfy 12 bits")
	}
}

func anonTestCfg() authConfig {
	return authConfig{Secret: []byte("test-secret-test-secret"), AnonPoWBits: 20, AnonPoWCeil: 24}
}

func TestAnonChallengeRoundTrip(t *testing.T) {
	c := anonTestCfg()
	tok, err := c.signAnonChallenge("203.0.113.7", 20)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	ch, err := c.verifyAnonChallenge(tok, "203.0.113.7")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ch.Bits != 20 {
		t.Errorf("Bits = %d, want 20", ch.Bits)
	}
	if ch.Nonce == "" {
		t.Error("Nonce should be set")
	}
}

func TestAnonChallengeRejectsWrongIP(t *testing.T) {
	c := anonTestCfg()
	tok, _ := c.signAnonChallenge("203.0.113.7", 20)
	if _, err := c.verifyAnonChallenge(tok, "203.0.113.8"); err == nil {
		t.Error("challenge bound to one IP must reject another IP")
	}
}

func TestAnonChallengeRejectsTamper(t *testing.T) {
	c := anonTestCfg()
	tok, _ := c.signAnonChallenge("203.0.113.7", 20)
	if _, err := c.verifyAnonChallenge(tok+"x", "203.0.113.7"); err == nil {
		t.Error("tampered challenge must fail signature check")
	}
}

func TestAnonChallengeRejectsExpired(t *testing.T) {
	c := anonTestCfg()
	payload, _ := json.Marshal(anonChallenge{
		Nonce: "n", IPHash: ipHash(c.Secret, "203.0.113.7"), Bits: 20,
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

func TestAnonGuardBitsEscalate(t *testing.T) {
	g := newAnonGuard()
	base, ceil := 20, 24
	if got := g.bitsFor("203.0.113.7", base, ceil); got != base {
		t.Errorf("first request bits = %d, want %d", got, base)
	}
	for i := 0; i < 3; i++ {
		g.recordMint("203.0.113.7")
	}
	if got := g.bitsFor("203.0.113.7", base, ceil); got != base+3 {
		t.Errorf("after 3 mints bits = %d, want %d", got, base+3)
	}
	for i := 0; i < 20; i++ {
		g.recordMint("203.0.113.7")
	}
	if got := g.bitsFor("203.0.113.7", base, ceil); got != ceil {
		t.Errorf("bits should cap at ceil %d, got %d", ceil, got)
	}
}

func TestAnonGuardBitsPerIP(t *testing.T) {
	g := newAnonGuard()
	g.recordMint("203.0.113.7")
	g.recordMint("203.0.113.7")
	if got := g.bitsFor("198.51.100.9", 20, 24); got != 20 {
		t.Errorf("untouched IP should be base 20, got %d", got)
	}
}

func solveAnon(t *testing.T, challenge string, bits int) string {
	t.Helper()
	for i := 0; i < 1<<27; i++ {
		s := strconv.Itoa(i)
		if powSolved(challenge, s, bits) {
			return s
		}
	}
	t.Fatalf("no solution at %d bits", bits)
	return ""
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
		Bits      int    `json:"bits"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Challenge == "" || resp.Bits != 20 {
		t.Errorf("resp = %+v, want challenge set + bits 20", resp)
	}
}

func TestHandleAnonRedeemHappyPath(t *testing.T) {
	c := anonTestCfg()
	c.anonGuard = newAnonGuard()
	c.CookieName = "kd_session"
	c.CookieDomain = ".kunaldawn.com"
	c.AnonTTL = 30 * time.Minute
	c.BaseURL = "https://kunaldawn.com"
	// Mint a low-difficulty challenge directly for a fast test.
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12)
	sol := solveAnon(t, chTok, 12)
	body := `{"challenge":"` + chTok + `","solution":"` + sol + `","redirect":"/wiki"}`
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
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12)
	sol := solveAnon(t, chTok, 12)
	body := `{"challenge":"` + chTok + `","solution":"` + sol + `","redirect":"/"}`
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
	chTok, _ := c.signAnonChallenge("203.0.113.7", 12)
	sol := solveAnon(t, chTok, 12)
	body := `{"challenge":"` + chTok + `","solution":"` + sol + `","redirect":"/"}`
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
	chTok, _ := c.signAnonChallenge("203.0.113.7", 20)
	body := `{"challenge":"` + chTok + `","solution":"definitely-not-valid","redirect":"/"}`
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
