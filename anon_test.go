package main

import (
	"net/http/httptest"
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
