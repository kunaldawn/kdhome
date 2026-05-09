# Server-side Counters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move visit counting off the client onto the server's index handler, and add a per-archive click counter (with `/go/<id>` tracked redirect, in-memory + SQLite storage, and a count badge on each Featured Archive card).

**Architecture:** All Go work in `main.go` (one extra `archive_click_count` table, one new registry slice, two new handlers, increment moved into `staticHandler`). All client work in `static/index.html` (visit fetch verb change, card href changes, `<h4>` layout + `.card-count` CSS, bootstrap fetch IIFE, `openFeaturedArchive` redirect URL + optimistic update). The Go server pre-builds static cache at startup, so each iteration is `go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &` then verify.

**Tech Stack:** Go 1.22, `github.com/mattn/go-sqlite3` (already a dep, CGO-based, WAL mode), vanilla JS in HTML, Playwright MCP for UI verification.

**Spec:** `docs/superpowers/specs/2026-05-09-server-side-counters-design.md`

**Conventions used in this plan:**

- Source line numbers reference the file *before* this plan starts. They drift as edits land. Use the surrounding code in `old_string` snippets to locate the right place — that anchor is authoritative.
- "Restart server" step: `pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &` then `sleep 1`. We use `/tmp/kdhome-data` (not `./data`) so iterating doesn't pollute the real visit DB committed in the repo. Wipe between tasks with `rm -rf /tmp/kdhome-data` if you want a clean slate.
- "Verify" steps assume the server is up on `http://localhost:8089/`.
- Each task ends with one commit. Commit-message style follows the repo: `feat(visits): ...`, `feat(clicks): ...`, `refactor(visits): ...`. Trailer: `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` per repo history.
- No Go unit-test files are added. The codebase has none today; verification is black-box via `curl` + Playwright, matching the pattern of the prior browser/cat/milkdrop plans.

---

### Task 1: Add archive registry + `archive_click_count` table init

**Files:**
- Modify: `main.go` (top-level vars near `visitCount`, function `initVisitDB`)

**Why first:** Storage and lookup table must exist before any handler can read or write them. No behaviour change yet — pure scaffolding.

- [ ] **Step 1: Add the registry and click-count cache**

Anchor: search for `visitCount int64 // atomic, cached in memory` (around line 29).

Replace this block:

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
	visitCount int64 // atomic, cached in memory
)

// archives mirrors the JS ARCHIVES registry in static/index.html.
// Both sides need the seven IDs and URLs; with seven entries that change
// rarely, mirroring beats a JSON file shared by both runtimes.
var archives = []struct {
	ID, URL string
}{
	{"wiki", "https://wiki.kunaldawn.com"},
	{"pdf", "https://pdf.kunaldawn.com"},
	{"os", "https://os.kunaldawn.com"},
	{"iso", "https://iso.kunaldawn.com"},
	{"chiptune", "https://chiptune.kunaldawn.com"},
	{"tube", "https://tube.kunaldawn.com"},
	{"audio", "https://audio.kunaldawn.com"},
}

// archiveURL is built from `archives` at startup for O(1) /go/<id> lookup.
var archiveURL = map[string]string{}

// archiveClicks holds the in-memory aggregate, mirrored to SQLite. Keyed by
// archive ID. Guarded by archiveClicksMu.
var (
	archiveClicksMu sync.RWMutex
	archiveClicks   = map[string]int64{}
)
```

- [ ] **Step 2: Add the `sync` import**

Anchor: the import block at the top of `main.go` (line 3-23). It already has `"sync/atomic"` but not `"sync"`.

Replace:

```go
	"sync/atomic"
```

with:

```go
	"sync"
	"sync/atomic"
