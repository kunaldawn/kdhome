# Maintenance Mode, Tile Cleanup & Auto-Hide Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add env-configurable maintenance mode, remove the per-tile "opens" counter and its backend, and auto-hide featured archives that have been down for over an hour.

**Architecture:** A new `maintenance.go` reads env vars at startup and (when enabled) wraps the whole handler chain with a 503-serving middleware rendering a self-contained themed countdown page. The click-tracking subsystem is deleted from `main.go` and `index.html`. The status probe in `status.go` gains an in-memory hide/restore state machine (`updateVisibility`) exposed as `"hidden"` in `/api/status.json`, which a home-view JS poller uses to toggle tile + Start-menu visibility.

**Tech Stack:** Go 1.22 (`net/http`, `mattn/go-sqlite3`), vanilla JS, single-file `static/index.html`.

## Global Constraints

- Go version floor: **Go 1.22** (per Dockerfile `golang:1.22-alpine`).
- No new third-party dependencies — standard library only.
- Env vars are read **once at startup** in `main()`, matching the existing `PORT` / `DATA_DIR` pattern (`os.Getenv`).
- The frontend is a single hand-edited file (`static/index.html`) — no build step; match surrounding code style (2-space indent in JS blocks, `var`/`function` in the legacy IIFEs).
- Truthy env parsing for `MAINTENANCE_MODE`: case-insensitive `1`, `true`, `on`, `yes`. Anything else = off.
- Maintenance default message: `We'll be back soon.`
- Hide threshold: **1 hour** down → hidden. Restore window: **1 minute** up → shown.
- Maintenance responses use **HTTP 503** + `Retry-After` + `Cache-Control: no-store`.
- Run the full Go suite with `CGO_ENABLED=1 go test ./...` (sqlite3 needs cgo).

---

## Task 1: Maintenance config + page rendering (`maintenance.go`)

**Files:**
- Create: `maintenance.go`
- Test: `maintenance_test.go`

**Interfaces:**
- Consumes: nothing (new file).
- Produces:
  - `type maintenanceConfig struct { Enabled bool; End time.Time; HasEnd bool; Message string }`
  - `func loadMaintenanceConfig() maintenanceConfig`
  - `func buildMaintenancePage(cfg maintenanceConfig) []byte`
  - `func maintenanceMiddleware(page []byte, cfg maintenanceConfig) func(http.Handler) http.Handler`

- [ ] **Step 1: Write the failing test**

Create `maintenance_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadMaintenanceConfig(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		os.Unsetenv("MAINTENANCE_MODE")
		os.Unsetenv("MAINTENANCE_END")
		os.Unsetenv("MAINTENANCE_MESSAGE")
		cfg := loadMaintenanceConfig()
		if cfg.Enabled {
			t.Fatal("expected disabled when MAINTENANCE_MODE unset")
		}
		if cfg.Message != "We'll be back soon." {
			t.Fatalf("default message = %q", cfg.Message)
		}
	})

	t.Run("truthy values enable", func(t *testing.T) {
		for _, v := range []string{"1", "true", "TRUE", "on", "YES"} {
			os.Setenv("MAINTENANCE_MODE", v)
			if !loadMaintenanceConfig().Enabled {
				t.Fatalf("MAINTENANCE_MODE=%q should enable", v)
			}
		}
		for _, v := range []string{"", "0", "off", "no", "nope"} {
			os.Setenv("MAINTENANCE_MODE", v)
			if loadMaintenanceConfig().Enabled {
				t.Fatalf("MAINTENANCE_MODE=%q should NOT enable", v)
			}
		}
		os.Unsetenv("MAINTENANCE_MODE")
	})

	t.Run("parses end time and message", func(t *testing.T) {
		os.Setenv("MAINTENANCE_MODE", "on")
		os.Setenv("MAINTENANCE_END", "2026-06-20T15:00:00Z")
		os.Setenv("MAINTENANCE_MESSAGE", "Upgrading the rig.")
		defer func() {
			os.Unsetenv("MAINTENANCE_MODE")
			os.Unsetenv("MAINTENANCE_END")
			os.Unsetenv("MAINTENANCE_MESSAGE")
		}()
		cfg := loadMaintenanceConfig()
		if !cfg.HasEnd || cfg.End.IsZero() {
			t.Fatal("expected parsed end time")
		}
		if cfg.Message != "Upgrading the rig." {
			t.Fatalf("message = %q", cfg.Message)
		}
	})

	t.Run("invalid end time → HasEnd false", func(t *testing.T) {
		os.Setenv("MAINTENANCE_MODE", "on")
		os.Setenv("MAINTENANCE_END", "not-a-date")
		defer func() {
			os.Unsetenv("MAINTENANCE_MODE")
			os.Unsetenv("MAINTENANCE_END")
		}()
		cfg := loadMaintenanceConfig()
		if cfg.HasEnd {
			t.Fatal("invalid end time should leave HasEnd false")
		}
	})
}

func TestBuildMaintenancePage(t *testing.T) {
	cfg := maintenanceConfig{
		Enabled: true,
		End:     time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC),
		HasEnd:  true,
		Message: "Upgrading <the> rig.",
	}
	page := string(buildMaintenancePage(cfg))
	if !strings.Contains(page, "2026-06-20T15:00:00Z") {
		t.Fatal("page should embed the ISO end time")
	}
	// message must be HTML-escaped
	if !strings.Contains(page, "Upgrading &lt;the&gt; rig.") {
		t.Fatal("message should be HTML-escaped in the page")
	}
	if strings.Contains(page, "Upgrading <the> rig.") {
		t.Fatal("raw unescaped message must not appear")
	}
}

func TestMaintenanceMiddleware(t *testing.T) {
	cfg := maintenanceConfig{Enabled: true, Message: "soon"}
	page := buildMaintenancePage(cfg)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("REAL SITE"))
	})
	h := maintenanceMiddleware(page, cfg)(inner)

	for _, path := range []string{"/", "/api/status.json", "/static/x.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: code = %d, want 503", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "REAL SITE") {
			t.Fatalf("%s: inner handler should not run", path)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s: missing Retry-After", path)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control = %q", path, rec.Header().Get("Cache-Control"))
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test ./... -run 'Maintenance' -v`
Expected: FAIL — `undefined: loadMaintenanceConfig` (and the other symbols).

