package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// googleEndpoint is hardcoded to avoid importing golang.org/x/oauth2/google
// (and its transitive deps). These URLs are stable.
var googleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

// authConfig is the env-driven auth configuration, read once at startup.
type authConfig struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	Secret       []byte
	BaseURL      string
	CookieDomain string
	CookieName   string
	SessionTTL   time.Duration
	oauth        *oauth2.Config
	exchanger    tokenExchanger
}

// loadAuthConfig reads AUTH_* and GOOGLE_* env vars. AUTH_ENABLED is truthy on
// 1/true/on/yes. Missing optional values fall back to defaults.
func loadAuthConfig() authConfig {
	c := authConfig{
		BaseURL:      "https://kunaldawn.com",
		CookieDomain: ".kunaldawn.com",
		CookieName:   "kd_session",
		SessionTTL:   720 * time.Hour,
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_ENABLED"))) {
	case "1", "true", "on", "yes":
		c.Enabled = true
	}
	c.ClientID = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	c.ClientSecret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	if s := strings.TrimSpace(os.Getenv("AUTH_SECRET")); s != "" {
		c.Secret = []byte(s)
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_BASE_URL")); v != "" {
		c.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_COOKIE_DOMAIN")); v != "" {
		c.CookieDomain = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_COOKIE_NAME")); v != "" {
		c.CookieName = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_SESSION_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.SessionTTL = d
		}
	}
	c.oauth = &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  strings.TrimRight(c.BaseURL, "/") + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     googleEndpoint,
	}
	c.exchanger = googleExchanger{cfg: c.oauth}
	return c
}

// valid reports whether an enabled config has the secrets it needs to run.
func (c authConfig) valid() bool {
	return c.ClientID != "" && c.ClientSecret != "" && len(c.Secret) > 0
}

// baseDomain is the cookie domain without its leading dot (e.g. kunaldawn.com).
func (c authConfig) baseDomain() string {
	return strings.TrimPrefix(c.CookieDomain, ".")
}

// safeRedirect returns raw only if it is a same-site https URL or a
// root-relative path; otherwise it returns the configured base URL. Guards
// against open-redirect abuse of the post-login destination.
func (c authConfig) safeRedirect(raw string) string {
	def := c.BaseURL
	if raw == "" {
		return def
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return strings.TrimRight(c.BaseURL, "/") + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return def
	}
	host := u.Hostname()
	base := c.baseDomain()
	if host == base || strings.HasSuffix(host, "."+base) {
		return raw
	}
	return def
}

// tokenExchanger turns an OAuth authorization code into a verified email.
// Abstracted so handlers can be tested without contacting Google.
type tokenExchanger interface {
	exchange(ctx context.Context, code string) (email string, verified bool, err error)
}

type googleExchanger struct {
	cfg *oauth2.Config
}

func (g googleExchanger) exchange(ctx context.Context, code string) (string, bool, error) {
	tok, err := g.cfg.Exchange(ctx, code)
	if err != nil {
		return "", false, err
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", false, errors.New("no id_token in token response")
	}
	return emailFromIDToken(raw)
}

// emailFromIDToken decodes the id_token payload (without re-verifying Google's
// signature — the token arrived directly from Google's token endpoint over
// TLS) and returns the email plus its verified flag. Google sends
// email_verified as either a bool or the string "true".
func emailFromIDToken(idToken string) (string, bool, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", false, errors.New("malformed id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false, err
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false, err
	}
	verified := false
	switch v := claims.EmailVerified.(type) {
	case bool:
		verified = v
	case string:
		verified = v == "true"
	}
	return claims.Email, verified, nil
}