```

- [ ] **Step 3: Build the URL map and initialize the click table inside `initVisitDB`**

Anchor: search for `log.Printf("[VISITS] initialized — total: %d", count)` (around line 83) — the last line of `initVisitDB`.

Insert *before* that `log.Printf` line, right after the `atomic.StoreInt64(&visitCount, count)`:

```go
	// ─── Archive click counts ───
	for _, a := range archives {
		archiveURL[a.ID] = a.URL
	}

	_, err = visitDB.Exec(`CREATE TABLE IF NOT EXISTS archive_click_count (
		id TEXT PRIMARY KEY,
		n  INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		log.Printf("[CLICKS] failed to create archive_click_count: %v", err)
		return
	}

	// Seed the seven known IDs so /api/archive-clicks always returns a full
	// shape and never misses a row.
	for _, a := range archives {
		if _, err := visitDB.Exec(
			`INSERT OR IGNORE INTO archive_click_count (id, n) VALUES (?, 0)`, a.ID,
		); err != nil {
			log.Printf("[CLICKS] failed to seed %s: %v", a.ID, err)
		}
	}

	// Hydrate the in-memory cache from disk.
	rows, err := visitDB.Query(`SELECT id, n FROM archive_click_count`)
	if err != nil {
		log.Printf("[CLICKS] failed to hydrate cache: %v", err)
	} else {
		archiveClicksMu.Lock()
		for rows.Next() {
			var id string
			var n int64
			if err := rows.Scan(&id, &n); err == nil {
				archiveClicks[id] = n
			}
		}
		archiveClicksMu.Unlock()
		rows.Close()
		log.Printf("[CLICKS] hydrated %d archive(s)", len(archiveClicks))
	}

```

- [ ] **Step 4: Build and start the server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
rm -rf /tmp/kdhome-data
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

Expected log lines:

```
[VISITS] initialized — total: 0
[CLICKS] hydrated 7 archive(s)
[STATIC] cached N files
[SERVER] serving ./static on :8089
```

If any line is missing or shows a different count, fix before moving on.

- [ ] **Step 5: Verify the table is real**

```bash
sqlite3 /tmp/kdhome-data/visits.db "SELECT id, n FROM archive_click_count ORDER BY id;"
```

Expected (in any line order, all `n=0`):

```
audio|0
chiptune|0
iso|0
os|0
pdf|0
tube|0
wiki|0
```

- [ ] **Step 6: Commit**

```bash
git add main.go
git commit -m "$(cat <<'EOF'
feat(clicks): scaffold archive registry + click_count table

Adds the Go-side mirror of the JS archive registry, a sync.RWMutex-guarded
in-memory click cache, and the SQLite table seeded with the seven known
archive IDs. No handlers wired yet; this is storage-only.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Add `/go/<id>` tracked redirect handler

**Files:**
- Modify: `main.go` (new function `archiveGoHandler` next to `visitHandler`; wire in `main`)

- [ ] **Step 1: Add the handler**

Anchor: search for `func visitHandler(w http.ResponseWriter, r *http.Request) {` (around line 100). Insert *before* it (so the click code stays grouped above the visit handler):

```go
// ─── Archive Click Counter ───

func recordArchiveClick(id string) {
	if visitDB == nil {
		return
	}
	go func() {
		_, err := visitDB.Exec(`UPDATE archive_click_count SET n = n + 1 WHERE id = ?`, id)
		if err != nil {
			log.Printf("[CLICKS] update error for %s: %v", id, err)
			return
		}
		archiveClicksMu.Lock()
		archiveClicks[id]++
		archiveClicksMu.Unlock()
	}()
}

// archiveGoHandler increments the click count for an archive id and
// 302-redirects to the archive's URL. Path: /go/<id>. Returns 404 on
// unknown id so links never silently swallow a typo.
func archiveGoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/go/")
	if id == "" || strings.ContainsRune(id, '/') {
		http.NotFound(w, r)
		return
	}
	url, ok := archiveURL[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	recordArchiveClick(id)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

// archiveClicksHandler returns {"counts": {id: n, ...}} for all known
// archive IDs. Read-only; no caching.
func archiveClicksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	archiveClicksMu.RLock()
	counts := make(map[string]int64, len(archiveClicks))
	for id, n := range archiveClicks {
		counts[id] = n
	}
	archiveClicksMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"counts": counts})
}

```

- [ ] **Step 2: Wire both handlers into `main`**

Anchor: search for `mux.HandleFunc("/api/visit", visitHandler)` (around line 444).

Replace:

```go
	mux.HandleFunc("/api/visit", visitHandler)
	mux.HandleFunc("/api/playlist", playlistHandler(staticDir))
```

with:

```go
	mux.HandleFunc("/api/visit", visitHandler)
	mux.HandleFunc("/api/archive-clicks", archiveClicksHandler)
	mux.HandleFunc("/api/playlist", playlistHandler(staticDir))
	mux.HandleFunc("/go/", archiveGoHandler)
```

- [ ] **Step 3: Restart and verify the redirect**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
curl -sI http://localhost:8089/go/wiki | head -3
```

Expected:

```
HTTP/1.1 302 Found
Location: https://wiki.kunaldawn.com
Cache-Control: no-store
```

- [ ] **Step 4: Verify increment landed in DB**

The increment is async; give the goroutine a moment, then check:

```bash
sleep 0.3
sqlite3 /tmp/kdhome-data/visits.db "SELECT id, n FROM archive_click_count WHERE id='wiki';"
```

Expected: `wiki|1`.

Hit it twice more, then:

```bash
curl -so /dev/null http://localhost:8089/go/wiki
curl -so /dev/null http://localhost:8089/go/wiki
sleep 0.3
sqlite3 /tmp/kdhome-data/visits.db "SELECT id, n FROM archive_click_count WHERE id='wiki';"
```

Expected: `wiki|3`.

- [ ] **Step 5: Verify unknown id returns 404**

```bash
curl -sI http://localhost:8089/go/nonexistent | head -1
curl -sI http://localhost:8089/go/ | head -1
```

Expected: both `HTTP/1.1 404 Not Found`.

- [ ] **Step 6: Verify the JSON endpoint**

```bash
curl -s http://localhost:8089/api/archive-clicks
```

Expected (key order may vary):

```json
{"counts":{"audio":0,"chiptune":0,"iso":0,"os":0,"pdf":0,"tube":0,"wiki":3}}
```

- [ ] **Step 7: Commit**