- [ ] **Step 3: Write the implementation**

Create `maintenance.go`:

```go
package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// maintenanceConfig captures the env-driven maintenance settings, read once
// at startup. When Enabled is false the middleware is never installed.
type maintenanceConfig struct {
	Enabled bool
	End     time.Time
	HasEnd  bool
	Message string
}

// loadMaintenanceConfig reads MAINTENANCE_MODE / MAINTENANCE_END /
// MAINTENANCE_MESSAGE. MODE is truthy on 1/true/on/yes (case-insensitive).
// END is RFC3339; an unset or unparseable value leaves HasEnd false (page
// renders with no countdown). MESSAGE defaults to a friendly line.
func loadMaintenanceConfig() maintenanceConfig {
	cfg := maintenanceConfig{Message: "We'll be back soon."}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAINTENANCE_MODE"))) {
	case "1", "true", "on", "yes":
		cfg.Enabled = true
	}

	if raw := strings.TrimSpace(os.Getenv("MAINTENANCE_END")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			cfg.End = t.UTC()
			cfg.HasEnd = true
		} else if cfg.Enabled {
			log.Printf("[MAINT] ignoring unparseable MAINTENANCE_END %q: %v", raw, err)
		}
	}

	if msg := strings.TrimSpace(os.Getenv("MAINTENANCE_MESSAGE")); msg != "" {
		cfg.Message = msg
	}

	return cfg
}

// buildMaintenancePage renders the self-contained themed 503 page once at
// startup. It carries inline CSS + JS only (every asset request is blocked
// by the middleware, so it can't depend on external files). When HasEnd is
// set, the embedded ISO timestamp drives a client-side countdown.
func buildMaintenancePage(cfg maintenanceConfig) []byte {
	endISO := ""
	if cfg.HasEnd {
		endISO = cfg.End.Format(time.RFC3339)
	}
	return []byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Under Maintenance — kunaldawn.com</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  html, body { height: 100%%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    background: #05110d;
    color: #cfeee0;
    font-family: 'Share Tech Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
    padding: 24px;
    background-image: radial-gradient(circle at 50%% 0%%, rgba(127,209,179,0.08), transparent 60%%);
  }
  .box {
    width: 100%%; max-width: 560px; text-align: center;
    border: 1px solid rgba(127,209,179,0.35);
    border-radius: 10px;
    padding: 40px 28px;
    background: rgba(10,28,22,0.6);
    box-shadow: 0 0 40px rgba(0,0,0,0.5);
  }
  h1 { font-size: 22px; margin: 0 0 6px; color: #7fd1b3; letter-spacing: 1px; }
  .tag { font-size: 12px; opacity: 0.7; margin-bottom: 22px; }
  .msg { font-size: 15px; line-height: 1.5; margin: 0 0 26px; }
  .countdown { display: flex; gap: 14px; justify-content: center; margin-top: 8px; }
  .unit { min-width: 58px; }
  .num { font-size: 30px; color: #7fd1b3; }
  .lbl { font-size: 10px; text-transform: uppercase; opacity: 0.6; letter-spacing: 1px; }
  .done { font-size: 15px; color: #7fd1b3; }
  .footer { margin-top: 28px; font-size: 11px; opacity: 0.5; }
</style>
</head>
<body>
  <div class="box">
    <h1>// UNDER MAINTENANCE</h1>
    <div class="tag">KD's Homebrew Digital Archive</div>
    <p class="msg">%s</p>
    <div id="cd" class="countdown" hidden>
      <div class="unit"><div class="num" id="cd-d">0</div><div class="lbl">days</div></div>
      <div class="unit"><div class="num" id="cd-h">0</div><div class="lbl">hours</div></div>
      <div class="unit"><div class="num" id="cd-m">0</div><div class="lbl">min</div></div>
      <div class="unit"><div class="num" id="cd-s">0</div><div class="lbl">sec</div></div>
    </div>
    <div id="done" class="done" hidden>back any moment now&hellip;</div>
    <div class="footer">no cloud · no CDN · just a shelf and enough panels to cover the draw</div>
  </div>
  <script>
    (function () {
      var endISO = %q;
      if (!endISO) return;
      var end = new Date(endISO).getTime();
      if (isNaN(end)) return;
      var cd = document.getElementById('cd');
      var done = document.getElementById('done');
      function pad(n) { return n < 10 ? '0' + n : '' + n; }
      function tick() {
        var diff = end - Date.now();
        if (diff <= 0) {
          cd.hidden = true;
          done.hidden = false;
          return;
        }
        cd.hidden = false;
        var s = Math.floor(diff / 1000);
        document.getElementById('cd-d').textContent = Math.floor(s / 86400);
        document.getElementById('cd-h').textContent = pad(Math.floor((s %% 86400) / 3600));
        document.getElementById('cd-m').textContent = pad(Math.floor((s %% 3600) / 60));
        document.getElementById('cd-s').textContent = pad(s %% 60);
      }
      tick();
      setInterval(tick, 1000);
    })();
  </script>
</body>
</html>`, html.EscapeString(cfg.Message), endISO))
}

