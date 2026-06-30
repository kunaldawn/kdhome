package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/bits"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// peerIsTrusted reports whether the direct TCP peer is a loopback/private
// address — i.e. the cloudflared sidecar that fronts the origin. Only then do we
// trust CF-Connecting-IP; a direct public hit (origin reachable outside the
// tunnel) arrives from a public peer and cannot forge the header.
func peerIsTrusted(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	// RFC 6598 CGNAT 100.64.0.0/10 — used by some sidecar/pod networks and not
	// covered by net.IP.IsPrivate(). Without this, a cloudflared sidecar on a
	// CGNAT pod IP would be distrusted and all clients would collapse onto one
	// rate-limit/difficulty bucket.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}

// clientIP returns the real client IP. Behind the Cloudflare tunnel the only
// ingress (cloudflared, a local/private peer) sets CF-Connecting-IP, which
// external clients cannot forge. The header is honoured ONLY when the direct
// peer is trusted; otherwise we fall back to RemoteAddr.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" && peerIsTrusted(r.RemoteAddr) {
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// rateKey normalises an IP into the unit used for both challenge IP-binding and
// rate limiting: the full address for IPv4, but the /64 network for IPv6. A
// single IPv6 client routinely owns 2^64 addresses, so per-address keying would
// let it rotate freely past adaptive difficulty and the binding check.
func rateKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// ipHash is a truncated keyed HMAC of the normalised client key, used to bind a
// PoW challenge to its requester without storing the raw IP in the
// client-visible challenge.
func ipHash(secret []byte, key string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("anon-ip:" + key))
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

// powMultiSolved reports whether the client solved all k independent sub-puzzles:
// for every index j in [0,k), sha256(challenge + ":" + j + ":" + solutions[j])
// must have at least subBits leading zero bits.
//
// Splitting one big puzzle into k small ones keeps the SAME expected work
// (k·2^subBits hashes) and the SAME cheap verification (k hashes), but collapses
// the solve-time variance: a single-target search is geometric (coefficient of
// variation ≈ 1, so "sometimes <1s, sometimes never"), whereas the sum of k
// independent searches is Erlang-k with CV = 1/√k. At k≈64 the wall-clock lands
// in a tight band around the target instead of a heavy-tailed lottery.
func powMultiSolved(challenge string, solutions []string, subBits, k int) bool {
	if k <= 0 || len(solutions) != k {
		return false
	}
	for j := 0; j < k; j++ {
		// Cap each solution length so a hostile client can't force oversized
		// digest inputs (the array length is already pinned to the signed k).
		if len(solutions[j]) > 64 {
			return false
		}
		sum := sha256.Sum256([]byte(challenge + ":" + strconv.Itoa(j) + ":" + solutions[j]))
		if leadingZeroBits(sum[:]) < subBits {
			return false
		}
	}
	return true
}

// anonChallengeEpoch rotates the challenge-signing key once per process start, so
// outstanding challenges never survive a restart. Together with the in-memory
// used-nonce set (also reset on restart) this closes the post-restart replay
// window entirely.
var anonChallengeEpoch = func() string {
	n, err := randomNonce()
	if err != nil {
		// No safe fallback: a constant epoch would make the challenge key
		// deterministic across restarts and reopen the post-restart replay
		// window. crypto/rand failing at startup is catastrophic anyway.
		panic("anon: crypto/rand unavailable for challenge epoch: " + err.Error())
	}
	return n
}()

// anonChallengeKey is the per-purpose, per-process signing subkey for PoW
// challenges. Domain separation is critical: a challenge token MUST NOT verify
// as a session cookie. The session token keeps the raw secret (for subdomain
// compatibility); the challenge uses this derived key, so cross-use is
// impossible by construction — at the apex AND at every subdomain.
func anonChallengeKey(secret []byte) []byte {
	return purposeKey(secret, "anon-pow/"+anonChallengeEpoch)
}

// anonChallenge is the stateless, HMAC-signed PoW challenge. It binds to the
// requester's IP via IPHash so the grind cannot be offloaded to a remote solver
// pool, and carries a signed difficulty the client cannot lower.
type anonChallenge struct {
	Nonce     string `json:"n"`
	IPHash    string `json:"ih"`
	SubBits   int    `json:"d"` // leading-zero bits required per sub-puzzle
	K         int    `json:"k"` // number of independent sub-puzzles
	Purpose   string `json:"pur"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

const anonChallengePurpose = "anon-pow"
const anonChallengeTTL = 90 * time.Second

// signAnonChallenge mints a fresh signed challenge for ip: k sub-puzzles of
// subBits leading zero bits each.
func (c authConfig) signAnonChallenge(ip string, subBits, k int) (string, error) {
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	now := time.Now()
	payload, err := json.Marshal(anonChallenge{
		Nonce:     nonce,
		IPHash:    ipHash(c.Secret, rateKey(ip)),
		SubBits:   subBits,
		K:         k,
		Purpose:   anonChallengePurpose,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(anonChallengeTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return signToken(payload, anonChallengeKey(c.Secret)), nil
}

// verifyAnonChallenge validates signature, purpose, expiry, and that the
// challenge was issued to this same IP (constant-time).
func (c authConfig) verifyAnonChallenge(token, ip string) (anonChallenge, error) {
	var ch anonChallenge
	payload, err := verifyToken(token, anonChallengeKey(c.Secret))
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
	if !hmac.Equal([]byte(ch.IPHash), []byte(ipHash(c.Secret, rateKey(ip)))) {
		return ch, errors.New("ip mismatch")
	}
	return ch, nil
}

// ─── adaptive difficulty + replay + rate-limit state ───

const anonRateWindow = 10 * time.Minute         // adaptive-difficulty lookback
const anonChallengeRateWindow = 1 * time.Minute // challenge-issue + redeem rate window
const anonChallengeRateMax = 30                 // max challenge issues per key per window
const anonRedeemRateMax = 20                    // max redeem attempts per key per window
const anonSweepInterval = 2 * time.Minute       // global eviction cadence

// anonGuard holds the only mutable anon-login state: a per-key sliding window of
// recent successful mints (drives adaptive difficulty), a per-key window of
// recent challenge issues (rate limit), and a single-use nonce set (blocks
// challenge replay). All keyed by rateKey(ip). A periodic global sweep evicts
// stale entries so no map grows unbounded; no background goroutine.
type anonGuard struct {
	mu        sync.Mutex
	recent    map[string][]int64 // key -> recent successful-mint unix times
	chReq     map[string][]int64 // key -> recent challenge-issue unix times
	rdReq     map[string][]int64 // key -> recent redeem-attempt unix times
	used      map[string]int64   // nonce -> challenge exp unix
	lastSweep int64
}

func newAnonGuard() *anonGuard {
	return &anonGuard{
		recent: map[string][]int64{},
		chReq:  map[string][]int64{},
		rdReq:  map[string][]int64{},
		used:   map[string]int64{},
	}
}

// pruneWindow drops timestamps at or before cutoff, reusing the backing array.
func pruneWindow(times []int64, cutoff int64) []int64 {
	kept := times[:0]
	for _, t := range times {
		if t > cutoff {
			kept = append(kept, t)
		}
	}
	return kept
}

// sweep globally evicts fully-expired entries from every map. The caller must
// hold mu. It runs at most once per anonSweepInterval so amortised cost stays
// low even though each pass is O(total entries).
func (g *anonGuard) sweep(now int64) {
	if now-g.lastSweep < int64(anonSweepInterval.Seconds()) {
		return
	}
	g.lastSweep = now
	recCut := now - int64(anonRateWindow.Seconds())
	for k, v := range g.recent {
		if kept := pruneWindow(v, recCut); len(kept) == 0 {
			delete(g.recent, k)
		} else {
			g.recent[k] = kept
		}
	}
	chCut := now - int64(anonChallengeRateWindow.Seconds())
	for k, v := range g.chReq {
		if kept := pruneWindow(v, chCut); len(kept) == 0 {
			delete(g.chReq, k)
		} else {
			g.chReq[k] = kept
		}
	}
	for k, v := range g.rdReq {
		if kept := pruneWindow(v, chCut); len(kept) == 0 {
			delete(g.rdReq, k)
		} else {
			g.rdReq[k] = kept
		}
	}
	for n, e := range g.used {
		if e <= now {
			delete(g.used, n)
		}
	}
}

// allowChallenge applies a per-key sliding-window rate limit to challenge
// issuance, returning false when the limit is exceeded.
func (g *anonGuard) allowChallenge(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().Unix()
	g.sweep(now)
	cutoff := now - int64(anonChallengeRateWindow.Seconds())
	kept := pruneWindow(g.chReq[key], cutoff)
	if len(kept) >= anonChallengeRateMax {
		g.chReq[key] = kept
		return false
	}
	g.chReq[key] = append(kept, now)
	return true
}

// allowRedeem applies a per-key sliding-window rate limit to redeem attempts so
// a single valid challenge token can't fuel an unbounded verify+PoW CPU flood.
func (g *anonGuard) allowRedeem(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().Unix()
	g.sweep(now)
	cutoff := now - int64(anonChallengeRateWindow.Seconds())
	kept := pruneWindow(g.rdReq[key], cutoff)
	if len(kept) >= anonRedeemRateMax {
		g.rdReq[key] = kept
		return false
	}
	g.rdReq[key] = append(kept, now)
	return true
}

// kFor returns the adaptive sub-puzzle count for key: baseK (already sized to the
// caller's device) plus a LINEAR penalty for each recent successful mint, capped
// at kMax. Escalating on mints (real, completed logins) — not on challenge issues
// — is deliberate: the old bits-based scheme escalated at ISSUE time, so a user
// whose first grind ran long and who simply refreshed the page kept re-issuing
// challenges and ratcheted their own difficulty toward the ceiling (a self-DoS).
// Challenge-issue bursts are still bounded, by the allowChallenge rate limit.
// Also prunes the mint window for key.
func (g *anonGuard) kFor(key string, baseK, kMax int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().Unix()
	g.sweep(now)
	cutoff := now - int64(anonRateWindow.Seconds())
	kept := pruneWindow(g.recent[key], cutoff)
	if len(kept) == 0 {
		delete(g.recent, key)
	} else {
		g.recent[key] = kept
	}
	k := baseK + len(kept)*anonKEscalationStep
	if k > kMax {
		k = kMax
	}
	return k
}

// recordMint logs a successful mint for key.
func (g *anonGuard) recordMint(key string) {
	g.mu.Lock()
	g.recent[key] = append(g.recent[key], time.Now().Unix())
	g.mu.Unlock()
}

// consume marks nonce used until exp and returns true; returns false on replay
// or if the challenge has already expired under this lock's clock. Checking
// expiry here (same lock, same now) closes the verify→consume boundary straddle:
// the old code evicted entries with e<=now and could re-insert a nonce whose exp
// equalled now, double-spending one PoW solve. Eviction is left to the periodic
// sweep, keeping this O(1) under the lock.
func (g *anonGuard) consume(nonce string, exp int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().Unix()
	if exp <= now {
		return false
	}
	g.sweep(now)
	if _, seen := g.used[nonce]; seen {
		return false
	}
	g.used[nonce] = exp
	return true
}

// ─── handlers ───

// anonCSRFHeader is a custom header the browser client always sends on both anon
// endpoints. Requiring it blocks cross-site forgery: browsers won't attach a
// custom header to a cross-origin request without a CORS preflight the server
// never grants.
const anonCSRFHeader = "X-KD-Anon"

// handleAnonChallenge issues a fresh IP-bound PoW challenge at the adaptive
// difficulty for the caller's IP, subject to a per-IP issue-rate limit.
func (c authConfig) handleAnonChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get(anonCSRFHeader) == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ip := clientIP(r)
	key := rateKey(ip)
	if !c.anonGuard.allowChallenge(key) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	// Optional device benchmark: the client may report its measured SHA-256
	// hashrate so the server can size k to hit the target solve time on THAT
	// device. Absent or implausible values fall back to the assumed hashrate, and
	// the result is always clamped to [KMin, KMax] — KMin is a hard work floor, so
	// a client that under-reports (or omits) its rate still pays bot-grade cost.
	var req struct {
		HPS float64 `json:"hps"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&req) // optional body
	hps := req.HPS
	if !(hps > 0) {
		hps = c.AnonPoWHashrate
	}
	if hps > anonPoWHashrateCap {
		hps = anonPoWHashrateCap
	}
	baseK := powKForHashrate(hps, c.AnonPoWTargetMS, c.AnonPoWSubBits, c.AnonPoWKMin, c.AnonPoWKMax)
	k := c.anonGuard.kFor(key, baseK, c.AnonPoWKMax)
	tok, err := c.signAnonChallenge(ip, c.AnonPoWSubBits, k)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"challenge": tok, "subBits": c.AnonPoWSubBits, "k": k})
}

