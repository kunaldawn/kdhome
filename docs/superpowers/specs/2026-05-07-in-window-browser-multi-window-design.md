# In-window browser — multi-window rewrite & bug audit

**Date:** 2026-05-07
**Status:** Spec, awaiting plan
**Scope:** `static/index.html` only

## Context

The site embeds an "in-window browser" used to open the seven featured archives
(Wiki, PDF, OS, CD/DVD, Chiptune, Tube, Audiobook) inside a faux-OS desktop
window when the user opts into `kd:prefs:archives-internal-browser`. Today the
implementation is a singleton: one `<div id="browser-window">`, one taskbar
slot, one shared state bag. The user wants:

1. **Multi-window**, but **one window per archive** — clicking "Wiki" twice
   focuses the existing Wiki window rather than spawning a duplicate.
2. **A working address-bar story** that does not depend on every archive
   opting into a `postMessage` broadcast (the current approach), since the
   archives live on cross-origin subdomains and not all of them carry the
   broadcaster shim.

A deep audit also surfaced ~20 other bugs and rough edges in the same
subsystem; this spec resolves all of them in one rewrite.

## Bug catalog (audit findings)

Severity legend: **Critical** = breaks user-facing feature · **Major** =
visible misbehavior · **Minor** = code smell · **Security** = exploitable.

| # | Sev | Summary |
|---|---|---|
| A1 | Critical | Single `<div id="browser-window">` and `#browser-taskbar`; opening a second archive destroys the first iframe (scroll, sub-page, media all lost). Module-scope state is shared, baking the singleton into JS. |
| A2 | Critical | Address bar can never reflect cross-origin navigation. `postMessage('kd-iframe-loc')` requires per-archive opt-in (not all archives have it). The same-origin `iframe.contentWindow.location.href` fallback throws on `*.kunaldawn.com` from a different host and is silently swallowed. The address bar is frozen at the entry URL, and "Open in new tab" likewise opens only the entry URL. |
| B1 | Major | `recenter()` clears inline styles but doesn't strip the `.dragged` class. After drag → close → reopen, the window appears with its left edge at viewport center because `transform: none` from `.dragged` defeats the base `translateX(-50%)`. (Same bug in demo-window.) |
| B2 | Major | Close-animation transform `(win.style.transform || 'translateX(-50%)') + ' scale(0.95)'` produces `'none scale(0.95)'` when dragged — invalid CSS, browser drops the entire transform. Same bug in demo-window. |
| B3 | Major | `prevStyles` for maximize is captured once and never reset across sessions — un-maximize after close+reopen restores from stale coordinates. |
| B4 | Major | Back button can only undo top-level archive switches because cross-origin navs never make it onto `historyStack`. Misleading. |
| B5 | Major | No reload, no forward. |
| B6 | Major | Address bar input is `readonly` and inert — looks like a real address bar, doesn't navigate on Enter. |
| B7 | Major | Mobile re-maximizes after the user manually un-maximized: minimize → restore re-applies the auto-max (lines 8418–8421, 8496–8498). |
| B8 | Major | Iframe is rebuilt on every history step (Back), wiping scroll & state inside the page. |
| B9 | Major | Cat avoidance hard-codes the `browser-window` element id, so with multi-window only one of N windows would be avoided. |
| B10 | Minor | `closeBrowserWindow` doesn't reset `currentLabel`, `prevStyles`, or `__focusedWindow`. Stale references leak across sessions. |
| B11 | Minor | `historyStack` has no cap; a misbehaving iframe pumping `kd-iframe-loc` could grow it unboundedly. |
| B12 | Minor | `homeDoc()` rebuilt on every render — minor perf, not a bug. |
| B13 | Minor | `homeDoc()` start cards do no HTML escaping; safe today but fragile for future contributors. |
| B14 | Security | `browser-nav` postMessage handler does not validate origin or scheme. A sandboxed iframe can post `{type:'browser-nav', url:'javascript:…'}` and the parent will hand it to `iframe.src`. |
| B15 | Minor | Dead code: `cfErrorDoc()` (~70 lines) is defined but never called. |
| B16 | Minor | `bringToFront` is binary — sets `1005` and clears every other window's z-index. With multi-window we want a real stacking history. |
| B17 | Minor | `recenter()` doesn't reset `isMax`; today `showWindow()` masks this, but the helper is brittle. |
| B18 | Minor | Iframe-focus heuristic uses `setTimeout(0)` + `document.activeElement`, which is unreliable on Safari. |
| B19 | Minor | No loading indicator while a heavy archive iframe boots. |
| B20 | Minor | No keyboard shortcuts (Esc, Ctrl+L, Ctrl+R, etc.) — out of scope for this rewrite. |

