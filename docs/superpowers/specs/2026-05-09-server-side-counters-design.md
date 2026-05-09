# Server-side Visit Counter + Per-archive Click Counter

**Date:** 2026-05-09
**Owner:** kunaldawn
**Status:** Draft

## Background

Two counters need to move (or be added) so behaviour stays correct under
docker compose and gives meaningful per-archive engagement data.

### Current visit counter — why it under-reports

`recordVisit()` in `main.go:86` is fired by a client-side
`fetch('/api/visit', { method: 'POST' })` IIFE in `static/index.html:5972-5988`.
The index page is served with `Cache-Control: public, max-age=300, must-revalidate`
(`main.go:262`). Consequences observed under `docker compose`:

- Repeat visits within the 5-minute TTL come from the browser's local cache;
  no HTTP request is made, so no POST is fired.
- Back-forward navigations restore the page from bfcache; the IIFE doesn't
  re-execute.
- Bots, prefetches, no-JS clients, and ad-blocked clients never POST at all.

The SQLite WAL is being written when the POST does fire (visible in
`git status`'s dirty `data/visits.db-wal`), so writes work — they just happen
far less often than real visits.

### Featured archive engagement — currently invisible

`openFeaturedArchive(id)` in `static/index.html:8553-8583` opens the archive
either in a new tab (`window.open`) or in the internal browser window. There
is no per-archive metric.

## Goals

1. Increment the visit counter **server-side** every time the index page is
   fetched, so the count reflects real traffic regardless of cache, JS, or bots.
2. Add a per-archive click counter that increments on every open path
   (home-page card click, start-menu item click, middle-click / Ctrl-click on a
   card, no-JS, ad-blocked) and is shown as a badge on each Featured Archive
   card.

## Non-goals

- Bot filtering. Counts are raw (every GET to `/`, every `/go/<id>` hit).
- Per-card analytics in the start menu. Start-menu items still increment the
  counter via the same redirect, but display only happens on the home cards.
- Time-series / windowed metrics. Counters are monotonic totals.

## Design

### Storage

Two SQLite tables, both single-row-per-key aggregates (no per-event rows):

```sql
-- existing, unchanged
CREATE TABLE visit_count (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  n  INTEGER NOT NULL DEFAULT 0
);

-- new
CREATE TABLE archive_click_count (
  id TEXT PRIMARY KEY,
  n  INTEGER NOT NULL DEFAULT 0
);
```

Seeded at startup with `INSERT OR IGNORE` for the seven known archive IDs
(`wiki, pdf, os, iso, chiptune, tube, audio`). Seeding ensures every card has
a count from day one and that GET endpoints return all keys even before the
first click.

In-memory cache: keep the existing `atomic.Int64 visitCount`, plus a new
`sync.Map`-protected `map[string]int64` for archive clicks (or a small
`struct { sync.RWMutex; m map[string]int64 }` — implementation detail).

### Server changes (`main.go`)

#### 1. Visit counter increment moved server-side

Inside `staticHandler`, after the cache lookup and before any 304 short-circuit:

```go
if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html") {
    recordVisit() // existing async-write goroutine
}
```

Skip `HEAD` requests (metadata-only) and any non-GET method. Run the
increment **before** the conditional-GET check so 304 responses still count —
the client did fetch the page.

`recordVisit()` keeps its current async pattern: goroutine does
`UPDATE visit_count SET n = n + 1` and `atomic.AddInt64(&visitCount, 1)`.
Response latency unchanged.

#### 2. Visit endpoint becomes read-only

- `POST /api/visit` is **removed**. Source of truth is now the server-side
  increment in `staticHandler`.
- `GET /api/visit` stays unchanged: returns `{"count": N}` from the in-memory
  atomic. The system-tray badge fetches it on page load to display the live
  count.

#### 3. Archive click endpoints

- `GET /go/<id>`:
  - Look up `id` in the same archive registry (mirrored in Go — see below).
  - If unknown → `404`.
  - Fire async goroutine: `UPDATE archive_click_count SET n = n + 1 WHERE id = ?`,
    then bump in-memory cache.
  - `302` redirect to the archive's URL with `Cache-Control: no-store`.
- `GET /api/archive-clicks`:
  - Returns `{"counts": {"wiki": 1204, "pdf": 87, ...}}` from the in-memory
    map. JSON, `Cache-Control: no-store`.

#### 4. Archive registry on the server

Add a small static slice in `main.go` mirroring the JS `ARCHIVES` array:

```go
var archives = []struct {
    ID, URL string
}{
    {"wiki",     "https://wiki.kunaldawn.com"},
    {"pdf",      "https://pdf.kunaldawn.com"},
    {"os",       "https://os.kunaldawn.com"},
    {"iso",      "https://iso.kunaldawn.com"},
    {"chiptune", "https://chiptune.kunaldawn.com"},
    {"tube",     "https://tube.kunaldawn.com"},
    {"audio",    "https://audio.kunaldawn.com"},
}
```

Built into a `map[string]string` at startup for O(1) `/go/<id>` lookup. This
duplicates the JS registry, but the alternative (a JSON file shared by both
sides) is overkill for seven entries that change rarely.

#### 5. CSP change

Current `frame-src` lists only the seven archive subdomains. The internal
browser iframe will now load `/go/<id>` (same origin) and follow a 302 to the
archive. Add `'self'` to `frame-src` so same-origin iframe load is allowed
before the redirect.

```
frame-src 'self' https://wiki.kunaldawn.com ... https://audio.kunaldawn.com;
```

This is a minor widening — nothing else on the site loads same-origin
iframes today, so no new attack surface in practice.

### Client changes (`static/index.html`)

#### 1. Visit counter fetch becomes GET

`static/index.html:5976-5988` — change:

```js
fetch('/api/visit', { method: 'POST' })
```

to:

```js
fetch('/api/visit')
```

Display logic and the `kd-visit-count` event for the cat are unchanged.

#### 2. Card hrefs point at tracked redirect

For each of the seven Featured Archive cards in `static/index.html:5558-5591`:

- `href="https://wiki.kunaldawn.com"` → `href="/go/wiki"` (and equivalents).
- `rel="noopener"` stays.
- `onclick="event.preventDefault(); openFeaturedArchive('wiki');"` stays.
- The href change is what makes middle-click, Ctrl-click, and right-click →
  "open in new tab" all flow through `/go/<id>` and get counted.

#### 3. `openFeaturedArchive(id)` opens the redirect URL

In `static/index.html:8553-8583`:

- New-tab mode: `window.open('/go/' + id, '_blank', 'noopener')` instead of
  `window.open(entry.url, ...)`.
- Internal-browser mode: the iframe `src` becomes `/go/<id>` (handled inside
  `loadIframe(inst)` — needs a tweak to that function or to `entry`). The
  iframe loads same-origin `/go/<id>`, server returns 302, browser follows to
  the archive URL.
- The "Open in new tab" toolbar button on the internal browser keeps using
  `inst.entry.url` directly (no need to count a tab open from a window already
  open — we already counted the original click that opened the window).

#### 4. Click count badge on each card

CSS:

```css
.card h4 {
  display: flex;
  align-items: center;
  gap: 8px;
}
.card h4 .card-title {
  flex: 1; /* pushes count to the right */
}
.card-count {
  color: #7fd1b3;
  font-family: 'Share Tech Mono', monospace;
  font-size: 11px;
  white-space: nowrap;
  opacity: 0.85;
}
```

HTML structure inside each card's `<h4>` becomes:

```html
<h4>
  <span class="card-icon">[svg]</span>
  <span class="card-title">Wiki Archive</span>
  <span class="card-count" data-archive-id="wiki">—</span>
</h4>
```

Bootstrap JS (a new IIFE near the visit-counter one):

```js
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
  .catch(() => { /* silent */ });
```

Optimistic increment in `openFeaturedArchive(id)`:

```js
var badge = document.querySelector('.card-count[data-archive-id="' + id + '"]');
if (badge) {
  var current = parseInt(badge.textContent.replace(/[^0-9]/g, ''), 10) || 0;
  badge.textContent = (current + 1).toLocaleString() + ' opens';
}
```

Optimistic update applies only to clicks that go through the JS handler — i.e.
left-click on a card or start-menu item. Middle-click / Ctrl-click / new-tab
opens skip the JS but still get counted server-side and reflected on next
reload. That divergence is acceptable.

## Verification

After implementation, run the site under `docker compose up -d` and use
Playwright (via the `playwright` MCP server) to verify:

1. Visit counter increments on plain page load (compare `/api/visit` before
   and after a fresh GET to `/`).
2. Visit counter increments on a 304 conditional GET (load the page, reload
   with `If-None-Match`, confirm count rose).
3. Card click on the home view triggers the optimistic UI bump and bumps the
   server-side count for the matching id.
4. Start-menu archive item click bumps the same id's server-side count.
5. Each archive card displays a non-empty `.card-count` badge after page load.

Capture screenshots of:
- The home view showing all seven cards with click counts visible.
- The system-tray visit counter with a non-`--` value.
- The start menu open with all seven archive items.

## Files Touched

- `main.go` — visit counter trigger, archive table init, `/go/<id>` handler,
  `/api/archive-clicks` handler, registry, CSP `frame-src` `'self'`.
- `static/index.html` — visit fetch verb, card hrefs, card `<h4>` structure,
  `.card-count` CSS, archive-counts bootstrap, `openFeaturedArchive` redirect
  URLs + optimistic update.

No changes needed to `Dockerfile`, `docker-compose.yml`, `entrypoint.sh`, or
the static asset cache build — the in-memory cache already serves the
modified `index.html` correctly after a rebuild.

## Open questions

None — design choices were locked during brainstorming
(see preceding conversation):

- Bot filtering: count everything.
- Click counter location: inline on right of `<h4>`.
- Start menu badges: cards only.
- Click trigger: server-side tracked redirect.
- UI refresh after click: optimistic JS increment.
