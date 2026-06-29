package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/bits"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clientIP returns the real client IP. Behind the Cloudflare tunnel the only
// ingress sets CF-Connecting-IP, which external clients cannot forge (CF
// overwrites any client-supplied copy). Falls back to the RemoteAddr host.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ipHash is a truncated keyed HMAC of an IP, used to bind a PoW challenge to
// its requester without storing the raw IP in the (client-visible) challenge.
func ipHash(secret []byte, ip string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("anon-ip:" + ip))
	return b64.EncodeToString(m.Sum(nil)[:12])
}

// leadingZeroBits counts the leading zero bits of a byte slice.
func leadingZeroBits(b []byte) int {
	n := 0
	for _, by := range b {
		if by == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(by)
	}
	return n
}

// powSolved reports whether sha256(challenge + ":" + solution) has at least
// `difficulty` leading zero bits.
func powSolved(challenge, solution string, difficulty int) bool {
	sum := sha256.Sum256([]byte(challenge + ":" + solution))
	return leadingZeroBits(sum[:]) >= difficulty
}

// anonChallenge is the stateless, HMAC-signed PoW challenge. It binds to the
// requester's IP via IPHash so the grind cannot be offloaded to a remote
// solver pool, and carries a signed difficulty the client cannot lower.
type anonChallenge struct {
	Nonce     string `json:"n"`
	IPHash    string `json:"ih"`
	Bits      int    `json:"b"`
	Purpose   string `json:"pur"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

const anonChallengePurpose = "anon-pow"
const anonChallengeTTL = 90 * time.Second

// signAnonChallenge mints a fresh signed challenge for ip at the given
// difficulty.
func (c authConfig) signAnonChallenge(ip string, difficulty int) (string, error) {
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	now := time.Now()
	payload, err := json.Marshal(anonChallenge{
		Nonce:     nonce,
		IPHash:    ipHash(c.Secret, ip),
		Bits:      difficulty,
		Purpose:   anonChallengePurpose,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(anonChallengeTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return signToken(payload, c.Secret), nil
}

// verifyAnonChallenge validates signature, purpose, expiry, and that the
// challenge was issued to this same IP (constant-time).
func (c authConfig) verifyAnonChallenge(token, ip string) (anonChallenge, error) {
	var ch anonChallenge
	payload, err := verifyToken(token, c.Secret)
	if err != nil {
		return ch, err
	}
	if err := json.Unmarshal(payload, &ch); err != nil {
		return ch, errors.New("bad challenge payload")
	}
	if ch.Purpose != anonChallengePurpose {
		return ch, errors.New("wrong purpose")
	}
	if ch.ExpiresAt <= time.Now().Unix() {
		return ch, errors.New("challenge expired")
	}
	if !hmac.Equal([]byte(ch.IPHash), []byte(ipHash(c.Secret, ip))) {
		return ch, errors.New("ip mismatch")
	}
	return ch, nil
}

// anonRateWindow is how far back per-IP mints count toward difficulty.
const anonRateWindow = 10 * time.Minute

// anonGuard holds the only mutable anon-login state: a per-IP sliding window of
// recent successful mints (drives adaptive difficulty) and a single-use nonce
// set (blocks challenge replay). Both evict on access; no background goroutine.
type anonGuard struct {
	mu     sync.Mutex
	recent map[string][]int64 // ip -> recent mint unix times
	used   map[string]int64   // nonce -> challenge exp unix
}

func newAnonGuard() *anonGuard {
	return &anonGuard{recent: map[string][]int64{}, used: map[string]int64{}}
}

// bitsFor returns the adaptive difficulty for ip: base + (recent mints in
// window), capped at ceil. It also prunes the window for ip.
func (g *anonGuard) bitsFor(ip string, base, ceil int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := time.Now().Add(-anonRateWindow).Unix()
	kept := g.recent[ip][:0]
	for _, t := range g.recent[ip] {
		if t > cutoff {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(g.recent, ip)
	} else {
		g.recent[ip] = kept
	}
	difficulty := base + len(kept)
	if difficulty > ceil {
		difficulty = ceil
	}
	return difficulty
}

// recordMint logs a successful mint for ip.
func (g *anonGuard) recordMint(ip string) {
	g.mu.Lock()
	g.recent[ip] = append(g.recent[ip], time.Now().Unix())
	g.mu.Unlock()
}

// consume marks nonce used until exp and returns true; returns false if the
// nonce was already used (replay). Expired entries are evicted on each call.
func (g *anonGuard) consume(nonce string, exp int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().Unix()
	for n, e := range g.used {
		if e <= now {
			delete(g.used, n)
		}
	}
	if _, seen := g.used[nonce]; seen {
		return false
	}
	g.used[nonce] = exp
	return true
}
