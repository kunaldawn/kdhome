# Human / Bot Visit Counter Split — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the inflated site visit counter into separate human and bot tallies, classified server-side by User-Agent, and surface it in the tray as `248 human · 1.2k bot`.

**Architecture:** A deny-list regexp (`isBot`) classifies each GET-to-`/` as human or bot at the existing `recordVisit` call site. Two new columns (`human`, `bot`) on the existing single-row `visit_count` table replace the retired `n` total; a one-time `PRAGMA user_version`-gated migration zeroes everything on first deploy. `/api/visit` returns `{"humans":H,"bots":B}`; the frontend renders both and drives the cat's milestone celebration off the human count.

**Tech Stack:** Go (`net/http`, `database/sql` + `mattn/go-sqlite3`, `regexp`), table-driven Go tests (`go test`, CGO enabled), and a single static `static/index.html` (vanilla JS) verified with curl + Playwright.

**Spec:** `docs/superpowers/specs/2026-05-26-human-bot-visit-counter-design.md`

**Run everything from the repo root** (`/home/kunaldawn/workspace/repos/kdhome`) — the server hardcodes `staticDir = "./static"`.

---

## File Structure

- **`main.go`** (modify) — owns counting. Adds `botUA`/`isBot`, the `human`/`bot` columns + reset migration + atomics, `recordVisit(bool)`, and the `/api/visit` JSON shape.
- **`main_test.go`** (create) — Go tests for `isBot`, the migration/hydration, `recordVisit` routing, and `visitHandler` output.
- **`static/index.html`** (modify) — owns display. The `#visit-count` tray span, its fetch IIFE (+ `abbrev` helper), the `kd-visit-count` event payload, and the cat's `onVisitCount` listener + milestone labels.

---

## Task 1: `isBot` User-Agent classifier

**Files:**
- Create: `main_test.go`
- Modify: `main.go` (add `"regexp"` import; add `botUA` + `isBot` just above `recordVisit`)

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:

```go
package main

import "testing"

func TestIsBot(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want bool
	}{
		{"chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36", false},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0", false},
		{"ios_safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1", false},
		{"gptbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.1; +https://openai.com/gptbot", true},
		{"claudebot", "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", true},
		{"ahrefsbot", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", true},
		{"uptimerobot", "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)", true},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true},
		{"bytespider", "Mozilla/5.0 (Linux; Android 5.0) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; Bytespider; spider-feedback@bytedance.com)", true},
		{"curl", "curl/8.7.1", true},
		{"python", "python-requests/2.31.0", true},
		{"gohttp", "Go-http-client/2.0", true},
		{"empty", "", true},
		{"whitespace", "   ", true},
	}
	for _, c := range cases {
		if got := isBot(c.ua); got != c.want {
			t.Errorf("%s: isBot(%q) = %v, want %v", c.name, c.ua, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestIsBot`
Expected: build failure — `./main_test.go:...: undefined: isBot` (FAIL).

- [ ] **Step 3: Add the implementation**

In `main.go`, add `"regexp"` to the import block (alphabetically, right after `"path/filepath"`). Then add this block immediately above `func recordVisit(` (which is currently around line 177):