## Goals

- One window per archive; clicking the same archive again focuses the existing
  window. Up to 7 archive windows can coexist.
- Address bar replaced with a static, always-correct title showing
  `"<label> — <hostname>"` for each window.
- Toolbar reduces to **Reload · Open in new tab**. Back, forward, home, and
  the about:home start page are removed.
- All 20 catalogued bugs resolved.
- No external dependencies; pure rewrite of one IIFE in `static/index.html`.

## Non-goals

- Cross-origin URL tracking. Browsers don't allow it; we accept the limit and
  stop pretending otherwise.
- In-archive back/forward/history. Without URL visibility, these can't work
  honestly.
- Keyboard shortcuts (B20). Reserved for a follow-up if useful.
- Server-side reverse proxy that would make sub-archives same-origin. Too
  much bandwidth/tunnel cost for the gain.
- Touching `main.go`, the music window, or demo window beyond piggybacking on
  the new monotonic z-index helper.
- Touching cat code beyond the four helpers named in the "Cat avoidance
  retrofit" section (`getAllAvoidRects`, `hasMaximizedWindow`,
  `windowVisible`, `startMusicAvoidWatcher`).

## Architecture

### Archive registry (single source of truth)

A `const ARCHIVES` array, defined once near the top of the new IIFE:

```js
const ARCHIVES = [
  { id: 'wiki',     url: 'https://wiki.kunaldawn.com',     label: 'Wiki Archive',      iconSvg: '<svg ...>' },
  { id: 'pdf',      url: 'https://pdf.kunaldawn.com',      label: 'PDF Archive',       iconSvg: '<svg ...>' },
  { id: 'os',       url: 'https://os.kunaldawn.com',       label: 'OS Archive',        iconSvg: '<svg ...>' },
  { id: 'iso',      url: 'https://iso.kunaldawn.com',      label: 'CD/DVD Archive',    iconSvg: '<svg ...>' },
  { id: 'chiptune', url: 'https://chiptune.kunaldawn.com', label: 'Chiptune Archive',  iconSvg: '<svg ...>' },
  { id: 'tube',     url: 'https://tube.kunaldawn.com',     label: 'Tube Archive',      iconSvg: '<svg ...>' },
  { id: 'audio',    url: 'https://audio.kunaldawn.com',    label: 'Audiobook Archive', iconSvg: '<svg ...>' },
];
const REGISTRY = Object.fromEntries(ARCHIVES.map(a => [a.id, { ...a, hostname: new URL(a.url).host }]));
```

Inline `onclick` handlers in the start menu (lines 5243–5269) and the homepage
featured cards (5501–5531) become `onclick="openFeaturedArchive('wiki')"`,
etc. The 14 places where `(url, label)` could drift collapse to one
declaration.

### `BrowserWindowManager`

Replaces the existing browser-window IIFE (8188–8577).

