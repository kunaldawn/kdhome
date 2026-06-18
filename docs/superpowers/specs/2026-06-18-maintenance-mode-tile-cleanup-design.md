# Design: Maintenance Mode, Tile Cleanup & Auto-Hide of Down Archives

**Date:** 2026-06-18
**Status:** Approved

Three independent features bundled into one change:

1. Env-configurable **maintenance mode** with a "coming back soon" page and a live countdown to a user-set end time.
2. **Remove the per-tile "opens" counter** from the Featured Archives — including the whole backend click-tracking subsystem.
3. **Auto-hide a featured archive** (tile + Start-menu entry) when it has been down for more than an hour, and restore it once it has been back up for a minute.

---

## Feature 1 — Maintenance Mode

### Goal

An operator can flip the whole site into a maintenance page purely via environment variables, with no code change or redeploy of new binaries — matching how `PORT` and `DATA_DIR` are already configured.

### Configuration (env vars, read once at startup)

| Var | Meaning | Default |
|-----|---------|---------|
| `MAINTENANCE_MODE` | Truthy (`1`, `true`, `on`, `yes`, case-insensitive) enables maintenance mode. Anything else / unset = off. | off |
| `MAINTENANCE_END` | RFC3339 timestamp (e.g. `2026-06-20T15:00:00Z`) the countdown ticks down to. Optional. If unset or unparseable, the page renders with no countdown (message only) and a warning is logged. | none |
| `MAINTENANCE_MESSAGE` | Custom body text shown under the heading. | `We'll be back soon.` |

### Behavior

- A new file `maintenance.go` holds all maintenance logic.
- At startup `main()` reads the env vars. If `MAINTENANCE_MODE` is off, **nothing is installed** — zero request-path overhead, normal behavior.
- If on, the maintenance page HTML is built **once** at startup (env values injected) and a middleware wraps the entire handler chain.
- The middleware short-circuits **every** request (all paths, including `/api/*` and static assets) with **HTTP 503 Service Unavailable** and a `Retry-After` header (seconds until `MAINTENANCE_END`, or a fixed fallback like 3600 if no end time). Browsers render the 503 body, so the themed page still shows.
- `Cache-Control: no-store` so the page is never cached and the site comes back immediately when the flag is flipped off.

### Maintenance page

- Self-contained single HTML document — inline CSS and inline JS, no external assets (since assets are also blocked by the middleware).
- Themed to match the late-'90s desktop / NFO-terminal aesthetic of the main site (dark background, monospace, accent color in the existing palette).
- Contents: a heading ("Under Maintenance" / similar), the configured message, and — when `MAINTENANCE_END` is set — a live JS countdown (days / hours / minutes / seconds) ticking down to the end time. The end time is injected into the page as an ISO string; the JS computes the remaining time client-side so it stays correct regardless of client clock skew relative to render time.
- When the countdown reaches zero it switches to a "back any moment now…" message rather than going negative. It does **not** auto-reload (the operator flips the env flag off to restore the site).

### Components / interfaces

- `maintenanceConfig` struct: `{ enabled bool; end time.Time; hasEnd bool; message string }`.
- `loadMaintenanceConfig() maintenanceConfig` — reads + parses env.
- `buildMaintenancePage(cfg) []byte` — renders the HTML once.
- `maintenanceMiddleware(page []byte, cfg) func(http.Handler) http.Handler` — wraps the chain; serves the page on every request.
- Wired in `main()`: `handler := securityHeaders(mux)`; if maintenance enabled, `handler = maintenanceMiddleware(page, cfg)(handler)` (outermost, so it intercepts before anything else).

---

## Feature 2 — Remove the "opens" counter and click tracking

The per-tile "N opens" badge and its entire backing subsystem are removed.

### Backend (`main.go`)

Delete:

- `archiveClicks` map, `archiveClicksMu` mutex.
- `archiveID` map.
- `recordArchiveClick()`.
- `archiveClicksHandler()`.
- The `mux.HandleFunc("/api/archive-clicks", …)` route registration.
- In `initVisitDB`: the `archive_click_count` `CREATE TABLE`, the seed loop, the `archiveID` population loop, and the cache-hydration block + its `[CLICKS]` logging.

Keep:

- The `archives` slice — still used by the status probe and `probe_daily`.