```go
// botUA matches self-declaring crawlers, monitors, HTTP libraries, and
// link-preview fetchers. It's a deny-list: real browser User-Agents carry none
// of these tokens and fall through to "human". Grouped only for maintenance —
// order within the alternation is irrelevant. All tokens are literal (no regexp
// metacharacters), so none need escaping.
var botUA = regexp.MustCompile(`(?i)` + strings.Join([]string{
	// generic
	"bot", "crawl", "spider", "slurp", "scrape",
	// AI / research
	"gptbot", "oai-searchbot", "chatgpt", "claudebot", "anthropic-ai",
	"ccbot", "perplexitybot", "google-extended", "bytespider", "amazonbot",
	"applebot", "diffbot", "cohere-ai", "imagesiftbot", "omgili",
	// search
	"googlebot", "bingbot", "duckduckbot", "baiduspider", "yandexbot",
	// SEO
	"ahrefsbot", "semrushbot", "mj12bot", "dotbot", "dataforseo",
	// monitors
	"uptimerobot", "pingdom", "statuscake", "site24x7",
	// HTTP libs / headless
	"curl", "wget", "libwww", "python-requests", "go-http-client", "okhttp",
	"java/", "node-fetch", "axios", "headlesschrome", "phantomjs",
	// link preview
	"facebookexternalhit", "twitterbot", "slackbot", "discordbot",
	"telegrambot", "whatsapp", "linkedinbot", "embedly", "redditbot",
}, "|"))

// isBot reports whether a request's User-Agent looks automated. An empty or
// whitespace-only UA counts as a bot (scripts that send no UA).
func isBot(ua string) bool {
	if strings.TrimSpace(ua) == "" {
		return true
	}
	return botUA.MatchString(ua)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestIsBot -v`
Expected: `--- PASS: TestIsBot` / `ok` (PASS).

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: add isBot User-Agent classifier

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Backend — columns, reset migration, atomics, recording, API

This task is one atomic change because all of it references the same package globals (renaming `visitCount` while leaving `recordVisit`/`visitHandler` on the old name would not compile). Write all tests first, watch the build fail, then make every edit, then go green.

**Files:**
- Modify: `main.go` (globals; `initVisitDB` columns + migration + hydration; remove dead legacy-`visits` block; `recordVisit`; `staticHandler` call site; `visitHandler`)
- Modify: `main_test.go` (expand imports; add three tests)

- [ ] **Step 1: Write the failing tests**

Replace the import line at the top of `main_test.go` (`import "testing"`) with:

```go
import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)
```

Then append these three tests to `main_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./...`
Expected: build failure — `undefined: humanCount`, `undefined: botCount`, and `too many arguments in call to recordVisit` (FAIL).

- [ ] **Step 3a: Swap the package globals**

In `main.go`, replace:

```go
var (
	visitDB    *sql.DB
	visitCount int64 // atomic, cached in memory
)
```

with:

```go
var (
	visitDB    *sql.DB
	humanCount int64 // atomic, cached in memory
	botCount   int64 // atomic, cached in memory
)
```

- [ ] **Step 3b: Add columns + reset migration + hydration in `initVisitDB`**

In `initVisitDB`, replace this region — from the `INSERT OR IGNORE INTO visit_count` block through the old legacy-`visits` migration and the `visitCount` hydration (currently the block that ends with `atomic.StoreInt64(&visitCount, count)`):

```go
	if _, err = visitDB.Exec(`INSERT OR IGNORE INTO visit_count (id, n) VALUES (1, 0)`); err != nil {
		log.Printf("[VISITS] failed to seed visit_count: %v", err)
		return
	}

	// One-shot migration: if the legacy per-row `visits` table exists and the
	// aggregate is still 0, copy the count over so we don't reset to zero on
	// upgrade. Leaves the old table in place for archival; nothing else
	// references it.
	var legacy int
	visitDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='visits'`).Scan(&legacy)
	if legacy > 0 {
		var aggregate, legacyCount int64
		visitDB.QueryRow(`SELECT n FROM visit_count WHERE id = 1`).Scan(&aggregate)
		visitDB.QueryRow(`SELECT COUNT(*) FROM visits`).Scan(&legacyCount)
		if aggregate == 0 && legacyCount > 0 {
			if _, err := visitDB.Exec(`UPDATE visit_count SET n = ? WHERE id = 1`, legacyCount); err != nil {
				log.Printf("[VISITS] migration update failed: %v", err)
			} else {
				log.Printf("[VISITS] migrated %d rows from legacy table to aggregate", legacyCount)
			}
		}
	}

	var count int64
	visitDB.QueryRow(`SELECT n FROM visit_count WHERE id = 1`).Scan(&count)
	atomic.StoreInt64(&visitCount, count)
