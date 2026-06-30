package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

// userRow is one tracked authenticated user, as shown on the admin page.
type userRow struct {
	Email       string
	FirstLogin  time.Time
	LastVisit   time.Time
	TotalVisits int64
}

// ensureUsersTable creates the per-user tracking table. Called from initVisitDB
// after the shared visitDB handle is open. One row per authenticated email.
func ensureUsersTable() {
	if visitDB == nil {
		return
	}
	if _, err := visitDB.Exec(`CREATE TABLE IF NOT EXISTS users (
		email        TEXT PRIMARY KEY,
		first_login  INTEGER NOT NULL,
		last_visit   INTEGER NOT NULL,
		total_visits INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		log.Printf("[USERS] failed to create users table: %v", err)
	}
}

// recordUserVisit upserts a visit for email: the first sighting sets first_login
// and seeds total_visits at 1; every later sighting bumps last_visit and the
// counter. Runs async like recordVisit so it never blocks the request path.
func recordUserVisit(email string) {
	if visitDB == nil || email == "" {
		return
	}
	now := time.Now().Unix()
	go func() {
		if _, err := visitDB.Exec(`INSERT INTO users (email, first_login, last_visit, total_visits)
			VALUES (?, ?, ?, 1)
			ON CONFLICT(email) DO UPDATE SET
				last_visit   = excluded.last_visit,
				total_visits = users.total_visits + 1`,
			email, now, now); err != nil {
			log.Printf("[USERS] visit upsert error: %v", err)
		}
	}()
}

// loadUsers returns every tracked user, most-recently-seen first.
func loadUsers() ([]userRow, error) {
	if visitDB == nil {
		return nil, nil
	}
	rows, err := visitDB.Query(`SELECT email, first_login, last_visit, total_visits
		FROM users ORDER BY last_visit DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userRow
	for rows.Next() {
		var u userRow
		var fl, lv int64
		if err := rows.Scan(&u.Email, &fl, &lv, &u.TotalVisits); err != nil {
			return nil, err
		}
		u.FirstLogin = time.Unix(fl, 0).UTC()
		u.LastVisit = time.Unix(lv, 0).UTC()
		out = append(out, u)
	}
	return out, rows.Err()
}

// isAdmin reports whether email is the configured super admin (case-insensitive).
func (c authConfig) isAdmin(email string) bool {
	return c.SuperAdmin != "" && strings.EqualFold(strings.TrimSpace(email), c.SuperAdmin)
}

// sessionEmail extracts the verified email from the request's session cookie.
// ok is false when the cookie is missing or the session fails verification.
//
// WARNING: ok is true for ANONYMOUS sessions too (anon:true, empty email). It
// answers "is there a valid session", NOT "is there a real user". To gate
// anything that requires a Google-identified user, use realUserEmail instead —
// never `email, ok := sessionEmail(r); if !ok { ... }; doPrivileged(email)`.
func (c authConfig) sessionEmail(r *http.Request) (string, bool) {
	ck, err := r.Cookie(c.CookieName)
	if err != nil {
		return "", false
	}
	claims, err := verifySession(ck.Value, c.Secret)
	if err != nil {
		return "", false
	}
	return claims.Email, true
}

// realUserEmail returns the verified email ONLY for a real (Google-identified,
// non-anonymous) session. Anonymous sessions and missing/invalid cookies all
// yield ok=false. Use this to gate anything requiring a real identity.
func (c authConfig) realUserEmail(r *http.Request) (string, bool) {
	ck, err := r.Cookie(c.CookieName)
	if err != nil {
		return "", false
	}
	claims, err := verifySession(ck.Value, c.Secret)
	if err != nil || claims.Anon || claims.Email == "" {
		return "", false
	}
	return claims.Email, true
}

// handleMe reports the current user's email and whether they're the super admin.
// Used by the desktop shell to conditionally reveal the Admin start-menu entry.
func (c authConfig) handleMe(w http.ResponseWriter, r *http.Request) {
	email, ok := c.sessionEmail(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"isAdmin"`
	}{email, c.isAdmin(email)}); err != nil {
		log.Printf("[ADMIN] /api/me render error: %v", err)
	}
}

// handleAdmin renders the user-tracking table. Gated to the super admin only;
// anyone else logged in gets a themed 403.
func (c authConfig) handleAdmin(w http.ResponseWriter, r *http.Request) {
	email, ok := c.sessionEmail(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !c.isAdmin(email) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(adminForbiddenHTML))
		return
	}
	users, err := loadUsers()
	if err != nil {
		log.Printf("[ADMIN] loadUsers error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	view := struct {
		Admin string
		Users []userRow
		Now   string
	}{email, users, time.Now().UTC().Format("2006-01-02 15:04 UTC")}
	if err := adminTmpl.Execute(w, view); err != nil {
		log.Printf("[ADMIN] render error: %v", err)
	}
}

// fmtTime renders a UTC timestamp for the admin table.
func fmtTime(t time.Time) string { return t.Format("2006-01-02 15:04 UTC") }

var adminTmpl = template.Must(template.New("admin").
	Funcs(template.FuncMap{"fmtTime": fmtTime}).
	Parse(adminHTML))

const adminForbiddenHTML = `<!doctype html><html><head><meta charset="utf-8">` +
	`<title>403 — forbidden</title><style>html,body{margin:0;height:100%;background:#05080a;` +
	`color:#ff48a0;font-family:'Share Tech Mono',ui-monospace,monospace;display:flex;` +
	`align-items:center;justify-content:center;text-shadow:0 0 10px rgba(255,72,160,.5)}` +
	`</style></head><body>403 — ACCESS DENIED · admins only</body></html>`

const adminHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Admin — KD Vault</title>
<style>
  :root { --grn:#00ff99; --dim:#b9e8d2; --pnk:#ff48a0; }
  * { box-sizing: border-box; }
  html, body { margin: 0; background: #05080a; color: var(--dim);
    font-family: 'Share Tech Mono', ui-monospace, Menlo, Consolas, monospace; }
  body { padding: 28px 20px 56px; }
  .wrap { max-width: 920px; margin: 0 auto; }
  h1 { color: var(--grn); font-size: 20px; letter-spacing: 1px; margin: 0 0 4px;
    text-shadow: 0 0 12px rgba(0,255,150,.45); }
  .meta { font-size: 12px; color: var(--dim); opacity: .8; margin-bottom: 22px; }
  .meta a { color: var(--pnk); text-decoration: none; }
  .meta a:hover { text-decoration: underline; }
  .stats { font-size: 12px; color: var(--grn); margin-bottom: 14px; opacity: .9; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  thead th { text-align: left; color: var(--grn); font-weight: 400;
    border-bottom: 1px solid rgba(0,255,150,.3); padding: 8px 10px; white-space: nowrap; }
  tbody td { padding: 8px 10px; border-bottom: 1px solid rgba(0,255,150,.08);
    white-space: nowrap; }
  tbody tr:hover { background: rgba(0,255,150,.04); }
  td.email { white-space: normal; word-break: break-all; color: #d8f5e8; }
  td.num { text-align: right; color: var(--grn); }
  .admin-tag { color: var(--pnk); font-size: 11px; margin-left: 6px; }
  .empty { padding: 28px 10px; opacity: .7; }
  @media (max-width: 560px) { thead th, tbody td { padding: 6px 6px; font-size: 12px; } }
</style>
</head>
<body>
  <div class="wrap">
    <h1>USER REGISTRY</h1>
    <div class="meta">signed in as {{.Admin}} · <a href="/">&laquo; back to vault</a> · <a href="/logout">sign out</a> · {{.Now}}</div>
    <div class="stats">{{len .Users}} user(s) tracked</div>
    {{if .Users}}
    <table>
      <thead>
        <tr><th>Email</th><th>First Login</th><th>Last Visit</th><th>Total Visits</th></tr>
      </thead>
      <tbody>
        {{range .Users}}
        <tr>
          <td class="email">{{.Email}}{{if eq .Email $.Admin}}<span class="admin-tag">[admin]</span>{{end}}</td>
          <td>{{fmtTime .FirstLogin}}</td>
          <td>{{fmtTime .LastVisit}}</td>
          <td class="num">{{.TotalVisits}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">No users tracked yet.</div>
    {{end}}
  </div>
</body>
</html>`
