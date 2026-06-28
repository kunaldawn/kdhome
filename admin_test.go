package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// adminAuthConfig returns a test auth config with a configured super admin.
func adminAuthConfig() authConfig {
	c := testAuthConfig()
	c.SuperAdmin = "boss@kunaldawn.com"
	return c
}

func TestIsAdmin(t *testing.T) {
	c := adminAuthConfig()
	cases := map[string]bool{
		"boss@kunaldawn.com": true,
		"BOSS@kunaldawn.com": true, // case-insensitive
		" boss@kunaldawn.com ": true,
		"someone@kunaldawn.com": false,
		"":                      false,
	}
	for email, want := range cases {
		if got := c.isAdmin(email); got != want {
			t.Errorf("isAdmin(%q) = %v, want %v", email, got, want)
		}
	}
	// Empty SuperAdmin must never grant admin.
	c.SuperAdmin = ""
	if c.isAdmin("") || c.isAdmin("boss@kunaldawn.com") {
		t.Error("no super admin configured should deny everyone")
	}
}

func TestRecordUserVisitTracksFirstAndCount(t *testing.T) {
	initVisitDB(t.TempDir())
	defer visitDB.Close()

	const email = "u@kunaldawn.com"
	// recordUserVisit runs async; poll until the first visit lands.
	recordUserVisit(email)
	waitForVisits(t, email, 1)

	var first0 int64
	visitDB.QueryRow(`SELECT first_login FROM users WHERE email = ?`, email).Scan(&first0)

	recordUserVisit(email)
	recordUserVisit(email)
	waitForVisits(t, email, 3)

	users, err := loadUsers()
	if err != nil {
		t.Fatalf("loadUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("want 1 user, got %d", len(users))
	}
	u := users[0]
	if u.Email != email {
		t.Errorf("email = %q", u.Email)
	}
	if u.TotalVisits != 3 {
		t.Errorf("total_visits = %d, want 3", u.TotalVisits)
	}
	if u.FirstLogin.Unix() != first0 {
		t.Errorf("first_login changed: %d -> %d", first0, u.FirstLogin.Unix())
	}
}

// waitForVisits blocks until email's total_visits reaches n (async upserts).
func waitForVisits(t *testing.T, email string, n int64) {
	t.Helper()
	for i := 0; i < 200; i++ {
		var got int64
		if err := visitDB.QueryRow(`SELECT total_visits FROM users WHERE email = ?`, email).Scan(&got); err == nil && got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d visits for %s", n, email)
}

func TestHandleMe(t *testing.T) {
	c := adminAuthConfig()
	tok, _ := signSession("boss@kunaldawn.com", time.Hour, c.Secret)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: tok})
	c.handleMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"isAdmin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json %q: %v", rec.Body.String(), err)
	}
	if body.Email != "boss@kunaldawn.com" || !body.IsAdmin {
		t.Fatalf("me = %+v", body)
	}
}

func TestHandleMeUnauthorized(t *testing.T) {
	c := adminAuthConfig()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	c.handleMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie should be 401, got %d", rec.Code)
	}
}

func TestHandleAdminForbiddenForNonAdmin(t *testing.T) {
	c := adminAuthConfig()
	tok, _ := signSession("rando@kunaldawn.com", time.Hour, c.Secret)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: tok})
	c.handleAdmin(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin should be 403, got %d", rec.Code)
	}
}

func TestHandleAdminRendersUsers(t *testing.T) {
	initVisitDB(t.TempDir())
	defer visitDB.Close()
	recordUserVisit("rando@kunaldawn.com")
	waitForVisits(t, "rando@kunaldawn.com", 1)

	c := adminAuthConfig()
	tok, _ := signSession("boss@kunaldawn.com", time.Hour, c.Secret)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: tok})
	c.handleAdmin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin should be 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, frag := range []string{"USER REGISTRY", "rando@kunaldawn.com", "Total Visits"} {
		if !strings.Contains(body, frag) {
			t.Errorf("admin page missing %q", frag)
		}
	}
}