```

with:

```go
	if _, err = visitDB.Exec(`INSERT OR IGNORE INTO visit_count (id, n) VALUES (1, 0)`); err != nil {
		log.Printf("[VISITS] failed to seed visit_count: %v", err)
		return
	}

	// Add the human/bot split columns. ADD COLUMN errors on the second boot
	// ("duplicate column name"); that's benign, so run unconditionally and
	// ignore the error. The old `n` column is retired — no longer read/written.
	visitDB.Exec(`ALTER TABLE visit_count ADD COLUMN human INTEGER NOT NULL DEFAULT 0`)
	visitDB.Exec(`ALTER TABLE visit_count ADD COLUMN bot   INTEGER NOT NULL DEFAULT 0`)

	// One-time reset, gated by PRAGMA user_version so restarts never re-zero
	// accumulating counts. The pre-existing total can't be split (no UA was
	// ever stored), so the human/bot tally starts clean at the 0 → 1 bump.
	var userVersion int
	visitDB.QueryRow(`PRAGMA user_version`).Scan(&userVersion)
	if userVersion < 1 {
		if _, err := visitDB.Exec(`UPDATE visit_count SET n = 0, human = 0, bot = 0 WHERE id = 1`); err != nil {
			log.Printf("[VISITS] reset migration failed: %v", err)
		} else {
			visitDB.Exec(`PRAGMA user_version = 1`)
			log.Printf("[VISITS] reset visit counters to 0 (human/bot split migration)")
		}
	}

	var human, bot int64
	visitDB.QueryRow(`SELECT human, bot FROM visit_count WHERE id = 1`).Scan(&human, &bot)
	atomic.StoreInt64(&humanCount, human)
	atomic.StoreInt64(&botCount, bot)
```

Then update the init log line near the end of `initVisitDB`, replacing:

```go
	log.Printf("[VISITS] initialized — total: %d", count)
```

with:

```go
	log.Printf("[VISITS] initialized — humans: %d, bots: %d", human, bot)
```

- [ ] **Step 3c: Route `recordVisit` by classification**

Replace the whole `recordVisit` function:

```go
func recordVisit() {
	if visitDB == nil {
		return
	}
	go func() {
		_, err := visitDB.Exec(`UPDATE visit_count SET n = n + 1 WHERE id = 1`)
		if err != nil {
			log.Printf("[VISITS] update error: %v", err)
			return
		}
		atomic.AddInt64(&visitCount, 1)
	}()
}
```

with:

```go
func recordVisit(bot bool) {
	if visitDB == nil {
		return
	}
	col, ctr := "human", &humanCount
	if bot {
		col, ctr = "bot", &botCount
	}
	go func() {
		// col is from a fixed two-value set (never user input), so building the
		// SQL by concatenation is safe here.
		if _, err := visitDB.Exec(`UPDATE visit_count SET ` + col + ` = ` + col + ` + 1 WHERE id = 1`); err != nil {
			log.Printf("[VISITS] update error (%s): %v", col, err)
			return
		}
		atomic.AddInt64(ctr, 1)
	}()
}
```

- [ ] **Step 3d: Classify at the call site in `staticHandler`**

Replace:

```go
		if r.Method == http.MethodGet &&
			(r.URL.Path == "/" || r.URL.Path == "/index.html") {
			recordVisit()
		}
```

with:

```go
		if r.Method == http.MethodGet &&
			(r.URL.Path == "/" || r.URL.Path == "/index.html") {
			recordVisit(isBot(r.UserAgent()))
		}
```

- [ ] **Step 3e: Update the `/api/visit` JSON shape**

In `visitHandler`, replace:

```go
	fmt.Fprintf(w, `{"count":%d}`, atomic.LoadInt64(&visitCount))
```

with:

```go
	fmt.Fprintf(w, `{"humans":%d,"bots":%d}`,
		atomic.LoadInt64(&humanCount), atomic.LoadInt64(&botCount))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: `PASS` for `TestIsBot`, `TestVisitHandlerJSON`, `TestInitVisitDBResetAndHydrate`, `TestRecordVisitRoutesToBucket` (`ok`).