// maintenanceMiddleware serves the pre-rendered page with 503 + Retry-After
// for every request, short-circuiting the real handler chain entirely.
func maintenanceMiddleware(page []byte, cfg maintenanceConfig) func(http.Handler) http.Handler {
	retry := "3600"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ra := retry
			if cfg.HasEnd {
				if secs := int(time.Until(cfg.End).Seconds()); secs > 0 {
					ra = strconv.Itoa(secs)
				} else {
					ra = "60"
				}
			}
			h := w.Header()
			h.Set("Content-Type", "text/html; charset=utf-8")
			h.Set("Cache-Control", "no-store")
			h.Set("Retry-After", ra)
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				w.Write(page)
			}
		})
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test ./... -run 'Maintenance' -v`
Expected: PASS — `TestLoadMaintenanceConfig`, `TestBuildMaintenancePage`, `TestMaintenanceMiddleware` all green.

- [ ] **Step 5: Commit**

```bash
git add maintenance.go maintenance_test.go
git commit -m "feat: add env-configurable maintenance mode with countdown page"
```

---

## Task 2: Wire maintenance middleware into `main()`

**Files:**
- Modify: `main.go` (the `main()` function, around lines 654–673)

**Interfaces:**
- Consumes: `loadMaintenanceConfig`, `buildMaintenancePage`, `maintenanceMiddleware` (Task 1).
- Produces: nothing new.

- [ ] **Step 1: Modify the handler wiring**

In `main.go`, find this block (currently lines ~662–670):

```go
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           securityHeaders(mux),
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
```

Replace it with:

```go
	var handler http.Handler = securityHeaders(mux)

	// Maintenance mode (env-gated). When enabled, the middleware wraps the
	// whole chain and serves a themed 503 for every request; when disabled
	// it's never installed, so there's zero request-path overhead.
	if mcfg := loadMaintenanceConfig(); mcfg.Enabled {
		handler = maintenanceMiddleware(buildMaintenancePage(mcfg), mcfg)(handler)
		log.Printf("[MAINT] maintenance mode ENABLED (end set: %t)", mcfg.HasEnd)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
```

- [ ] **Step 2: Verify it builds and the suite passes**

Run: `CGO_ENABLED=1 go build . && CGO_ENABLED=1 go test ./...`
Expected: build succeeds; all tests PASS.

- [ ] **Step 3: Manual smoke test (maintenance ON)**

Run:
```bash
MAINTENANCE_MODE=on MAINTENANCE_END=2026-12-31T00:00:00Z MAINTENANCE_MESSAGE="Upgrading the rig." PORT=8099 CGO_ENABLED=1 go run . &
sleep 2
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/
curl -s http://localhost:8099/ | grep -c "UNDER MAINTENANCE"
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/api/status.json
kill %1
```
Expected: first curl prints `503`; grep prints `1`; the `/api/status.json` curl also prints `503`.

- [ ] **Step 4: Manual smoke test (maintenance OFF)**

Run:
```bash
PORT=8099 CGO_ENABLED=1 go run . &
sleep 2
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/
kill %1
```
Expected: prints `200` (normal site served).

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat: install maintenance middleware when MAINTENANCE_MODE is enabled"
```

---

## Task 3: Remove click-tracking backend (`main.go`)

**Files:**
- Modify: `main.go` (remove globals, `initVisitDB` blocks, handler, route)

**Interfaces:**
- Consumes: nothing.
- Produces: removes `/api/archive-clicks`, `archiveClicksHandler`, `recordArchiveClick`, `archiveClicks`, `archiveClicksMu`, `archiveID`. The `archives` slice STAYS.

- [ ] **Step 1: Remove the click-tracking globals**

In `main.go`, delete the `archiveID` declaration (currently lines ~51–53):

```go
// archiveID lets archiveClicksHandler validate POST {"id": ...} bodies
// against the known set without touching the DB.
var archiveID = map[string]bool{}
```

And delete the `archiveClicks` / `archiveClicksMu` block (currently lines ~55–60):

```go
// archiveClicks holds the in-memory aggregate, mirrored to SQLite. Keyed by
// archive ID. Guarded by archiveClicksMu.
var (
	archiveClicksMu sync.RWMutex
	archiveClicks   = map[string]int64{}
)
```

- [ ] **Step 2: Remove the click tables/seeding/hydration from `initVisitDB`**

In `initVisitDB`, delete the entire `// ─── Archive click counts ───` section: the `archiveID[a.ID] = true` loop, the `archive_click_count` `CREATE TABLE`, the seed loop, and the hydration `rows` block with its `[CLICKS]` logging (currently lines ~124–184). This is everything from:

```go
	// ─── Archive click counts ───
	for _, a := range archives {
		archiveID[a.ID] = true
	}
```

down through (and including):

```go
		log.Printf("[CLICKS] hydrated %d archive(s)", len(archiveClicks))
	}
```

Leave the `probe_daily` `CREATE TABLE` block (which sits between them, currently ~148–163) in place, and leave the final `log.Printf("[VISITS] initialized — humans: %d, bots: %d", human, bot)` line. After this edit, the `probe_daily` creation should come right after the visit-count hydration (`atomic.StoreInt64(&botCount, bot)`).

- [ ] **Step 3: Remove `recordArchiveClick` and `archiveClicksHandler`**

Delete `recordArchiveClick` (currently ~248–262) and `archiveClicksHandler` (currently ~264–300), including the `// ─── Archive Click Counter ───` banner comment above `recordArchiveClick`.

- [ ] **Step 4: Remove the route registration**

In `main()`, delete this line (currently ~656):

```go
	mux.HandleFunc("/api/archive-clicks", archiveClicksHandler)
```

- [ ] **Step 5: Fix imports if needed**

Check whether `sync` and `io` are still used elsewhere in `main.go`. `sync` is no longer used after removing `archiveClicksMu` (search: `grep -n 'sync\.' main.go`). `io` is still used by `playlistHandler` / `archiveClicksHandler` — after removing the handler, check `grep -n 'io\.' main.go`; `io.LimitReader` was only in the deleted handler, so `io` likely becomes unused too. Remove any now-unused imports from the `import` block so the build passes.

- [ ] **Step 6: Verify build and tests**

Run: `CGO_ENABLED=1 go build . && CGO_ENABLED=1 go test ./...`
Expected: build succeeds (no unused-import or undefined-symbol errors); all tests PASS.

- [ ] **Step 7: Verify the route is gone**

Run:
```bash
PORT=8099 CGO_ENABLED=1 go run . &
sleep 2
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8099/api/archive-clicks
kill %1
```
Expected: prints `404` (route no longer registered).

- [ ] **Step 8: Commit**

```bash
git add main.go
git commit -m "refactor: remove archive click-tracking subsystem"
```

---

## Task 4: Remove the "opens" badge from the frontend (`index.html`)

**Files:**
- Modify: `static/index.html` (CSS rule, 7 tile badges, load fetch, click helpers)

**Interfaces:**
- Consumes: nothing.
- Produces: tiles without `.card-count`; `openFeaturedArchive` no longer calls `bumpCardCount` / `trackArchiveClick`.

- [ ] **Step 1: Remove the `.card-count` CSS rule**

In `static/index.html`, delete this rule (currently lines ~4114–4120):

```css
    .card-count {
      color: #7fd1b3;
      font-family: 'Share Tech Mono', monospace;
      font-size: 11px;
      white-space: nowrap;
      opacity: 0.85;
    }
```

- [ ] **Step 2: Remove the `.card-count` span from all 7 tiles**

In each of the 7 Featured Archive tiles (lines ~7381–7411), remove the trailing `<span class="card-count" data-archive-id="...">—</span>` from inside the `<h4>`. For example, the wiki tile's `<h4>` changes from:

```html
<h4><span class="card-icon">...</span><span class="card-title">Wiki Archive</span><span class="card-count" data-archive-id="wiki">—</span></h4>
```

to:

```html
<h4><span class="card-icon">...</span><span class="card-title">Wiki Archive</span></h4>
```

Do this for all 7 ids: `wiki`, `pdf`, `os`, `iso`, `chiptune`, `tube`, `audio`. (The `data-archive-id` attribute will be re-added to the card anchor itself in Task 6 — it is NOT needed on the span.)

- [ ] **Step 3: Remove the load-time archive-clicks fetch**

Delete this IIFE (currently lines ~7828–7843):

```javascript
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
```

- [ ] **Step 4: Remove `bumpCardCount` and `trackArchiveClick`**

Delete `bumpCardCount` (currently ~10531–10539) including its leading comment:

```javascript
        // Optimistic bump on the home-view badge so the user sees their click
        // immediately. Server is still authoritative; next reload reconciles.
        function bumpCardCount(id) {
          var badge = document.querySelector('.card-count[data-archive-id="' + id + '"]');
          if (!badge) return;
          var current = parseInt((badge.textContent || '').replace(/[^0-9]/g, ''), 10);
          if (isNaN(current)) current = 0;
          badge.textContent = (current + 1).toLocaleString() + ' opens';
        }
```

And delete `trackArchiveClick` (currently ~10541–10554) including its leading comment:

```javascript
        // Fire-and-forget click tracking. `keepalive: true` lets the request
        // complete even if the page is navigating to a new tab. Failures are
        // silent — older deployments without the POST handler return 405 and
        // we just lose that tick (the optimistic badge still shows it).
        function trackArchiveClick(id) {
          try {
            fetch('/api/archive-clicks', {
              method: 'POST',
              body: JSON.stringify({ id: id }),
              headers: { 'Content-Type': 'application/json' },
              keepalive: true
            }).catch(function() {});
          } catch (e) { /* sendBeacon-style — never throws to caller */ }
        }
```

- [ ] **Step 5: Remove the calls inside `openFeaturedArchive`**

In `openFeaturedArchive` there are two call-sites. In the open-in-new-tab branch (currently ~10566–10567), remove the two lines:

```javascript
            bumpCardCount(id);
            trackArchiveClick(id);
```

so the branch becomes:

```javascript
          if (!useInternal) {
            // Open the archive directly in a new tab.
            window.open(entry.url, '_blank', 'noopener');
            return;
          }
```

In the new internal-browser-instance branch (currently ~10587–10588), remove the same two lines:

```javascript
          bumpCardCount(id);
          trackArchiveClick(id);
```

so the instance is created directly after the comment (`var inst = makeInstance(id);` follows).

- [ ] **Step 6: Verify no stragglers remain**

Run: `grep -n "card-count\|bumpCardCount\|trackArchiveClick\|archive-clicks" static/index.html`
Expected: **no output** (all references removed).

- [ ] **Step 7: Commit**

```bash
git add static/index.html
git commit -m "feat: remove per-tile opens counter from featured archives"
```

---

## Task 5: Add hide/restore state machine to the probe (`status.go`)

**Files:**
- Modify: `status.go` (consts, `probeState`, `debounceProbe`, `probeResult`, `probeOne`, `statusJSONHandler`)
- Test: `status_test.go`

**Interfaces:**
- Consumes: existing `probeStates`, `probeStateMu`, `debounceProbe`.
- Produces:
  - consts `hideThreshold`, `recoverWindow`
  - `func updateVisibility(st *probeState, now time.Time)`
  - `func isHidden(id string) bool`
  - `probeResult.Hidden bool` field
  - `/api/status.json` archive objects gain `"hidden":<bool>`

- [ ] **Step 1: Write the failing test**

Append to `status_test.go`:

```go
func TestUpdateVisibility(t *testing.T) {
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	t.Run("down under an hour stays visible", func(t *testing.T) {
		st := &probeState{effectiveOK: false}
		updateVisibility(st, base) // first down probe
		updateVisibility(st, base.Add(59*time.Minute))
		if st.hidden {
			t.Fatal("should not be hidden before 1h")
		}
	})

	t.Run("down for over an hour hides", func(t *testing.T) {
		st := &probeState{effectiveOK: false}
		updateVisibility(st, base)
		updateVisibility(st, base.Add(time.Hour))
		if !st.hidden {
			t.Fatal("should be hidden at 1h")
		}
	})

	t.Run("hidden then up under a minute stays hidden", func(t *testing.T) {
		st := &probeState{effectiveOK: false}
		updateVisibility(st, base)
		updateVisibility(st, base.Add(time.Hour)) // hidden
		st.effectiveOK = true
		updateVisibility(st, base.Add(time.Hour)) // up at T+1h
		updateVisibility(st, base.Add(time.Hour+30*time.Second))
		if !st.hidden {
			t.Fatal("should remain hidden until up >= 1m")
		}
	})

	t.Run("hidden then up for a minute restores", func(t *testing.T) {
		st := &probeState{effectiveOK: false}
		updateVisibility(st, base)
		updateVisibility(st, base.Add(time.Hour)) // hidden
		st.effectiveOK = true
		updateVisibility(st, base.Add(time.Hour)) // up clock starts
		updateVisibility(st, base.Add(time.Hour+time.Minute))
		if st.hidden {
			t.Fatal("should be visible after up >= 1m")
		}
	})

	t.Run("brief up then down again resets the down clock", func(t *testing.T) {
		st := &probeState{effectiveOK: false}
		updateVisibility(st, base)
		updateVisibility(st, base.Add(50*time.Minute)) // still visible
		// brief recovery
		st.effectiveOK = true
		updateVisibility(st, base.Add(51*time.Minute))
		// down again — down clock must restart from here, not the original base
		st.effectiveOK = false
		updateVisibility(st, base.Add(52*time.Minute))
		updateVisibility(st, base.Add(52*time.Minute+59*time.Minute)) // ~1h59m after base, but only 59m down
		if st.hidden {
			t.Fatal("down clock should have reset on the brief recovery")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test ./... -run 'UpdateVisibility' -v`
Expected: FAIL — `undefined: updateVisibility` and `st.hidden` field unknown.

- [ ] **Step 3: Add consts and extend `probeState`**

In `status.go`, extend the existing `const` block (currently ~21–27) to add the two thresholds:

```go
const (
	// probeInterval is the gap between probe sweeps (the "1 min gap").
	probeInterval = 1 * time.Minute
	// failureThreshold is how many consecutive failed probes mark an archive
	// down — like a Kubernetes livenessProbe failureThreshold.
	failureThreshold = 5
	// hideThreshold is how long an archive must stay (debounced) down before
	// it's dropped from the featured tiles / Start menu.
	hideThreshold = 1 * time.Hour
	// recoverWindow is how long a hidden archive must stay up before it's
	// shown again — guards against flapping.
	recoverWindow = 1 * time.Minute
)
```

Extend `probeState` (currently ~55–59):

```go
type probeState struct {
	consecFails int
	effectiveOK bool
	seenSuccess bool
	downSince   time.Time // when effectiveOK last became false (zero when up)
	upSince     time.Time // when effectiveOK last became true (zero when down)
	hidden      bool      // current featured-visibility decision
}
```

- [ ] **Step 4: Add `updateVisibility` and `isHidden`, call from `debounceProbe`**

Add these functions to `status.go` (e.g. right after `debounceProbe`):

```go
// updateVisibility folds the current (debounced) effectiveOK into the
// hide/restore state machine, using an explicit `now` so it's unit-testable.
// Down for >= hideThreshold hides the archive; once hidden, it's shown again
// only after staying up for >= recoverWindow. Caller holds probeStateMu.
func updateVisibility(st *probeState, now time.Time) {
	if st.effectiveOK {
		if st.upSince.IsZero() {
			st.upSince = now
		}
		st.downSince = time.Time{}
		if st.hidden && now.Sub(st.upSince) >= recoverWindow {
			st.hidden = false
		}
	} else {
		if st.downSince.IsZero() {
			st.downSince = now
		}
		st.upSince = time.Time{}
		if now.Sub(st.downSince) >= hideThreshold {
			st.hidden = true
		}
	}
}

// isHidden reports the current featured-visibility decision for an archive.
func isHidden(id string) bool {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	st := probeStates[id]
	return st != nil && st.hidden
}
```

In `debounceProbe`, call `updateVisibility` while the lock is held, just before the return. Change the tail of `debounceProbe` from:

```go
	} else {
		st.consecFails++
		if st.consecFails >= failureThreshold || !st.seenSuccess {
			st.effectiveOK = false
		}
	}
	return st.effectiveOK
}
```

to:

```go
	} else {
		st.consecFails++
		if st.consecFails >= failureThreshold || !st.seenSuccess {
			st.effectiveOK = false
		}
	}
	updateVisibility(st, time.Now())
	return st.effectiveOK
}
```

- [ ] **Step 5: Add `Hidden` to `probeResult` and populate it**

Add the field to `probeResult` (currently ~29–37):

```go
type probeResult struct {
	ID        string
	URL       string
	OK        bool
	Status    int
	LatencyMS int64
	CheckedAt time.Time
	Err       string
	Hidden    bool
}
```

In `probeOne`, set `r.Hidden = isHidden(id)` right before each `return r`. There are three return points (the `NewRequest` error path, the `client.Do` error path, and the success path). For each, add the line after the `recordProbeSample(...)` call and before `return r`. Example for the success path (currently ~161–163):

```go
	r.OK = debounceProbe(id, resp.StatusCode < 500)
	recordProbeSample(id, r.OK, latency)
	r.Hidden = isHidden(id)
	return r
```

Apply the same `r.Hidden = isHidden(id)` line to the other two `return r` paths.

- [ ] **Step 6: Emit `"hidden"` in `statusJSONHandler`**

In `statusJSONHandler`, the per-archive `fmt.Fprintf` (currently ~324–325) is:

```go
		fmt.Fprintf(w, `{"id":%q,"url":%q,"ok":%t,"status":%d,"latency_ms":%d,"checked_at":%q`,
			p.ID, p.URL, p.OK, p.Status, p.LatencyMS, p.CheckedAt.Format(time.RFC3339))
```

Change it to include `hidden`:

```go
		fmt.Fprintf(w, `{"id":%q,"url":%q,"ok":%t,"hidden":%t,"status":%d,"latency_ms":%d,"checked_at":%q`,
			p.ID, p.URL, p.OK, p.Hidden, p.Status, p.LatencyMS, p.CheckedAt.Format(time.RFC3339))
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `CGO_ENABLED=1 go test ./...`
Expected: PASS — new `TestUpdateVisibility` green AND the existing `TestDebounceProbe` still green (its return-value contract is unchanged).

- [ ] **Step 8: Commit**

```bash
git add status.go status_test.go
git commit -m "feat: hide featured archives after 1h down, restore after 1m up"
```

---

## Task 6: Home-view poller toggles tile + menu visibility (`index.html`)

**Files:**
- Modify: `static/index.html` (add `data-archive-id` to cards + menu items; add poller script)

**Interfaces:**
- Consumes: `/api/status.json` with `"hidden"` per archive (Task 5).
- Produces: nothing JS-global; self-contained IIFE.

- [ ] **Step 1: Add `data-archive-id` to each card anchor**

For each of the 7 Featured Archive `<a class="card" ...>` anchors (lines ~7380–7411), add a `data-archive-id` attribute matching its id. Example — the wiki anchor changes from:

```html
<a class="card" href="https://wiki.kunaldawn.com" rel="noopener" target="_blank" onclick="event.preventDefault(); openFeaturedArchive('wiki');">
```

to:

```html
<a class="card" data-archive-id="wiki" href="https://wiki.kunaldawn.com" rel="noopener" target="_blank" onclick="event.preventDefault(); openFeaturedArchive('wiki');">
```

Do this for all 7: `wiki`, `pdf`, `os`, `iso`, `chiptune`, `tube`, `audio`.

- [ ] **Step 2: Add `data-archive-id` to each Start-menu item**

For each of the 7 Quick Access `<div class="menu-item" onclick="openFeaturedArchive('...')">` entries (lines ~7007–7034), add a matching `data-archive-id`. Example — the wiki item changes from:

```html
<div class="menu-item" onclick="openFeaturedArchive('wiki')">
```

to:

```html
<div class="menu-item" data-archive-id="wiki" onclick="openFeaturedArchive('wiki')">
```

Do this for all 7 ids.

- [ ] **Step 3: Add the home-view visibility poller**

In place of the archive-clicks IIFE removed in Task 4 (the same spot in the script region near line ~7828, right after the visit-counter IIFE that ends with `})();`), add:

```javascript
        // Featured-archive auto-hide. Polls /api/status.json and removes any
        // archive marked `hidden` (down > 1h, server-decided) from both the
        // featured tiles and the Start-menu Quick Access list; restores it
        // once the server reports it visible again. Fail-open: on any error
        // everything stays visible.
        (function () {
          function applyVisibility(archives) {
            if (!Array.isArray(archives)) return;
            archives.forEach(function (a) {
              if (!a || !a.id) return;
              var hide = a.hidden === true;
              document
                .querySelectorAll('[data-archive-id="' + a.id + '"]')
                .forEach(function (el) {
                  el.style.display = hide ? 'none' : '';
                });
            });
          }
          function poll() {
            fetch('/api/status.json', { credentials: 'same-origin' })
              .then(function (r) { return r.ok ? r.json() : null; })
              .then(function (data) { if (data) applyVisibility(data.archives); })
              .catch(function () { /* fail-open: leave everything visible */ });
          }
          poll();
          setInterval(poll, 60000);
        })();
```

- [ ] **Step 4: Manual smoke test — hide path**

Run the server, confirm all tiles visible, then verify the toggle logic against a forced response. Since the live probe won't report `hidden` without a real 1h outage, verify the DOM logic directly in the browser console while the server runs:

```bash
PORT=8099 CGO_ENABLED=1 go run . &
sleep 2
# Confirm status.json now carries the hidden field:
curl -s http://localhost:8099/api/status.json | grep -o '"hidden":[a-z]*' | head -1
kill %1
```
Expected: prints `"hidden":false` (field present in the payload).

For the DOM toggle itself, open `http://localhost:8099/` and run in the browser console:
```javascript
document.querySelectorAll('[data-archive-id="wiki"]').forEach(e => e.style.display = 'none');
```
Expected: the Wiki tile AND the Wiki Start-menu item disappear; clearing `display` restores them. This confirms the selectors match both element types.

- [ ] **Step 5: Verify the field plumbs end-to-end**

Confirm there are exactly 14 `data-archive-id` occurrences (7 cards + 7 menu items):

Run: `grep -c 'data-archive-id' static/index.html`
Expected: `14`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "feat: auto-hide down archives from tiles and start menu"
```

---

## Self-Review Notes

- **Spec coverage:** Feature 1 → Tasks 1–2; Feature 2 → Tasks 3–4; Feature 3 → Tasks 5–6. All spec sections mapped.
- **Type consistency:** `maintenanceConfig` fields (`Enabled`, `End`, `HasEnd`, `Message`) are used identically across Tasks 1–2. `probeState.hidden`, `updateVisibility`, `isHidden`, `probeResult.Hidden`, and the `"hidden"` JSON key are consistent across Tasks 5–6. `data-archive-id` is the single attribute name used by both the JS poller and the markup.
- **Migration note:** the legacy `archive_click_count` table is intentionally left in existing DBs (no `DROP`) — Task 3 only stops reading/writing it.
- **Fail-open:** the Task 6 poller never hides on fetch error, matching the spec.