```
class WindowInstance {
  id            // archive id
  el            // cloned .browser-window node
  taskbarEl     // paired taskbar item
  iframe        // <iframe> currently inside .browser-content
  isMaximized   // bool
  isMinimized   // bool
  prevStyles    // captured at maximize time
  userUnmaxed   // bool — set when the user explicitly un-maximizes; suppresses mobile auto-max on next restore
}

const instances = new Map(); // archiveId -> WindowInstance
let nextZ = 1005;            // monotonic z-index counter
let cascadeIndex = 0;        // resets when instances.size === 0
```

#### Template

A `<template id="browser-window-template">` near the bottom of `<body>`
contains the markup that today lives at lines 5382–5404, minus the
address-bar input and minus the Back / Home buttons. Toolbar inside the
template is:

```html
<div class="browser-toolbar">
  <button class="browser-btn browser-reload" title="Reload">
    <svg>...</svg>
  </button>
  <span class="browser-title-strip"></span>  <!-- "<label> — <hostname>", flex-grows -->
  <button class="browser-btn browser-external" title="Open in new tab">
    <svg>...</svg>
  </button>
</div>
```

The titlebar `<span class="vault-titlebar-title">` continues to show
`browser@kd:~/<label>` for visual consistency with other windows.

#### `openFeaturedArchive(id)`

```
1. Check instances.get(id).
   - If exists and minimized: restore + bringToFront + return.
   - If exists and visible: bringToFront + return.
2. Otherwise create:
   a. Clone template, set data-archive-id=id, attach to body.
   b. Cascade-position: if cascadeIndex > 0, set inline left/top with
      offsets (cascadeIndex % 5) * 30px relative to the centered base,
      and add the .dragged class so CSS doesn't fight us. Increment
      cascadeIndex.
   c. Set the title text and toolbar title-strip text from REGISTRY[id].
   d. Create iframe with src = REGISTRY[id].url; append to .browser-content.
      Add a .loading class on .browser-content; remove on iframe.load
      (with a 10s safety timeout).
   e. Wire up close / minimize / maximize / drag / dblclick-titlebar /
      reload / external on this instance's element.
   f. Create paired taskbar item, append to taskbar-items container.
   g. instances.set(id, inst); bringToFront(inst.el).
   h. On mobile (matchMedia max-width 800px): auto-maximize once.
   i. If start menu is open, close it.
```

`window.openFeaturedArchive(id)` becomes the only public entry point. The
existing prefs check (`kd:prefs:archives-internal-browser`) stays at the call
site: if off, fall back to `window.open(REGISTRY[id].url, '_blank', 'noopener')`.

#### Per-window controls

- **Close**: animate (CSS class `.closing` driving opacity + scale, see
  "CSS additions" below — fixes B2), then `instance.el.remove()`,
  `instance.taskbarEl.remove()`, `instances.delete(id)`. If the map empties,
  reset `cascadeIndex = 0`.
- **Minimize**: add `.minimizing` class + `.minimized` to taskbar item;
  after animation, set `display: none`. Don't destroy.
- **Maximize**: capture `prevStyles` from inline styles, add `.maximized`
  class. Un-maximize: clear class, restore captured styles, set
  `userUnmaxed = true` so the next restore on mobile won't auto-re-maximize
  (fixes B7).
- **Drag**: existing `makeDraggable(el, titlebar)` works as-is — instance is
  the element, no shared state.
- **Reload**: append a cache-buster (`?_kd_reload=Date.now()`) to the entry
  URL on each click and reassign `iframe.src`. Add `.loading` class to
  `.browser-content`; remove on `iframe.load` or 10s timeout. Disable the
  button while loading.
- **Open external**: `window.open(REGISTRY[id].url, '_blank', 'noopener')`.

#### Taskbar items

Created/destroyed alongside their window. Markup pattern matches the existing
`.taskbar-item`. Click delegates to a single
`toggleArchiveWindow(id)` that mirrors the existing toggle semantics
(restore-from-min, focus-if-not-focused, minimize-if-focused).