```bash
git add main.go
git commit -m "$(cat <<'EOF'
feat(clicks): /go/<id> tracked redirect + /api/archive-clicks JSON

GET /go/<id> increments the per-archive count async and 302-redirects to
the archive URL. GET /api/archive-clicks returns the full {id: n} map.
404 for unknown ids — typos shouldn't silently fall through.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Move visit-count increment server-side; drop POST /api/visit

**Files:**
- Modify: `main.go` (function `visitHandler`, function `staticHandler`)

- [ ] **Step 1: Drop the POST branch from `visitHandler`**

Anchor: search for `func visitHandler(w http.ResponseWriter, r *http.Request) {` (around line 100).

Replace this whole function:

```go
func visitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		recordVisit()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `{"count":%d}`, atomic.LoadInt64(&visitCount))
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `{"count":%d}`, atomic.LoadInt64(&visitCount))
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
```

with:

```go
func visitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `{"count":%d}`, atomic.LoadInt64(&visitCount))
}
```

- [ ] **Step 2: Increment on every GET to / or /index.html in `staticHandler`**

Anchor: search for the start of `staticHandler` body — the line `if r.Method != http.MethodGet && r.Method != http.MethodHead {` (around line 361).

Replace this block:

```go
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fallback.ServeHTTP(w, r)
			return
		}
		cf, ok := staticCache[r.URL.Path]
```

with:

```go
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fallback.ServeHTTP(w, r)
			return
		}

		// Server-side visit counter. Counts every GET to the index page
		// (including conditional 304 responses below) so cache, bfcache,
		// no-JS clients, and bots are all reflected. Skips HEAD on purpose
		// — HEAD is metadata-only, not a real page view.
		if r.Method == http.MethodGet &&
			(r.URL.Path == "/" || r.URL.Path == "/index.html") {
			recordVisit()
		}

		cf, ok := staticCache[r.URL.Path]
