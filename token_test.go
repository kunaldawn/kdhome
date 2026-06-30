package main

import (
	"strings"
	"testing"
	"time"
)

func TestSignVerifySession(t *testing.T) {
	secret := []byte("test-secret")
	tok, err := signSession("a@b.com", time.Hour, secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	c, err := verifySession(tok, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Email != "a@b.com" {
		t.Fatalf("email = %q", c.Email)
	}
	if c.Issuer != sessionIssuer {
		t.Fatalf("iss = %q, want %q", c.Issuer, sessionIssuer)
	}
}

func TestVerifyRejectsTampered(t *testing.T) {
	secret := []byte("test-secret")
	tok, _ := signSession("a@b.com", time.Hour, secret)
	parts := strings.Split(tok, ".")
	bad := parts[0] + "." + parts[1] + "x." + parts[2] // tamper payload segment
	if _, err := verifySession(bad, secret); err == nil {
		t.Fatal("tampered token must fail")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	tok, _ := signSession("a@b.com", time.Hour, []byte("secret-1"))
	if _, err := verifySession(tok, []byte("secret-2")); err == nil {
		t.Fatal("wrong secret must fail")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	secret := []byte("test-secret")
	tok, _ := signSession("a@b.com", -time.Minute, secret) // already expired
	if _, err := verifySession(tok, secret); err == nil {
		t.Fatal("expired token must fail")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if _, err := verifySession("only-one-part", []byte("s")); err == nil {
		t.Fatal("malformed must fail")
	}
	if _, err := verifySession("a.b.c.d", []byte("s")); err == nil {
		t.Fatal("4-part token must fail")
	}
}

func TestVerifyRejectsNonHS256(t *testing.T) {
	secret := []byte("s")
	header := b64.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := b64.EncodeToString([]byte(`{"email":"x@y.com","exp":99999999999}`))
	signingInput := header + "." + payload
	tok := signingInput + "." + sign(signingInput, secret) // signature is valid, but alg=none
	if _, err := verifySession(tok, secret); err == nil {
		t.Fatal("non-HS256 alg must be rejected")
	}
}

func TestSignAnonSession(t *testing.T) {
	secret := []byte("test-secret-test-secret")
	tok, err := signAnonSession(30*time.Minute, secret)
	if err != nil {
		t.Fatalf("signAnonSession: %v", err)
	}
	claims, err := verifySession(tok, secret)
	if err != nil {
		t.Fatalf("verifySession: %v", err)
	}
	if !claims.Anon {
		t.Error("Anon should be true")
	}
	if claims.Email != "" {
		t.Errorf("Email = %q, want empty", claims.Email)
	}
	if !strings.HasPrefix(claims.Subject, "anon:") {
		t.Errorf("Subject = %q, want anon: prefix", claims.Subject)
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		t.Error("token already expired")
	}
}

func TestSignAnonSessionUniqueSubjects(t *testing.T) {
	secret := []byte("test-secret-test-secret")
	a, _ := signAnonSession(time.Minute, secret)
	b, _ := signAnonSession(time.Minute, secret)
	ca, _ := verifySession(a, secret)
	cb, _ := verifySession(b, secret)
	if ca.Subject == cb.Subject {
		t.Error("two anon sessions share a subject; must be random")
	}
}
