package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsBot(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		// real browsers → human
		{"chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36", false},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0", false},
		{"ios_safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1", false},
		// crawlers caught by the generic "bot" token
		{"gptbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.1; +https://openai.com/gptbot", true},
		{"claudebot", "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", true},
		{"ahrefsbot", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", true},
		{"uptimerobot", "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)", true},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true},
		// caught by "spider" / "slurp"
		{"bytespider", "Mozilla/5.0 (Linux; Android 5.0) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; Bytespider; spider-feedback@bytedance.com)", true},
		{"slurp", "Mozilla/5.0 (compatible; Yahoo! Slurp; http://help.yahoo.com/help/us/ysearch/slurp)", true},
		// specific tokens NOT subsumed by a generic token (regression guards)
		{"curl", "curl/8.7.1", true},
		{"wget", "Wget/1.21.3", true},
		{"python_requests", "python-requests/2.31.0", true},
		{"go_http", "Go-http-client/2.0", true},
		{"java", "Java/11.0.2", true},
		{"headlesschrome", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/112.0.0.0 Safari/537.36", true},
		{"facebookexternalhit", "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", true},
		// no User-Agent at all
		{"empty", "", true},
		{"whitespace", "   ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isBot(c.ua); got != c.want {
				t.Errorf("isBot(%q) = %v, want %v", c.ua, got, c.want)
			}
		})
	}
}

func TestVisitHandlerJSON(t *testing.T) {
	atomic.StoreInt64(&humanCount, 7)
	atomic.StoreInt64(&botCount, 3)
	req := httptest.NewRequest(http.MethodGet, "/api/visit", nil)
	rec := httptest.NewRecorder()
	visitHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := rec.Body.String(), `{"humans":7,"bots":3}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestInitVisitDBResetAndHydrate(t *testing.T) {
	tmp := t.TempDir()

	// Seed a pre-split DB shaped like production: visit_count(id, n) with a
	// non-zero total and user_version still 0.
	seed, err := sql.Open("sqlite3", filepath.Join(tmp, "visits.db"))
	if err != nil {
		t.Fatalf("open seed: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE visit_count (id INTEGER PRIMARY KEY CHECK (id = 1), n INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO visit_count (id, n) VALUES (1, 500)`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	seed.Close()

	// First boot: migration should zero everything and bump user_version.
	initVisitDB(tmp)
	var n, human, bot, uv int64
	if err := visitDB.QueryRow(`SELECT n, human, bot FROM visit_count WHERE id = 1`).Scan(&n, &human, &bot); err != nil {
		t.Fatalf("read after init: %v", err)
	}
	visitDB.QueryRow(`PRAGMA user_version`).Scan(&uv)
	if n != 0 || human != 0 || bot != 0 {
		t.Fatalf("after reset got n=%d human=%d bot=%d, want 0/0/0", n, human, bot)
	}
	if uv != 1 {
		t.Fatalf("user_version = %d, want 1", uv)
	}
	if h, b := atomic.LoadInt64(&humanCount), atomic.LoadInt64(&botCount); h != 0 || b != 0 {
		t.Fatalf("atomics after reset = %d/%d, want 0/0", h, b)
	}

	// Accumulate, then re-init: the reset must NOT run again, and the atomics
	// must hydrate from disk.
	if _, err := visitDB.Exec(`UPDATE visit_count SET human = 5, bot = 3 WHERE id = 1`); err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	visitDB.Close()
	initVisitDB(tmp)
	if err := visitDB.QueryRow(`SELECT human, bot FROM visit_count WHERE id = 1`).Scan(&human, &bot); err != nil {
		t.Fatalf("read after re-init: %v", err)
	}
	if human != 5 || bot != 3 {
		t.Fatalf("reset re-ran: human=%d bot=%d, want 5/3", human, bot)
	}
	if h, b := atomic.LoadInt64(&humanCount), atomic.LoadInt64(&botCount); h != 5 || b != 3 {
		t.Fatalf("atomics after re-init = %d/%d, want 5/3", h, b)
	}
}

func TestRecordVisitRoutesToBucket(t *testing.T) {
	tmp := t.TempDir()
	initVisitDB(tmp) // fresh DB → human=0, bot=0
	t.Cleanup(func() { visitDB.Close() })

	recordVisit(false)
	recordVisit(false)
	recordVisit(true)

	// recordVisit writes on a goroutine; poll the atomics briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&humanCount) == 2 && atomic.LoadInt64(&botCount) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if h := atomic.LoadInt64(&humanCount); h != 2 {
		t.Fatalf("humanCount = %d, want 2", h)
	}
	if b := atomic.LoadInt64(&botCount); b != 1 {
		t.Fatalf("botCount = %d, want 1", b)
	}
	var human, bot int64
	visitDB.QueryRow(`SELECT human, bot FROM visit_count WHERE id = 1`).Scan(&human, &bot)
	if human != 2 || bot != 1 {
		t.Fatalf("db human=%d bot=%d, want 2/1", human, bot)
	}
}