Also confirm the binary builds: `go build -o /tmp/kdhome-verify/kdhome-bin .` → no output, exit 0.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: split visit counter into human/bot buckets server-side

Add human/bot columns to visit_count with a one-time user_version-gated
reset, classify each GET to / via isBot(UA), and return {humans,bots}
from /api/visit. Retires the old monotonic total.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Frontend — tray display, event payload, cat milestones

No JS test harness exists in this repo; this task is verified by build + curl + Playwright in Task 4. Make the four edits below exactly.

**Files:**
- Modify: `static/index.html` (tray span ~line 7247; fetch IIFE ~lines 7744-7756; cat event listener ~line 8041; milestone labels ~lines 7229-7236 within `onVisitCount`)

- [ ] **Step 1: Update the tray span placeholder + title**

Replace:

```html
        <span id="visit-count" style="color: #7fd1b3; font-family: 'Share Tech Mono', monospace; font-size: 11px;" title="Total site visits">-- visits</span>
```

with:

```html
        <span id="visit-count" style="color: #7fd1b3; font-family: 'Share Tech Mono', monospace; font-size: 11px;" title="Humans · filtered bots">-- · --</span>
```

- [ ] **Step 2: Update the fetch IIFE (render both buckets + abbreviate + dispatch)**

Replace:

```javascript
        (function () {
          const el = document.getElementById('visit-count');
          fetch('/api/visit')
            .then(r => r.ok ? r.json() : null)
            .then(data => {
              if (el && data && data.count != null) {
                el.textContent = data.count.toLocaleString() + ' visits';
                // Let the cat react to milestone crossings.
                window.dispatchEvent(new CustomEvent('kd-visit-count', { detail: { count: data.count } }));
              }
            })
            .catch(() => { /* silent: no counter is fine */ });
        })();
```

with:

```javascript
        (function () {
          const el = document.getElementById('visit-count');
          // Compact form for the cramped tray slot: 248 → "248", 1203 → "1.2k".
          const abbrev = (n) => {
            if (n < 1000) return n.toLocaleString();
            if (n < 1e6) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'k';
            return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M';
          };
          fetch('/api/visit')
            .then(r => r.ok ? r.json() : null)
            .then(data => {
              if (el && data && data.humans != null && data.bots != null) {
                el.textContent = abbrev(data.humans) + ' human · ' + abbrev(data.bots) + ' bot';
                el.title = data.humans.toLocaleString() + ' humans · ' +
                           data.bots.toLocaleString() + ' bots filtered';
                // Let the cat react to milestone crossings (human count only).
                window.dispatchEvent(new CustomEvent('kd-visit-count', {
                  detail: { humans: data.humans, bots: data.bots }
                }));
              }
            })
            .catch(() => { /* silent: no counter is fine */ });
        })();
```

- [ ] **Step 3: Point the cat's listener at the human count**

Replace:

```javascript
            window.addEventListener('kd-visit-count', (e) => this.onVisitCount(e.detail.count));
```

with:

```javascript
            window.addEventListener('kd-visit-count', (e) => this.onVisitCount(e.detail.humans));
```

- [ ] **Step 4: Reword milestone labels to "visitors"**

Replace the `labels` map inside `onVisitCount`:

```javascript
            const labels = {
              100:    '100 visits! Mrow!',
              500:    '500 visits!',
              1000:   '1k visits! \u{1F389}',
              5000:   '5k visits!',
              10000:  '10k visits!',
              100000: '100k visits! \u{1F389}',
            };
```

with:

```javascript
            const labels = {
              100:    '100 visitors! Mrow!',
              500:    '500 visitors!',
              1000:   '1k visitors! \u{1F389}',
              5000:   '5k visitors!',
              10000:  '10k visitors!',
              100000: '100k visitors! \u{1F389}',
            };
```

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "feat: show human/bot visit split in tray, cat reacts to humans

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: End-to-end verification + cleanup

No code changes (unless verification surfaces a bug). Confirms the whole feature against a running server, then cleans up temp artifacts. Leaves the repo tree clean.