// handleAnonRedeem validates a solved challenge (signature, IP-binding, PoW,
// single-use) and, on success, mints an anonymous session cookie.
func (c authConfig) handleAnonRedeem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get(anonCSRFHeader) == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Challenge string   `json:"challenge"`
		Solutions []string `json:"solutions"`
		Redirect  string   `json:"redirect"`
	}
	// Solutions is one short integer string per sub-puzzle; KMax sub-puzzles at a
	// few bytes each fits comfortably under this cap.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	if !c.anonGuard.allowRedeem(rateKey(ip)) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	ch, err := c.verifyAnonChallenge(body.Challenge, ip)
	if err != nil {
		http.Error(w, "bad challenge", http.StatusForbidden)
		return
	}
	if !powMultiSolved(body.Challenge, body.Solutions, ch.SubBits, ch.K) {
		http.Error(w, "bad solution", http.StatusForbidden)
		return
	}
	if !c.anonGuard.consume(ch.Nonce, ch.ExpiresAt) {
		http.Error(w, "challenge already used", http.StatusForbidden)
		return
	}
	tok, err := signAnonSession(c.AnonTTL, c.Secret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	c.anonGuard.recordMint(rateKey(ip))
	http.SetCookie(w, &http.Cookie{
		Name:     c.CookieName,
		Value:    tok,
		Path:     "/",
		Domain:   c.CookieDomain,
		MaxAge:   int(c.AnonTTL.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"redirect": c.safeRedirect(body.Redirect)})
}
