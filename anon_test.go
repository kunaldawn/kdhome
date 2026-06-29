package main

import (
	"net/http/httptest"
	"strconv"
	"testing"
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