**Files:** none (verification only)

- [ ] **Step 1: Build to /tmp and start the server**

```bash
go build -o /tmp/kdhome-verify/kdhome-bin . && \
  PORT=8099 DATA_DIR=/tmp/kdhome-verify /tmp/kdhome-verify/kdhome-bin
```

Run this in the background. Expected log lines include `[VISITS] reset visit counters to 0 (human/bot split migration)` (fresh DATA_DIR) and `[SERVER] serving ./static on :8099`.

- [ ] **Step 2: Drive the UA matrix and assert the buckets**

In a second shell, hit `/` with 3 human and 6 bot User-Agents, then read the API:

```bash
H_UAS=(
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
  "Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0"
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1"
)
B_UAS=(
  "Mozilla/5.0 (compatible; GPTBot/1.1; +https://openai.com/gptbot)"
  "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"
  "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)"
  "Mozilla/5.0 (compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)"
  "curl/8.7.1"
  ""
)
for ua in "${H_UAS[@]}"; do curl -s -o /dev/null -A "$ua" http://localhost:8099/; done
for ua in "${B_UAS[@]}"; do curl -s -o /dev/null -A "$ua" http://localhost:8099/; done
sleep 0.5  # recordVisit writes on a goroutine
curl -s http://localhost:8099/api/visit
```

Expected output: `{"humans":3,"bots":6}`

- [ ] **Step 3: Verify the tray rendering with Playwright**

Navigate to `http://localhost:8099/`, dismiss the entry gate if present, then read the tray element:

```js
// browser_evaluate
() => {
  const el = document.getElementById('visit-count');
  return { text: el.textContent, title: el.title };
}
```

Expected: `text` is `"3 human · 6 bot"` and `title` is `"3 humans · 6 bots filtered"`. (If a bot count ≥ 1000 were present it would render like `1.2k bot`.)

- [ ] **Step 4: Stop the server and clean up**

Stop via the port's PID (do NOT `pkill -f kdhome` — it matches its own cwd):

```bash
PID=$(ss -ltnp 2>/dev/null | grep ':8099' | grep -o 'pid=[0-9]*' | cut -d= -f2 | head -1)
[ -n "$PID" ] && kill "$PID"
rm -rf /tmp/kdhome-verify
```

Confirm the repo tree has no stray build/db artifacts: `git status --short` should show only the intended committed changes already in history (no untracked `kdhome` binary, no `/tmp` leakage).

---

## Self-Review

**Spec coverage:**
- Detection (`isBot` deny-list, empty-UA → bot) → Task 1. ✓
- `human`/`bot` columns, `n` retired, atomics → Task 2 (Steps 3a, 3b). ✓
- One-time `user_version`-gated reset → Task 2 (Step 3b) + `TestInitVisitDBResetAndHydrate`. ✓
- `recordVisit(isBot(UA))` at the GET-`/` call site, includes 304, skips HEAD → Task 2 (Steps 3c, 3d); the `HEAD`/304 trigger is the unchanged surrounding code. ✓
- `/api/visit` → `{"humans":H,"bots":B}` → Task 2 (Step 3e) + `TestVisitHandlerJSON`. ✓
- Tray `N human · N bot` + `abbrev` + full-number tooltip → Task 3 (Steps 1, 2). ✓
- Event payload `{humans,bots}`; cat milestones key off humans; labels reworded → Task 3 (Steps 2, 3, 4). ✓
- Verification (build, UA matrix curl, reset/idempotency, Playwright tray) → Task 4 + the Go migration test. ✓
- Non-goals (no JS beacon, no per-crawler breakdown, no dedup, irreversible reset) → respected; nothing in the plan adds them. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every command has an expected result. ✓

**Type/name consistency:** `humanCount`/`botCount` (globals), `recordVisit(bot bool)`, `isBot(ua string)`, `botUA`, JSON keys `humans`/`bots`, event `detail.humans`/`detail.bots`, and the `abbrev` helper are used identically across Tasks 1–4. ✓
