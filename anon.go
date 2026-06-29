package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"net"
	"net/http"
	"strings"
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