CSS:
- `max-width: 110px; text-overflow: ellipsis; overflow: hidden`.
- At `(max-width: 600px)` the label text hides; icon stays; full label
  remains in `aria-label` and `title`.

### Focus stacking

Replace the binary `bringToFront` (7670–7677) with a counter:

```js
let nextZ = 1005;
window.bringToFront = function(el) {
  if (!el) return;
  el.style.zIndex = String(++nextZ);
  window.__focusedWindow = el;
};
```

Music and demo windows already call this helper; behavior for them improves
(real stacking) without changes.

The iframe-focus heuristic (7693–7701) is kept but made multi-window-aware:

```js
window.addEventListener('blur', function() {
  setTimeout(function() {
    const ae = document.activeElement;
    if (ae && ae.tagName === 'IFRAME') {
      const win = ae.closest('.browser-window, .music-window, .demo-window, .vault');
      if (win) window.bringToFront(win);
    }
  }, 0);
});
```

### Cat avoidance retrofit

In `cat-window-watcher` and avoidance helpers (lines 6760–6960):

- `getAllAvoidRects()`: replace the fixed id list `['music-window',
  'browser-window', 'demo-window']` with
  `[document.getElementById('music-window'), document.getElementById('demo-window'),
   ...document.querySelectorAll('.browser-window')]`.
- `hasMaximizedWindow()`: same change — accumulate from all `.browser-window`
  matches.
- `windowVisible('browser-window')`: this is used by `getNextState()` to bias
  the cat's behavior toward "reading"-style states when an archive is open.
  Change to: `document.querySelector('.browser-window:not(.hidden)')` is
  truthy iff *any* archive window is open (and not display:none, mirroring
  current logic).