Migration note: the `archive_click_count` table is simply left untouched on existing databases (no `DROP`); it just stops being read or written. Harmless and avoids a destructive migration.

### Frontend (`static/index.html`)

Delete:

- The `<span class="card-count" data-archive-id="…">—</span>` element from each of the 7 Featured Archive tiles (the `data-archive-id` moves up to the card anchor — see Feature 3).
- The `.card-count { … }` CSS rule.
- The load-time `fetch('/api/archive-clicks')` IIFE that populates the badges.
- `bumpCardCount()` and `trackArchiveClick()` functions and their calls inside `openFeaturedArchive()`.

`openFeaturedArchive()` keeps its open-in-tab / open-in-internal-browser logic, just without the click bump/track calls.

---

## Feature 3 — Auto-hide archives down > 1 hour (tiles + Start menu)

### Goal

If an archive subdomain stays down long enough that linking to it just frustrates visitors, drop it from the featured tiles and Start-menu list automatically; bring it back shortly after it recovers. Single source of truth on the server.

### Backend (`status.go`)

Thresholds as named consts:

```go
const (
    hideThreshold = 1 * time.Hour    // down this long → hidden
    recoverWindow = 1 * time.Minute  // up this long after being hidden → shown
)
```

Extend `probeState`:

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

A **pure, time-injectable** helper keeps the transition logic unit-testable:

```go
// updateVisibility folds the current (debounced) effectiveOK into the
// hide/restore state machine using an explicit `now` for testability.
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
```

`debounceProbe` calls `updateVisibility(st, time.Now())` after computing `st.effectiveOK`, and its existing return contract (returns `effectiveOK`) is unchanged so current tests keep passing.

The "down since" clock starts from the **debounced** down moment (i.e. when `effectiveOK` flips false, after the existing 5-failure grace), which is the same moment the status window reports the archive offline — consistent and intuitive.

A small accessor reads the hidden flag for the snapshot:

```go
func isHidden(id string) bool {
    probeStateMu.Lock()
    defer probeStateMu.Unlock()
    st := probeStates[id]
    return st != nil && st.hidden
}
```

`probeOne` records `Hidden` into its `probeResult` (new field `Hidden bool`), and `statusJSONHandler` emits `"hidden":<bool>` for each archive in `/api/status.json`.

### Frontend (`static/index.html`)

- Add `data-archive-id="<id>"` to each `<a class="card">` anchor (7 tiles) and to each Start-menu archive `<div class="menu-item">` (7 entries).
- Add a lightweight **home-view poller** (runs on page load, independent of the status window): fetch `/api/status.json` immediately and then every 60 s. For each archive in the response, toggle `display:none` on `[data-archive-id="<id>"]` elements (cards + menu items) based on `hidden`.
- **Fail-open:** if the fetch fails or returns no `archives`, leave all elements visible. Never hide on error.
- Edge case: if every archive is hidden the Featured Archives grid is empty — acceptable (transient; recovers automatically). No special "all down" placeholder in this iteration.

---

## Testing

- **`status_test.go`** — new `TestUpdateVisibility` driving `updateVisibility` with a controlled `now`:
  - Down < 1h → not hidden; crossing 1h → hidden.
  - Hidden, then up < 1m → still hidden; up ≥ 1m → shown.
  - Flap (down a while, brief up, down again) resets the down clock so it doesn't prematurely hide/show.
  - Existing `TestDebounceProbe` must still pass unchanged.
- **`main_test.go`** — no archive-clicks tests exist today, so removal needs no test deletion; verify the suite still builds and passes after the handler/route removal.
- **Manual / smoke:**
  - Maintenance off (default): site serves normally.
  - `MAINTENANCE_MODE=on MAINTENANCE_END=<future> MAINTENANCE_MESSAGE="…"`: every route returns 503 with the themed countdown page; flipping the env off restores the site.
  - Tiles no longer show "opens"; `/api/archive-clicks` returns 404 (route gone).
  - Simulating a `hidden:true` in `/api/status.json` hides the matching tile + menu item; back to `false` restores it.

## Out of scope

- No admin UI for maintenance mode — env vars only.
- No persistence of `hidden` / down-duration across process restarts (in-memory, matching the existing probe state).
- No "all archives down" empty-state placeholder.
- The legacy `archive_click_count` table is left in place (not dropped).