```

- [ ] **Step 3: Restart**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
rm -rf /tmp/kdhome-data
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 4: Verify GET / increments**

```bash
curl -s http://localhost:8089/api/visit
curl -so /dev/null http://localhost:8089/
sleep 0.3
curl -s http://localhost:8089/api/visit
```

Expected: first prints `{"count":0}`, second prints `{"count":1}`.

- [ ] **Step 5: Verify /index.html also increments**

```bash
curl -so /dev/null http://localhost:8089/index.html
sleep 0.3
curl -s http://localhost:8089/api/visit
```

Expected: `{"count":2}`.

- [ ] **Step 6: Verify 304 conditional GET also counts**

Get the ETag, then re-fetch with `If-None-Match`:

```bash
ETAG=$(curl -sI http://localhost:8089/ | awk -F': ' '/^ETag/{print $2}' | tr -d '\r')
curl -sI -H "If-None-Match: $ETAG" http://localhost:8089/ | head -1
sleep 0.3
curl -s http://localhost:8089/api/visit
```

Expected: response status `HTTP/1.1 304 Not Modified`, count rises to `{"count":3}`.

- [ ] **Step 7: Verify HEAD does NOT increment**

```bash
curl -sI http://localhost:8089/ -X HEAD > /dev/null
sleep 0.3
curl -s http://localhost:8089/api/visit
```

Expected: still `{"count":3}`.

- [ ] **Step 8: Verify POST /api/visit is now rejected**

```bash
curl -sI -X POST http://localhost:8089/api/visit | head -1
```

Expected: `HTTP/1.1 405 Method Not Allowed`.

- [ ] **Step 9: Commit**

```bash
git add main.go
git commit -m "$(cat <<'EOF'
refactor(visits): increment server-side on GET / and drop POST /api/visit

The client-side POST under-counted visits on every cache hit, bfcache
restore, no-JS load, and bot/prefetch. Increment is now in staticHandler
before the conditional-GET branch so 304 responses also count.

GET /api/visit stays as the read-only display source for the system tray.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Allow same-origin iframe in CSP

**Files:**
- Modify: `main.go` (function `securityHeaders`, the `frame-src` directive)

**Why:** The internal browser iframe will load `/go/<id>` (same-origin) and follow a 302 to the archive. `frame-src` currently lists only the seven archive subdomains, so a same-origin iframe would be blocked by CSP. Add `'self'`.

- [ ] **Step 1: Add `'self'` to `frame-src`**

Anchor: search for `"frame-src https://wiki.kunaldawn.com` (around line 134).

Replace:

```go
				"frame-src https://wiki.kunaldawn.com https://pdf.kunaldawn.com https://os.kunaldawn.com https://iso.kunaldawn.com https://chiptune.kunaldawn.com https://tube.kunaldawn.com https://audio.kunaldawn.com; "+
```

with:

```go
				"frame-src 'self' https://wiki.kunaldawn.com https://pdf.kunaldawn.com https://os.kunaldawn.com https://iso.kunaldawn.com https://chiptune.kunaldawn.com https://tube.kunaldawn.com https://audio.kunaldawn.com; "+
```

- [ ] **Step 2: Restart and verify the header**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
curl -sI http://localhost:8089/ | grep -i content-security-policy | tr ';' '\n' | grep frame-src
```

Expected: line containing `frame-src 'self' https://wiki.kunaldawn.com ...`.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "$(cat <<'EOF'
fix(csp): allow 'self' in frame-src so internal browser can load /go/<id>

Same-origin iframe load is needed for the tracked-redirect path inside
the internal archive browser; the iframe loads /go/<id> and follows a
302 to the archive subdomain. Minor widening — nothing else loads
same-origin iframes today.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Client visit fetch — POST → GET

**Files:**
- Modify: `static/index.html` (visit-counter IIFE around line 5972-5988)

- [ ] **Step 1: Switch the verb and refresh the comment**

Anchor: search for `// Visit counter. POST records the visit and returns the new count;` (around line 5972).

Replace this block:

```js
        // Visit counter. POST records the visit and returns the new count;
        // we don't bother with a GET-only fallback because both endpoints
        // share the same in-memory atomic and a transient POST failure
        // just means "skip the counter for this visit".
        (function () {
          const el = document.getElementById('visit-count');
          fetch('/api/visit', { method: 'POST' })
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

```js
        // Visit counter. The increment happens server-side when staticHandler
        // serves the index page, so this fetch is read-only display. A
        // transient failure just means "no counter shown this load".
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

- [ ] **Step 2: Restart and verify in browser via Playwright**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
rm -rf /tmp/kdhome-data
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

Use Playwright MCP:

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => document.getElementById('visit-count')?.textContent
```

Expected: text like `"1 visits"` (one because that very navigation is the first count). Reload once and re-evaluate; expect `"2 visits"`.

Also confirm in DevTools (or `browser_network_requests`) that the request to `/api/visit` is a `GET`, not a `POST`.

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(visits): client fetches /api/visit as GET-only display

The POST path was deleted server-side in the prior commit. The client
no longer drives the increment — it just reads the current count for
the tray badge.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Card layout + `.card-count` CSS + count placeholder span

**Files:**
- Modify: `static/index.html` (CSS — find a sensible spot near the `.card` rules; the seven `<a class="card">` blocks at lines 5558-5591)

**Why before the bootstrap fetch:** the JS in Task 8 selects `.card-count[data-archive-id]`, so the DOM hooks must exist first. Doing CSS + HTML first means a clean-build state where the page renders with a `—` placeholder badge; then Task 8 replaces it with real numbers.

- [ ] **Step 1: Add the CSS rules**

Anchor: search for `.card h4` in the CSS — likely a one-line selector around line 1700-1900. If you can't find an exact match, add a fresh block immediately *before* the closing `</style>` of the main stylesheet (search for `</style>` and pick the first match after `.grid-cards`). Either way, insert this block:

```css
    /* Featured Archive card header — flex row so the click-count badge
       hugs the right edge while the title takes the remaining width. */
    .card h4 {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .card h4 .card-title {
      flex: 1;
      min-width: 0; /* allow the title to shrink before the badge */
    }
    .card-count {
      color: #7fd1b3;
      font-family: 'Share Tech Mono', monospace;
      font-size: 11px;
      white-space: nowrap;
      opacity: 0.85;
    }
```

- [ ] **Step 2: Wrap each card title and add the badge span**

For each of the seven cards in `static/index.html` (line range 5558-5591), wrap the bare archive name in `<span class="card-title">…</span>` and append a `<span class="card-count" data-archive-id="…">—</span>`. Do this once per card; the structure is the same for all seven.

For the **wiki** card (around line 5559), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><ellipse cx="12" cy="12" rx="4" ry="10"/><line x1="2" y1="12" x2="22" y2="12"/></svg></span>Wiki Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><ellipse cx="12" cy="12" rx="4" ry="10"/><line x1="2" y1="12" x2="22" y2="12"/></svg></span><span class="card-title">Wiki Archive</span><span class="card-count" data-archive-id="wiki">—</span></h4>
```

For the **pdf** card (around line 5564), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/><path d="M8 7h8M8 11h5" stroke="rgba(0,0,0,0.3)" stroke-width="1.5" fill="none"/></svg></span>PDF Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/><path d="M8 7h8M8 11h5" stroke="rgba(0,0,0,0.3)" stroke-width="1.5" fill="none"/></svg></span><span class="card-title">PDF Archive</span><span class="card-count" data-archive-id="pdf">—</span></h4>
```

For the **os** card (around line 5569), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/><path d="M7 9l2 2-2 2"/><line x1="11" y1="13" x2="15" y2="13"/></svg></span>OS Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/><path d="M7 9l2 2-2 2"/><line x1="11" y1="13" x2="15" y2="13"/></svg></span><span class="card-title">OS Archive</span><span class="card-count" data-archive-id="os">—</span></h4>
```

For the **iso** card (around line 5574), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></span>CD/DVD Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></span><span class="card-title">CD/DVD Archive</span><span class="card-count" data-archive-id="iso">—</span></h4>
```

For the **chiptune** card (around line 5579), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg></span>Chiptune Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg></span><span class="card-title">Chiptune Archive</span><span class="card-count" data-archive-id="chiptune">—</span></h4>
```

For the **tube** card (around line 5584), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><rect x="2" y="5" width="20" height="14" rx="3"/><polygon points="10 9 16 12 10 15" fill="currentColor" stroke="none"/></svg></span>Tube Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><rect x="2" y="5" width="20" height="14" rx="3"/><polygon points="10 9 16 12 10 15" fill="currentColor" stroke="none"/></svg></span><span class="card-title">Tube Archive</span><span class="card-count" data-archive-id="tube">—</span></h4>
```

For the **audio** card (around line 5589), replace:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 18v-6a9 9 0 0 1 18 0v6"/><path d="M21 19a2 2 0 0 1-2 2h-1v-6h3zM3 19a2 2 0 0 0 2 2h1v-6H3z"/></svg></span>Audiobook Archive</h4>
```

with:

```html
                <h4><span class="card-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 18v-6a9 9 0 0 1 18 0v6"/><path d="M21 19a2 2 0 0 1-2 2h-1v-6h3zM3 19a2 2 0 0 0 2 2h1v-6H3z"/></svg></span><span class="card-title">Audiobook Archive</span><span class="card-count" data-archive-id="audio">—</span></h4>
```

- [ ] **Step 3: Restart and visually inspect**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

Use Playwright:

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_take_screenshot → page (full)
mcp__playwright__browser_evaluate → () => Array.from(document.querySelectorAll('.card-count[data-archive-id]')).map(e => ({id: e.dataset.archiveId, text: e.textContent}))
```

Expected: seven entries, each with `text: "—"`. Each card's `<h4>` should now show the title left, the `—` badge right.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(clicks): card-count badge placeholder + flex h4 layout

Wraps each Featured Archive card title in .card-title and appends a
.card-count badge with data-archive-id. Empty placeholder ("—") for now;
populated by the bootstrap fetch in the next commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Card hrefs → `/go/<id>`

**Files:**
- Modify: `static/index.html` (the seven `<a class="card">` opening tags at lines 5558-5588)

**Why now:** with the badge placeholders rendering correctly, swap the link target. After this task, middle-click and right-click → "open in new tab" go through the tracked redirect, even before any JS runs.

- [ ] **Step 1: Replace each card's `href`**

For each of the seven cards, change only the `href=` value. The rest of the opening tag stays intact.

Wiki (around line 5558):

```html
              <a class="card" href="https://wiki.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('wiki');">
```

→

```html
              <a class="card" href="/go/wiki" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('wiki');">
```

Pdf (around line 5563):

```html
              <a class="card" href="https://pdf.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('pdf');">
```

→

```html
              <a class="card" href="/go/pdf" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('pdf');">
```

Os (around line 5568):

```html
              <a class="card" href="https://os.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('os');">
```

→

```html
              <a class="card" href="/go/os" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('os');">
```

Iso (around line 5573):

```html
              <a class="card" href="https://iso.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('iso');">
```

→

```html
              <a class="card" href="/go/iso" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('iso');">
```

Chiptune (around line 5578):

```html
              <a class="card" href="https://chiptune.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('chiptune');">
```

→

```html
              <a class="card" href="/go/chiptune" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('chiptune');">
```

Tube (around line 5583):

```html
              <a class="card" href="https://tube.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('tube');">
```

→

```html
              <a class="card" href="/go/tube" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('tube');">
```

Audio (around line 5588):

```html
              <a class="card" href="https://audio.kunaldawn.com" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('audio');">
```

→

```html
              <a class="card" href="/go/audio" rel="noopener" onclick="event.preventDefault(); openFeaturedArchive('audio');">
```

- [ ] **Step 2: Restart, verify hrefs in DOM**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

Playwright:

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => Array.from(document.querySelectorAll('a.card')).map(a => a.getAttribute('href'))
```

Expected: `["/go/wiki", "/go/pdf", "/go/os", "/go/iso", "/go/chiptune", "/go/tube", "/go/audio"]`.

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(clicks): card hrefs point at /go/<id> tracked redirect

Middle-click, Ctrl-click, and right-click → 'open in new tab' on a
Featured Archive card now flow through the server's redirect handler
and increment the per-archive counter. The onclick handler still calls
openFeaturedArchive(id) for the regular click path; that's updated next.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Bootstrap fetch — populate `.card-count` badges

**Files:**
- Modify: `static/index.html` (insert a new IIFE near the visit-counter IIFE around line 5988)

- [ ] **Step 1: Add the bootstrap IIFE right after the visit-counter one**

Anchor: the visit-counter IIFE ends with `})();` around line 5988 (the line right above the `// ARCHIVE CAT` banner). Insert the new IIFE on the line *after* that closing `})();` and before the empty line that precedes the cat banner.

Find this:

```js
            .catch(() => { /* silent: no counter is fine */ });
        })();

        // ═══════════════════════════════════════════════════════════
        // ARCHIVE CAT — A Clippy-inspired state machine companion
```

Replace with:

```js
            .catch(() => { /* silent: no counter is fine */ });
        })();

        // Featured Archive click counters. Reads /api/archive-clicks once on
        // load and writes each {id: n} into its .card-count badge. Optimistic
        // bumps inside openFeaturedArchive() keep the UI live between loads.
        (function () {
          fetch('/api/archive-clicks')
            .then(r => r.ok ? r.json() : null)
            .then(data => {
              if (!data || !data.counts) return;
              document.querySelectorAll('.card-count[data-archive-id]').forEach(el => {
                const id = el.dataset.archiveId;
                const n = data.counts[id] || 0;
                el.textContent = n.toLocaleString() + ' opens';
              });
            })
            .catch(() => { /* silent: badges keep their — placeholder */ });
        })();

        // ═══════════════════════════════════════════════════════════
        // ARCHIVE CAT — A Clippy-inspired state machine companion
```

- [ ] **Step 2: Restart and verify badges populate**

First seed some counts so the test isn't all zeros:

```bash
pkill -f /tmp/kdhome ; sleep 0.3
rm -rf /tmp/kdhome-data
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
for i in 1 2 3 4 5; do curl -so /dev/null http://localhost:8089/go/wiki; done
for i in 1 2; do curl -so /dev/null http://localhost:8089/go/pdf; done
sleep 0.3
```

Playwright:

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => Array.from(document.querySelectorAll('.card-count[data-archive-id]')).map(e => ({id: e.dataset.archiveId, text: e.textContent}))
```

Expected:

```json
[
  {"id":"wiki","text":"5 opens"},
  {"id":"pdf","text":"2 opens"},
  {"id":"os","text":"0 opens"},
  {"id":"iso","text":"0 opens"},
  {"id":"chiptune","text":"0 opens"},
  {"id":"tube","text":"0 opens"},
  {"id":"audio","text":"0 opens"}
]
```

Take a screenshot to confirm visual layout:

```
mcp__playwright__browser_take_screenshot → page
```

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(clicks): bootstrap fetch populates .card-count badges

Adds a small IIFE that hits /api/archive-clicks on load and writes
'<n> opens' into each Featured Archive card's badge. Failure leaves the
'—' placeholder in place — silent like the visit counter.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: `openFeaturedArchive` — open `/go/<id>` + optimistic UI bump

**Files:**
- Modify: `static/index.html` (function `loadIframe` around line 8433; function `openFeaturedArchive` around line 8553)

**Why:** With the redirect handler live, the JS-driven open paths (left-click on a card, start-menu item) now need to use `/go/<id>` too — otherwise they'd bypass the counter. And the optimistic UI bump gives the user instant feedback without waiting for a reload.

- [ ] **Step 1: Make `loadIframe` use the tracked URL**

Anchor: search for `function loadIframe(inst) {` (around line 8433).

Replace this function:

```js
        function loadIframe(inst) {
          var iframe = document.createElement('iframe');
          iframe.src = inst.entry.url;
          inst.contentEl.classList.add('loading');
          var done = function() {
            inst.contentEl.classList.remove('loading');
            iframe.removeEventListener('load', done);
            if (inst._loadTimer) { clearTimeout(inst._loadTimer); inst._loadTimer = null; }
          };
          iframe.addEventListener('load', done);
          inst._loadTimer = setTimeout(done, 10000);
          inst.contentEl.innerHTML = '';
          inst.contentEl.appendChild(iframe);
          inst.iframe = iframe;
        }
```

with:

```js
        function loadIframe(inst) {
          var iframe = document.createElement('iframe');
          // Same-origin /go/<id> → server increments + 302s to inst.entry.url.
          // CSP frame-src includes 'self' so the initial load isn't blocked.
          iframe.src = '/go/' + inst.entry.id;
          inst.contentEl.classList.add('loading');
          var done = function() {
            inst.contentEl.classList.remove('loading');
            iframe.removeEventListener('load', done);
            if (inst._loadTimer) { clearTimeout(inst._loadTimer); inst._loadTimer = null; }
          };
          iframe.addEventListener('load', done);
          inst._loadTimer = setTimeout(done, 10000);
          inst.contentEl.innerHTML = '';
          inst.contentEl.appendChild(iframe);
          inst.iframe = iframe;
        }
```

Note: `reloadInstance` (around line 8499) keeps using `inst.entry.url + sep + '_kd_reload=...'` — reload should NOT re-count a click, so it bypasses `/go/`. That's the existing code; leave it alone.

- [ ] **Step 2: Make `openFeaturedArchive` open `/go/<id>` + bump the badge**

Anchor: search for `window.openFeaturedArchive = function(id) {` (around line 8553).

Replace this function:

```js
        window.openFeaturedArchive = function(id) {
          var entry = REGISTRY[id];
          if (!entry) return;
          var useInternal = false;
          try { useInternal = localStorage.getItem('kd:prefs:archives-internal-browser') === 'on'; } catch(e) {}
          if (!useInternal) {
            window.open(entry.url, '_blank', 'noopener');
            return;
          }
          var existing = instances.get(id);
          if (existing) {
            // Restore if minimized; bring to front either way.
            if (existing.isMin || existing.el.style.display === 'none') {
              toggleInstanceFromTaskbar(existing);
            } else {
              window.bringToFront(existing.el);
            }
            var menu = document.getElementById('startMenu');
            if (menu && menu.classList.contains('show')) toggleStartMenu();
            return;
          }
          var inst = makeInstance(id);
          if (!inst) return;
          instances.set(id, inst);
          placeNewWindow(inst);
          showInstance(inst);
          loadIframe(inst);
          maybeMobileMaximize(inst);
          var menu = document.getElementById('startMenu');
          if (menu && menu.classList.contains('show')) toggleStartMenu();
        };
```

with:

```js
        // Optimistic bump on the home-view badge so the user sees their click
        // immediately. Server is still authoritative; next reload reconciles.
        function bumpCardCount(id) {
          var badge = document.querySelector('.card-count[data-archive-id="' + id + '"]');
          if (!badge) return;
          var current = parseInt((badge.textContent || '').replace(/[^0-9]/g, ''), 10);
          if (isNaN(current)) current = 0;
          badge.textContent = (current + 1).toLocaleString() + ' opens';
        }

        window.openFeaturedArchive = function(id) {
          var entry = REGISTRY[id];
          if (!entry) return;
          bumpCardCount(id);
          var useInternal = false;
          try { useInternal = localStorage.getItem('kd:prefs:archives-internal-browser') === 'on'; } catch(e) {}
          if (!useInternal) {
            // /go/<id> → server increments + 302s to the archive URL.
            window.open('/go/' + id, '_blank', 'noopener');
            return;
          }
          var existing = instances.get(id);
          if (existing) {
            // Restore if minimized; bring to front either way. No new
            // /go/ hit — the original open already counted, and re-focusing
            // an already-open window isn't a click.
            if (existing.isMin || existing.el.style.display === 'none') {
              toggleInstanceFromTaskbar(existing);
            } else {
              window.bringToFront(existing.el);
            }
            var menu = document.getElementById('startMenu');
            if (menu && menu.classList.contains('show')) toggleStartMenu();
            return;
          }
          var inst = makeInstance(id);
          if (!inst) return;
          instances.set(id, inst);
          placeNewWindow(inst);
          showInstance(inst);
          loadIframe(inst); // iframe loads /go/<id> → 302 → archive
          maybeMobileMaximize(inst);
          var menu = document.getElementById('startMenu');
          if (menu && menu.classList.contains('show')) toggleStartMenu();
        };
```

Note one subtlety: `bumpCardCount` is called once at the top, *before* the early-return for already-open internal-browser windows. That early return doesn't actually count as a new open server-side, so we'd ideally not bump there — but for simplicity (and because re-clicking a card to refocus is rare on a desktop with seven cards), we accept the one-extra bump on refocus. It reconciles on next reload.

If you want stricter behavior, move `bumpCardCount(id);` to *after* the existing-instance early-return — i.e., place it right before `var inst = makeInstance(id);` and add a duplicate at the start of the `useInternal === false` branch. Either is fine.

- [ ] **Step 3: Restart and verify the new-tab path**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
rm -rf /tmp/kdhome-data
go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

Playwright:

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => { window.openFeaturedArchive('wiki'); return document.querySelector('.card-count[data-archive-id="wiki"]').textContent; }
```

Expected: returned text is `"1 opens"` (optimistic bump).

Confirm server-side:

```bash
sleep 0.3
curl -s http://localhost:8089/api/archive-clicks
```

Expected: `wiki` count is `1`. (The `window.open` was suppressed by the test environment headlessly, but Playwright's evaluate still ran the bump; the increment happens server-side via the new tab nav. If your headless environment doesn't actually navigate the popup, hit the redirect manually with `curl -so /dev/null http://localhost:8089/go/wiki` to confirm symmetry.)

- [ ] **Step 4: Verify the internal-browser path**

```
mcp__playwright__browser_evaluate → () => { localStorage.setItem('kd:prefs:archives-internal-browser', 'on'); return localStorage.getItem('kd:prefs:archives-internal-browser'); }
mcp__playwright__browser_navigate → http://localhost:8089/   (reload so the pref is read)
mcp__playwright__browser_evaluate → () => { window.openFeaturedArchive('pdf'); return null; }
```

Wait a moment for the iframe to load:

```
mcp__playwright__browser_wait_for → text "PDF Archive" or selector ".browser-window:not(.hidden)"
mcp__playwright__browser_evaluate → () => document.querySelector('.browser-window:not(.hidden) iframe')?.src
```

Expected: the iframe `src` is either the same-origin `/go/pdf` OR the post-redirect `https://pdf.kunaldawn.com/` — both are correct (browsers vary on whether `iframe.src` reports pre- or post-redirect). The important check is server-side:

```bash
sleep 0.3
curl -s http://localhost:8089/api/archive-clicks
```

Expected: `pdf` count went up by exactly 1.

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(clicks): openFeaturedArchive routes via /go/<id> + optimistic bump

window.open and the internal-browser iframe both target /go/<id>, so the
server counter sees clicks regardless of which open path the user took.
A small bumpCardCount() helper updates the home-view badge immediately
for instant feedback; the server reconciles on next reload.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: End-to-end verification under docker compose with screenshots

**Files:**
- Modify: none (verification only)

**Why a separate task:** The user explicitly asked to verify with Playwright + screenshots, and the fix is specifically about behaviour under `docker compose`. Earlier tasks used `go build` + bare binary for fast iteration; this task closes the loop with the actual deploy target.

- [ ] **Step 1: Stop the bare binary, build and start docker compose**

```bash
pkill -f /tmp/kdhome ; sleep 0.3
cd /home/kunaldawn/workspace/repos/kdhome
docker compose build
docker compose up -d
sleep 3
docker compose logs --tail=30 kdhome
```

Expected logs include:

```
[VISITS] initialized — total: <some number>
[CLICKS] hydrated 7 archive(s)
[STATIC] cached N files
[SERVER] serving ./static on :8080
```

Note: the host bind mount means visit/click counts persist across container restarts — they'll start from whatever was last in `./data/visits.db`, not from 0.

- [ ] **Step 2: Snapshot baseline counts**

```bash
curl -s http://localhost:8888/api/visit
curl -s http://localhost:8888/api/archive-clicks
```

Save these numbers; you'll diff against them.

- [ ] **Step 3: Drive a visit + click pattern in Playwright**

```
mcp__playwright__browser_navigate → http://localhost:8888/
mcp__playwright__browser_take_screenshot → home page (full)         [SCREENSHOT 1]
```

Then click each card via JS and screenshot the result:

```
mcp__playwright__browser_evaluate → () => { window.openFeaturedArchive('wiki'); window.openFeaturedArchive('pdf'); window.openFeaturedArchive('os'); return null; }
mcp__playwright__browser_take_screenshot → cards after clicks       [SCREENSHOT 2]
```

Open the start menu and screenshot it (proves clicks from start menu also flow through the redirect):

```
mcp__playwright__browser_evaluate → () => toggleStartMenu()
mcp__playwright__browser_take_screenshot → start menu open          [SCREENSHOT 3]
mcp__playwright__browser_evaluate → () => { document.querySelector('.menu-item[onclick*="openFeaturedArchive(\'iso\')"]').click(); return null; }
```

- [ ] **Step 4: Confirm server-side counters moved**

```bash
sleep 0.5
curl -s http://localhost:8888/api/visit
curl -s http://localhost:8888/api/archive-clicks
```

Expected:

- `/api/visit` → baseline + at least 2 (the original navigate + the toggleStartMenu navigate counted only one; reload counts another). The exact number depends on how many times Playwright fetched `/`.
- `/api/archive-clicks` → `wiki`, `pdf`, `os`, `iso` each up by exactly 1 from baseline. The other three unchanged.

Reload to see the badges populated by the server-side numbers (no longer just optimistic):

```
mcp__playwright__browser_navigate → http://localhost:8888/
mcp__playwright__browser_take_screenshot → home page after reload    [SCREENSHOT 4]
mcp__playwright__browser_evaluate → () => Array.from(document.querySelectorAll('.card-count[data-archive-id]')).map(e => ({id: e.dataset.archiveId, text: e.textContent}))
```

Expected: each clicked archive shows the exact server-confirmed count.

- [ ] **Step 5: Confirm visit counter rises across cache hits (the original docker bug)**

```bash
BEFORE=$(curl -s http://localhost:8888/api/visit | grep -oE '[0-9]+')
ETAG=$(curl -sI http://localhost:8888/ | awk -F': ' '/^ETag/{print $2}' | tr -d '\r')
curl -sI -H "If-None-Match: $ETAG" http://localhost:8888/ | head -1
sleep 0.3
AFTER=$(curl -s http://localhost:8888/api/visit | grep -oE '[0-9]+')
echo "before=$BEFORE after=$AFTER"
```

Expected: `after = before + 1`. The 304 response counted — that's the whole point of moving the counter server-side.

- [ ] **Step 6: Tear down or leave running, then commit**

If you want to leave docker running for inspection, do nothing. Otherwise:

```bash
docker compose down
```

This task makes no code changes, so no commit is required. If you'd like the screenshots checked in, save them under `docs/superpowers/specs/screenshots/2026-05-09-server-side-counters/` and:

```bash
git add docs/superpowers/specs/screenshots/2026-05-09-server-side-counters/
git commit -m "$(cat <<'EOF'
docs(clicks): playwright verification screenshots under docker compose

Captures the home view with click counters, the home view after clicks,
the start menu, and the post-reload state where badges reflect
server-confirmed counts. Confirms the 304-conditional-GET visit count
also rises, fixing the under-count under docker compose.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Spec coverage check

| Spec section                                  | Implemented in |
|------------------------------------------------|----------------|
| `archive_click_count` table + seed             | Task 1         |
| Server registry (`archives` slice + URL map)   | Task 1         |
| `recordVisit` trigger moved into `staticHandler` | Task 3       |
| Skip HEAD; count GET / + GET /index.html       | Task 3 step 2  |
| 304 responses still counted                    | Task 3 (increment is before conditional-GET branch) |
| `POST /api/visit` removed                      | Task 3         |
| `GET /api/visit` unchanged display source      | Task 3         |
| `GET /go/<id>` async increment + 302           | Task 2         |
| Unknown id → 404                               | Task 2         |
| `GET /api/archive-clicks` JSON shape           | Task 2         |
| CSP `frame-src 'self'`                         | Task 4         |
| Visit fetch verb POST → GET                    | Task 5         |
| Card hrefs → `/go/<id>`                        | Task 7         |
| `openFeaturedArchive` → `/go/<id>` (new tab)   | Task 9         |
| `loadIframe` → `/go/<id>` (internal browser)   | Task 9         |
| `.card-count` badge HTML + CSS                 | Task 6         |
| Bootstrap fetch populates badges               | Task 8         |
| Optimistic UI bump on `openFeaturedArchive`    | Task 9         |
| Verification under docker compose + screenshots | Task 10       |

All spec items are covered. Tasks are sequenced so each is independently verifiable, and the server is functional after every commit (no half-states).