- `startMusicAvoidWatcher()`: the current code observes a fixed list of
  elements at startup. With dynamic `.browser-window` nodes it would miss
  new ones. Two options, picking the simpler:

  Add a top-level `MutationObserver` on `document.body` watching `childList`.
  When a `.browser-window` is added, attach the per-element style/class
  observer to it and trigger a recheck. When removed, no-op (the per-element
  observer is GC'd with the node).

  As a defense against any missed mutation, add a 5-second interval that
  calls `recheck()` while any archive window is open. Cheap and bounded.

### Cascade math

```js
function cascadePosition(el) {
  if (cascadeIndex === 0) {
    cascadeIndex = 1;
    return; // first window uses default centered CSS
  }
  const offset = (cascadeIndex % 5) * 30;
  // Base = 40px from top, viewport-center horizontally minus half of the
  // window's typical width (we don't have a measured rect yet, so we
  // approximate by switching to dragged-mode positioning relative to
  // viewport center).
  const baseLeft = (window.innerWidth - Math.min(1100, window.innerWidth * 0.92)) / 2;
  const baseTop = 40;
  el.style.position = 'fixed';
  el.style.left = (baseLeft + offset) + 'px';
  el.style.top = (baseTop + offset) + 'px';
  el.style.transform = 'none';
  el.classList.add('dragged');
  cascadeIndex++;
}
```

`clampWindowBounds` (existing helper at 7642) is still wired into the
resize listener and will keep cascaded windows on screen.

### Mobile behavior

On `(max-width: 800px)` viewports, every newly-opened archive window
auto-maximizes once on creation (current behavior). Multiple windows can
exist; only the focused (z-topped) maximized window is visible at any time.
The taskbar is how the user switches between them. `userUnmaxed` is a
per-instance flag set the first time the user explicitly un-maximizes; once
set, restoring from minimized never re-applies auto-max for that window.
Closing the window discards the flag along with the rest of the instance.

### Removals

In `static/index.html`:

- `homeDoc()` (8314–8362) — entire about:home start page.
- `cfErrorDoc()` (8242–8312) — dead code.
- The `'browser-nav'` postMessage handler (8540–8543) — closes B14.
- The `'kd-iframe-loc'` postMessage handler (8544–8555) — was only useful
  with the cross-origin URL display, which is gone.
- The same-origin `load`-event URL-read fallback (8559–8569).
- Address-bar `<input>` markup (5398).
- Back / Home button markup + handlers (5392–5397, plus
  `historyStack`/`historyIndex`/`pushHistory`/`updateBackBtn`/`browserGoBack`/
  `browserGoHome`).
- The dragged-window `recenter()` helper (8391–8403) — fresh clones don't
  need it.

### CSS additions

- `.browser-window.closing { animation: closeFade 0.3s ease-out forwards; }`
  with a keyframe that handles the opacity+scale transition without inline
  transform concatenation. Fixes B2; same class added to `.demo-window`.
- `.browser-content.loading::after` — a centered spinner overlay shown
  while the iframe is loading. Fades out on `iframe.load`.
- `.taskbar-item.archive-window` — adds `max-width: 130px` with ellipsis
  truncation, plus the `(max-width: 600px)` icon-only collapse.
- `.browser-toolbar` — adjust to flex layout with the title strip in the
  middle, since the address-bar input is gone.

## Bug-by-bug crosswalk

| # | Resolution |
|---|---|
| A1 | Multi-window via cloned templates + `Map<archiveId, instance>` |
| A2 | Address bar removed; toolbar shows static `"<label> — <hostname>"` |
| B1 | No `recenter()`; fresh clones each open |
| B2 | `.closing` CSS class drives the close animation; no inline transform concat |
| B3 | Per-instance `prevStyles`; instance destroyed on close |
| B4 | Back button removed |
| B5 | Reload added; forward intentionally absent (no history) |
| B6 | Address bar removed |
| B7 | `userUnmaxed` flag suppresses auto-max on restore |
| B8 | History gone; reload is the only intentional iframe re-assignment |
| B9 | Cat avoidance switches to `querySelectorAll('.browser-window')` + body MutationObserver |
| B10 | Per-instance state deleted on close |
| B11 | History gone |
| B12 | `homeDoc` removed |
| B13 | `homeDoc` removed |
| B14 | `browser-nav` handler removed entirely |
| B15 | `cfErrorDoc` removed |
| B16 | Monotonic `nextZ` counter |
| B17 | `recenter` removed |
| B18 | Heuristic kept, multi-window-aware via `closest('.browser-window')` |
| B19 | `.loading` class with spinner during iframe load |
| B20 | Out of scope |

## Files touched

Single file: `static/index.html`.

Estimated diff:
- **Removed** ~250 lines: `homeDoc()`, `cfErrorDoc()`, Back/Home markup +
  handlers, address-bar markup + URL-update postMessage handlers + same-origin
  fallback + history system, `recenter()`, 14 inline `(url, label)` strings
  collapsed.
- **Added** ~300 lines: `BrowserWindowManager` IIFE with registry, instance
  map, cascade positioning, per-instance lifecycle, taskbar-item lifecycle,
  reload-with-spinner, multi-window-aware focus stacking; updated
  cat-avoidance helpers; `<template>`; CSS additions.
- **Net**: roughly +50 lines.

No changes to: `main.go`, music window, demo window (only the `bringToFront`
helper is replaced — they call into it but don't define it), CSS for existing
window chrome.

## Risks & rollback

- **Cat avoidance regression** if MutationObserver retrofit misses a state
  change. *Mitigation*: 5-second reconciliation interval re-reading rects
  while any archive window is open.
- **z-index counter**: at 1000 focuses/sec it'd take ~280k years to overflow.
  Non-issue.
- **`iframe.src = iframe.src` reload** can be cache-served by upstream.
  *Mitigation*: append `?_kd_reload=<ts>` cache-buster on Reload.
- **Rollback**: single HTML file in a git repo with frequent commits.
  `git revert` the implementation commit; no DB/migration concerns.

## Open questions

None — all design choices confirmed during brainstorming.
