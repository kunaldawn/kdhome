package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// b64 is the JWT segment encoding (base64url, no padding).
var b64 = base64.RawURLEncoding

const sessionIssuer = "kunaldawn.com"

// sessionClaims is the session JWT payload. Standard registered claim names
// are used so subdomain apps can verify the cookie with any off-the-shelf JWT
// library (HS256 + the shared AUTH_SECRET).
type sessionClaims struct {
	Subject   string `json:"sub"`
	Email     string `json:"email"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Issuer    string `json:"iss"`
}

// sign returns the base64url HMAC-SHA256 of input under secret.
func sign(input string, secret []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(input))
	return b64.EncodeToString(m.Sum(nil))
}

// signToken frames payload as an HS256 JWT and signs it.
func signToken(payload []byte, secret []byte) string {
	header := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	signingInput := header + "." + b64.EncodeToString(payload)
	return signingInput + "." + sign(signingInput, secret)
}

// verifyToken checks an HS256 JWT's signature (constant-time) and alg header,
// returning the decoded payload. It does NOT check exp — callers do.
func verifyToken(token string, secret []byte) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(sign(signingInput, secret)), []byte(parts[2])) {
		return nil, errors.New("bad signature")
	}
	hdrBytes, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("bad header encoding")
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return nil, errors.New("bad header json")
	}
	if hdr.Alg != "HS256" {
		return nil, errors.New("unexpected alg")
	}
	return b64.DecodeString(parts[1])
}

// signSession mints a session JWT for email, valid for ttl from now.
func signSession(email string, ttl time.Duration, secret []byte) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("empty signing secret")
	}
	now := time.Now()
	payload, err := json.Marshal(sessionClaims{
		Subject:   email,
		Email:     email,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		Issuer:    sessionIssuer,
	})
	if err != nil {
		return "", err
	}
	return signToken(payload, secret), nil
}

// verifySession validates a session JWT (signature, alg, expiry) and returns
// its claims.
func verifySession(token string, secret []byte) (sessionClaims, error) {
	var c sessionClaims
	payload, err := verifyToken(token, secret)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return c, errors.New("bad payload json")
	}
	if c.ExpiresAt <= time.Now().Unix() {
		return c, errors.New("token expired")
	}
	return c, nil
}
