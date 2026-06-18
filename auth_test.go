package main

import (
	"os"
	"testing"
	"time"
)

func clearAuthEnv() {
	for _, k := range []string{"AUTH_ENABLED", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
		"AUTH_SECRET", "AUTH_BASE_URL", "AUTH_COOKIE_DOMAIN", "AUTH_COOKIE_NAME", "AUTH_SESSION_TTL"} {
		os.Unsetenv(k)
	}
}

func TestLoadAuthConfigDefaults(t *testing.T) {
	clearAuthEnv()
	c := loadAuthConfig()
	if c.Enabled {
		t.Fatal("should be disabled by default")
	}
	if c.CookieName != "kd_session" {
		t.Fatalf("cookie name = %q", c.CookieName)
	}
	if c.CookieDomain != ".kunaldawn.com" {
		t.Fatalf("cookie domain = %q", c.CookieDomain)
	}
	if c.BaseURL != "https://kunaldawn.com" {
		t.Fatalf("base url = %q", c.BaseURL)
	}
	if c.SessionTTL != 720*time.Hour {
		t.Fatalf("ttl = %v", c.SessionTTL)
	}
}

func TestLoadAuthConfigEnabledRequiresSecrets(t *testing.T) {
	clearAuthEnv()
	os.Setenv("AUTH_ENABLED", "on")
	defer clearAuthEnv()
	c := loadAuthConfig()
	if !c.Enabled {
		t.Fatal("AUTH_ENABLED=on should enable")
	}
	if c.valid() {
		t.Fatal("missing secrets must be invalid")
	}
	os.Setenv("GOOGLE_CLIENT_ID", "cid")
	os.Setenv("GOOGLE_CLIENT_SECRET", "csec")
	os.Setenv("AUTH_SECRET", "shh")
	if !loadAuthConfig().valid() {
		t.Fatal("all secrets present must be valid")
	}
}

func testAuthConfig() authConfig {
	return authConfig{
		Enabled:      true,
		ClientID:     "cid",
		ClientSecret: "csec",
		Secret:       []byte("test-secret"),
		BaseURL:      "https://kunaldawn.com",
		CookieDomain: ".kunaldawn.com",
		CookieName:   "kd_session",
		SessionTTL:   time.Hour,
	}
}

func TestSafeRedirect(t *testing.T) {
	c := testAuthConfig()
	cases := []struct{ in, want string }{
		{"https://kunaldawn.com/x", "https://kunaldawn.com/x"},
		{"https://wiki.kunaldawn.com/a?b=c", "https://wiki.kunaldawn.com/a?b=c"},
		{"https://evil.com/", "https://kunaldawn.com"},
		{"http://kunaldawn.com/x", "https://kunaldawn.com"},
		{"//evil.com", "https://kunaldawn.com"},
		{"", "https://kunaldawn.com"},
		{"/dashboard", "https://kunaldawn.com/dashboard"},
	}
	for _, tc := range cases {
		if got := c.safeRedirect(tc.in); got != tc.want {
			t.Errorf("safeRedirect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEmailFromIDToken(t *testing.T) {
	payload := b64.EncodeToString([]byte(`{"email":"a@b.com","email_verified":true}`))
	email, verified, err := emailFromIDToken("h." + payload + ".s")
	if err != nil || email != "a@b.com" || !verified {
		t.Fatalf("bool form: got %q %v %v", email, verified, err)
	}
	payload2 := b64.EncodeToString([]byte(`{"email":"c@d.com","email_verified":"true"}`))
	email2, v2, _ := emailFromIDToken("h." + payload2 + ".s")
	if email2 != "c@d.com" || !v2 {
		t.Fatalf("string form: got %q %v", email2, v2)
	}
}
