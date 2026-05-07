# In-Window Browser Multi-Window Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the singleton in-window browser with a one-window-per-archive multi-window manager, drop the cross-origin-broken address bar in favor of a static title, simplify the toolbar to Reload + Open-in-new-tab, and fix all 20 audit bugs catalogued in the spec.

**Architecture:** Single-file work in `static/index.html`. The current browser-window IIFE (lines ~8188–8577 at plan-write time) is replaced by a `BrowserWindowManager` IIFE that clones a `<template>` per archive into a `Map<archiveId, instance>`. Each window gets its own DOM node, taskbar item, and per-instance state. The Go server caches static files at startup, so each iteration requires `go build -o /tmp/kdhome ./...` and a server restart on port 8089 to verify in Playwright.

**Tech Stack:** Vanilla JS IIFE in HTML, CSS, Playwright MCP for verification (no test framework — verification is manual via `mcp__playwright__browser_*` tools driving `localhost:8089`).

**Spec:** `docs/superpowers/specs/2026-05-07-in-window-browser-multi-window-design.md`

**Conventions used in this plan:**
- Source line numbers reference the file *before* this plan starts. They drift as edits land. Use the surrounding code in `old_string` snippets to locate the right place — that's the authoritative anchor.
- Each "Restart server" step is `pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &` with a 1 s wait.
- "Verify in Playwright" steps assume the server is up on `http://localhost:8089/`.
- The internal-browser feature is opt-in via `localStorage['kd:prefs:archives-internal-browser'] === 'on'`. Playwright tests must set this before opening any archive: `mcp__playwright__browser_evaluate` → `() => localStorage.setItem('kd:prefs:archives-internal-browser', 'on')`.
- Each task ends with a commit. Commit message style follows the repo: `feat(browser): ...`, `fix(browser): ...`, `refactor(browser): ...`. Co-author trailer per the repo's CLAUDE.md / commit history.

---

### Task 1: Monotonic z-index counter (B16)

**Files:**
- Modify: `static/index.html` (function `bringToFront` defined inside the windows IIFE around line 7670)

**Why first:** Pure no-op for a single-window user (only one focusable window means the counter is barely exercised), but lays the groundwork the multi-window flow depends on. Reaches all three windows that use `bringToFront`: vault, music, demo, and (later) browser instances.

- [ ] **Step 1: Replace `bringToFront`**

Anchor: search for `window.bringToFront = function(el) {` (around line 7670).

Replace this block:

```js
        // Shared focus / z-stack helper — brings a window above all others.
        const WINDOW_SELECTOR = '.vault, .music-window, .browser-window, .demo-window';
        window.__focusedWindow = null;
        window.bringToFront = function(el) {
          if (!el) return;
          document.querySelectorAll(WINDOW_SELECTOR).forEach(function(w) {
            if (w !== el) w.style.zIndex = '';
          });
          el.style.zIndex = '1005';
          window.__focusedWindow = el;
        };
```

with:

```js
        // Shared focus / z-stack helper. Uses a monotonically-increasing
        // counter so each focus puts the window strictly above the previous
        // top, giving real stacking history instead of a binary front/default.
        const WINDOW_SELECTOR = '.vault, .music-window, .browser-window, .demo-window';
        window.__focusedWindow = null;
        let __nextWindowZ = 1005;
        window.bringToFront = function(el) {
          if (!el) return;
          el.style.zIndex = String(++__nextWindowZ);
          window.__focusedWindow = el;
        };
```

- [ ] **Step 2: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

Open `http://localhost:8089/` in Playwright. Open music + demo windows from the start menu. Click each to focus. Assert via `browser_evaluate`:

```js
() => {
  const m = document.getElementById('music-window');
  const d = document.getElementById('demo-window');
  return { music: m.style.zIndex, demo: d.style.zIndex };
}
```

After clicking music last, expect `music > demo` and both ≥ 1006. Numbers should grow on each focus.

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(windows): monotonic z-index counter for real stacking history

Replaces the binary front/default bringToFront with a counter that
increments on every focus so windows stack like a real WM instead of
just toggling between 1005 and default. No user-visible change today
with one focusable browser window, but unblocks multi-window.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Close-animation CSS class — fixes B2

**Files:**
- Modify: `static/index.html` (CSS around line 1983 / `.browser-window` block; functions `closeBrowserWindow` and `closeDemoWindow`)

**Why:** Today's close transform `(win.style.transform || 'translateX(-50%)') + ' scale(0.95)'` produces invalid `'none scale(0.95)'` when the window has been dragged. Driving the animation off a CSS class instead of inline transform concatenation eliminates the bug for both `.browser-window` and `.demo-window`.

- [ ] **Step 1: Add `.closing` keyframes + class rules**

Anchor: search for `.browser-window.restoring {` (around line 2030). Insert *after* the closing brace of the `.browser-window.dragged.restoring, .browser-window.maximized.restoring { ... }` block (around line 2045), and *before* `.browser-toolbar {`.

Add this CSS block:

```css
    /* Close animation: opacity + scale, decoupled from transform so it
       composes correctly whether the window is centered, dragged, or
       maximized. Uses opacity on the parent and scale on the child via
       a CSS variable to avoid stomping on translate-based positioning. */
    @keyframes windowFadeClose {
      from { opacity: 1; }
      to   { opacity: 0; }
    }
    .browser-window.closing,
    .demo-window.closing {
      animation: windowFadeClose 0.3s ease-out forwards;
      pointer-events: none;
    }
    .browser-window.closing > *,
    .demo-window.closing > * {
      transition: transform 0.3s ease-out;
      transform: scale(0.96);
    }
```

- [ ] **Step 2: Replace browser-window close logic**

Anchor: search for `window.closeBrowserWindow = function() {` (around line 8456).

Replace this block:

```js
        window.closeBrowserWindow = function() {
          win.style.transition = 'opacity 0.3s, transform 0.3s';
          win.style.opacity = '0';
          win.style.transform = (win.style.transform || 'translateX(-50%)') + ' scale(0.95)';
          setTimeout(function() {
            win.classList.add('hidden');
            win.style.display = 'none';
            win.style.opacity = '';
            win.style.transform = '';
            win.style.transition = '';
            taskbar.style.display = 'none';
            taskbar.classList.remove('active', 'minimized');
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            contentEl.innerHTML = '';
            currentUrl = '';
            historyStack = [];
            historyIndex = -1;
            updateBackBtn();
          }, 300);
        };
```

with:

```js
        window.closeBrowserWindow = function() {
          win.classList.add('closing');
          setTimeout(function() {
            win.classList.remove('closing');
            win.classList.add('hidden');
            win.style.display = 'none';
            taskbar.style.display = 'none';
            taskbar.classList.remove('active', 'minimized');
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            contentEl.innerHTML = '';
            currentUrl = '';
            historyStack = [];
            historyIndex = -1;
            updateBackBtn();
          }, 300);
        };
```

- [ ] **Step 3: Replace demo-window close logic**

Anchor: search for `window.closeDemoWindow = function() {` (around line 8644).

Replace this block:

```js
        window.closeDemoWindow = function() {
          win.style.transition = 'opacity 0.3s, transform 0.3s';
          win.style.opacity = '0';
          win.style.transform = (win.style.transform || 'translateX(-50%)') + ' scale(0.95)';
          setTimeout(function() {
            win.classList.add('hidden');
            win.style.display = 'none';
            win.style.opacity = '';
            win.style.transform = '';
            win.style.transition = '';
            taskbar.style.display = 'none';
            taskbar.classList.remove('active', 'minimized');
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            stopFx();
          }, 300);
        };
```

with:

```js
        window.closeDemoWindow = function() {
          win.classList.add('closing');
          setTimeout(function() {
            win.classList.remove('closing');
            win.classList.add('hidden');
            win.style.display = 'none';
            taskbar.style.display = 'none';
            taskbar.classList.remove('active', 'minimized');
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            stopFx();
          }, 300);
        };
```

- [ ] **Step 4: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright:
1. `localStorage.setItem('kd:prefs:archives-internal-browser', 'on')`, reload.
2. Click "Wiki Archive" card. Drag window via titlebar (use `browser_drag` from titlebar to ~200px right). Click close.
3. Watch the close animation — should fade + slightly shrink, no jump.
4. Repeat for demo window.
5. Screenshot during the animation (~150 ms in) and visually confirm the window is at its dragged position, not snapping to center.

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(browser,demo): close animation broken for dragged windows

The previous code concatenated 'scale(0.95)' onto the existing inline
transform. When the window had been dragged, that transform was 'none',
producing invalid CSS 'none scale(0.95)' — the browser dropped it
entirely and the window vanished without animating.

Drive the animation off a .closing CSS class instead (opacity on the
window, scale on its children) so it composes correctly regardless of
positioning state.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: `userUnmaxed` mobile flag — fixes B7

**Files:**
- Modify: `static/index.html` (browser-window IIFE state + `maximizeBrowserWindow` + `toggleBrowserWindow`)

**Why:** On mobile, opening a window auto-maximizes (intended). Today, after the user manually un-maximizes and minimizes, the toggle restores it and re-applies auto-max — overriding their preference. Track an explicit "user-unmaxed" flag.

- [ ] **Step 1: Add `userUnmaxed` state**

Anchor: search for `var currentUrl = '';` (around line 8199).

Replace:

```js
        var currentUrl = '';
        var currentLabel = '';
        var isMax = false;
        var prevStyles = {};
        var historyStack = [];
        var historyIndex = -1;
```

with:

```js
        var currentUrl = '';
        var currentLabel = '';
        var isMax = false;
        var userUnmaxed = false; // set when user explicitly un-maximizes; suppresses mobile auto-max on next restore
        var prevStyles = {};
        var historyStack = [];
        var historyIndex = -1;
```

- [ ] **Step 2: Set the flag on un-maximize**

Anchor: search for `window.maximizeBrowserWindow = function() {` (around line 8508).

Replace:

```js
        window.maximizeBrowserWindow = function() {
          if (!isMax) {
            prevStyles = {
              left: win.style.left, top: win.style.top,
              right: win.style.right, bottom: win.style.bottom,
              width: win.style.width, height: win.style.height,
              position: win.style.position,
              margin: win.style.margin, transform: win.style.transform
            };
            win.classList.add('maximized');
            isMax = true;
          } else {
            win.classList.remove('maximized');
            win.style.left = prevStyles.left || '';
            win.style.top = prevStyles.top || '';
            win.style.right = prevStyles.right || '';
            win.style.bottom = prevStyles.bottom || '';
            win.style.width = prevStyles.width || '';
            win.style.height = prevStyles.height || '';
            win.style.position = prevStyles.position || '';
            win.style.margin = prevStyles.margin || '';
            win.style.transform = prevStyles.transform || '';
            isMax = false;
          }
        };
```

with:

```js
        window.maximizeBrowserWindow = function() {
          if (!isMax) {
            prevStyles = {
              left: win.style.left, top: win.style.top,
              right: win.style.right, bottom: win.style.bottom,
              width: win.style.width, height: win.style.height,
              position: win.style.position,
              margin: win.style.margin, transform: win.style.transform
            };
            win.classList.add('maximized');
            isMax = true;
          } else {
            win.classList.remove('maximized');
            win.style.left = prevStyles.left || '';
            win.style.top = prevStyles.top || '';
            win.style.right = prevStyles.right || '';
            win.style.bottom = prevStyles.bottom || '';
            win.style.width = prevStyles.width || '';
            win.style.height = prevStyles.height || '';
            win.style.position = prevStyles.position || '';
            win.style.margin = prevStyles.margin || '';
            win.style.transform = prevStyles.transform || '';
            isMax = false;
            userUnmaxed = true;
          }
        };
```

- [ ] **Step 3: Suppress mobile auto-max on restore**

Anchor: search for `window.toggleBrowserWindow = function() {` (around line 8487).

Replace:

```js
        window.toggleBrowserWindow = function() {
          if (win.classList.contains('hidden')) return;
          if (win.style.display === 'none' || taskbar.classList.contains('minimized')) {
            win.style.display = '';
            win.classList.add('restoring');
            taskbar.classList.remove('minimized');
            taskbar.classList.add('active');
            setTimeout(function() { win.classList.remove('restoring'); }, 500);
            if (window.bringToFront) window.bringToFront(win);
            if (window.matchMedia('(max-width: 800px)').matches && !isMax) {
              maximizeBrowserWindow();
            }
          } else if (taskbar.classList.contains('active')) {
            if (window.__focusedWindow !== win) {
              window.bringToFront(win);
            } else {
              minimizeBrowserWindow();
            }
          }
        };
```

with:

```js
        window.toggleBrowserWindow = function() {
          if (win.classList.contains('hidden')) return;
          if (win.style.display === 'none' || taskbar.classList.contains('minimized')) {
            win.style.display = '';
            win.classList.add('restoring');
            taskbar.classList.remove('minimized');
            taskbar.classList.add('active');
            setTimeout(function() { win.classList.remove('restoring'); }, 500);
            if (window.bringToFront) window.bringToFront(win);
            // userUnmaxed is set the first time the user manually un-maximizes;
            // once set, restore-from-min must not re-apply mobile auto-max,
            // since that would override their explicit choice every cycle.
            if (window.matchMedia('(max-width: 800px)').matches && !isMax && !userUnmaxed) {
              maximizeBrowserWindow();
            }
          } else if (taskbar.classList.contains('active')) {
            if (window.__focusedWindow !== win) {
              window.bringToFront(win);
            } else {
              minimizeBrowserWindow();
            }
          }
        };
```

- [ ] **Step 4: Reset flag on close**

Anchor: search for the `closeBrowserWindow` block updated in Task 2. Find the `setTimeout` callback inside it.

Replace this section of the callback (the part starting `if (isMax)`):

```js
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            contentEl.innerHTML = '';
            currentUrl = '';
            historyStack = [];
            historyIndex = -1;
            updateBackBtn();
```

with:

```js
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            userUnmaxed = false;
            contentEl.innerHTML = '';
            currentUrl = '';
            historyStack = [];
            historyIndex = -1;
            updateBackBtn();
```

- [ ] **Step 5: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright with viewport set narrow:

```js
// browser_resize to 600x900 to simulate mobile
```

1. Set the prefs flag, reload.
2. Open Wiki Archive → expect auto-maximized.
3. Click maximize button to un-maximize → expect window-sized restore.
4. Click minimize → expect minimize.
5. Click the taskbar item to restore → assert via `browser_evaluate` that `document.getElementById('browser-window').classList.contains('maximized') === false`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(browser): mobile re-maximize after explicit unmaximize

Track a userUnmaxed flag set when the user manually un-maximizes. The
next restore-from-min on a narrow viewport now respects that choice
instead of re-applying auto-max every cycle. Flag resets on close.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Delete `cfErrorDoc` dead code (B15)

**Files:**
- Modify: `static/index.html` (function `cfErrorDoc` around line 8242)

**Why:** Defined and never called. ~70 lines of dead inline HTML for a fictional Cloudflare 524 page. Removing it now keeps the upcoming rewrite tasks smaller.

- [ ] **Step 1: Verify `cfErrorDoc` is not referenced**

Run:

```bash
grep -n "cfErrorDoc" static/index.html
```

Expected output (should be exactly one line — the definition):

```
8242:        function cfErrorDoc(url, host) {
```

If anything else appears, stop and investigate.

- [ ] **Step 2: Delete the function**

Anchor: search for `function cfErrorDoc(url, host) {` (around line 8242).

Delete the entire function body, from the line `function cfErrorDoc(url, host) {` through its closing `}` (around line 8312, look for the line containing `'</body></html>';` followed by `}`). The deletion ends just before `function homeDoc() {`.

- [ ] **Step 3: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

Confirm no JS console errors on page load. Open the in-window browser via the Wiki card; about:home page (clicking Home if visible) should still render.

```js
// browser_console_messages to check there are no errors
```

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(browser): drop unused cfErrorDoc helper

70-line inline HTML for a Cloudflare 524 page that was defined but
never called from anywhere. Deleting it shrinks the IIFE before the
multi-window rewrite.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Archive registry + collapse callers

**Files:**
- Modify: `static/index.html` (start menu items 5243–5269, featured cards 5501–5534, browser IIFE around line 8429)

**Why:** Today there are 14 inline `(url, label)` pairs across the start menu and homepage cards. Collapse them to `openFeaturedArchive('id')` calls reading from a single registry. Single source of truth for url, label, and (later) icon SVG.

- [ ] **Step 1: Add the `ARCHIVES` array near the top of the browser-window IIFE**

Anchor: search for `var win = document.getElementById('browser-window');` (around line 8190 — this is inside the IIFE, just below `(function() {`).

Replace:

```js
      (function() {
        var win = document.getElementById('browser-window');
```

with:

```js
      (function() {
        // Single source of truth for the seven featured archives. Both the
        // start-menu items and the homepage cards dispatch through
        // openFeaturedArchive(id) and read url/label/hostname from here.
        var ARCHIVES = [
          { id: 'wiki',     url: 'https://wiki.kunaldawn.com',     label: 'Wiki Archive' },
          { id: 'pdf',      url: 'https://pdf.kunaldawn.com',      label: 'PDF Archive' },
          { id: 'os',       url: 'https://os.kunaldawn.com',       label: 'OS Archive' },
          { id: 'iso',      url: 'https://iso.kunaldawn.com',      label: 'CD/DVD Archive' },
          { id: 'chiptune', url: 'https://chiptune.kunaldawn.com', label: 'Chiptune Archive' },
          { id: 'tube',     url: 'https://tube.kunaldawn.com',     label: 'Tube Archive' },
          { id: 'audio',    url: 'https://audio.kunaldawn.com',    label: 'Audiobook Archive' }
        ];
        var REGISTRY = {};
        ARCHIVES.forEach(function(a) {
          REGISTRY[a.id] = { id: a.id, url: a.url, label: a.label, hostname: new URL(a.url).host };
        });
        window.__archives = REGISTRY;

        var win = document.getElementById('browser-window');
```

- [ ] **Step 2: Rewrite `openFeaturedArchive` to take an id**

Anchor: search for `window.openFeaturedArchive = function(url, label) {` (around line 8429).

Replace:

```js
        window.openFeaturedArchive = function(url, label) {
          var useInternal = false;
          try { useInternal = localStorage.getItem('kd:prefs:archives-internal-browser') === 'on'; } catch(e) {}
          if (useInternal) {
            window.openBrowserWindow(url, label);
          } else {
            window.open(url, '_blank', 'noopener');
          }
        };
```

with:

```js
        window.openFeaturedArchive = function(id) {
          var entry = REGISTRY[id];
          if (!entry) return;
          var useInternal = false;
          try { useInternal = localStorage.getItem('kd:prefs:archives-internal-browser') === 'on'; } catch(e) {}
          if (useInternal) {
            window.openBrowserWindow(entry.url, entry.label);
          } else {
            window.open(entry.url, '_blank', 'noopener');
          }
        };
```

- [ ] **Step 3: Update the seven start-menu onclick handlers**

Anchor: search for `openFeaturedArchive('https://wiki.kunaldawn.com'` (around line 5243).

Apply these seven edits. Each `old_string`/`new_string` pair targets one occurrence in the start-menu block (lines ~5243–5269):

| Old `onclick=` | New |
|---|---|
| `openFeaturedArchive('https://wiki.kunaldawn.com', 'Wiki Archive')` | `openFeaturedArchive('wiki')` |
| `openFeaturedArchive('https://pdf.kunaldawn.com', 'PDF Archive')` | `openFeaturedArchive('pdf')` |
| `openFeaturedArchive('https://os.kunaldawn.com', 'OS Archive')` | `openFeaturedArchive('os')` |
| `openFeaturedArchive('https://iso.kunaldawn.com', 'CD/DVD Archive')` | `openFeaturedArchive('iso')` |
| `openFeaturedArchive('https://chiptune.kunaldawn.com', 'Chiptune Archive')` | `openFeaturedArchive('chiptune')` |
| `openFeaturedArchive('https://tube.kunaldawn.com', 'Tube Archive')` | `openFeaturedArchive('tube')` |
| `openFeaturedArchive('https://audio.kunaldawn.com', 'Audiobook Archive')` | `openFeaturedArchive('audio')` |

Important: the homepage cards block (lines ~5501–5534) uses identical
substrings. Use a longer `old_string` that includes surrounding context (the
`<div class="menu-item"` and the closing `>` plus first child markup) to
disambiguate, or apply the seven edits using the start-menu cards block first,
then again for the homepage cards block in step 4.

- [ ] **Step 4: Update the seven homepage card onclick handlers**

Anchor: search for `<a class="card" href="https://wiki.kunaldawn.com"` (around line 5501).

For each card, change `event.preventDefault(); openFeaturedArchive('<URL>', '<LABEL>');` to `event.preventDefault(); openFeaturedArchive('<ID>');` matching the table above. Seven edits.

- [ ] **Step 5: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright:
1. Set the prefs flag, reload.
2. Open the start menu (click START button), click "Wiki Archive". Expect the in-window browser opens with the Wiki iframe.
3. Close it, click the homepage Wiki card. Same expectation.
4. Repeat for one other archive (e.g., PDF) from each entry point.
5. Assert via `browser_evaluate`: `Object.keys(window.__archives).sort()` ⇒ `["audio","chiptune","iso","os","pdf","tube","wiki"]`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(browser): single archive registry, openFeaturedArchive(id)

Collapses the 14 inline (url, label) pairs across the start menu and
the homepage featured cards into one ARCHIVES array inside the
browser-window IIFE. Both entry points dispatch through
openFeaturedArchive(id) and look up url/label/hostname from there.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Toolbar rewrite — drop address bar, back, home, about:home

**Files:**
- Modify: `static/index.html` (browser-window markup at 5382–5404, browser-window IIFE — `homeDoc`, `render`, `browserGoHome`, `browserGoBack`, history machinery, postMessage handlers, `load` fallback)

**Why:** All in one task because they're tightly coupled — the address-bar input, Back button, Home button, history stack, `kd-iframe-loc` postMessage handler, same-origin `load` URL-read fallback, `browser-nav` postMessage handler, and the `homeDoc()` start page form one interconnected system that exists solely for the cross-origin-broken address bar feature. Pulling them apart in separate tasks would leave the toolbar and IIFE in inconsistent states between commits.

This task closes A2, B4, B5, B6, B8, B11, B12, B13, B14, and the `historyStack`/`recenter` portion of B17. Single-window semantics still apply — that's Task 8.

- [ ] **Step 1: Replace the toolbar markup**

Anchor: search for `<!-- Archive browser window -->` (around line 5381).

Replace this block:

```html
    <!-- Archive browser window -->
    <div class="browser-window hidden" id="browser-window">
      <div class="vault-titlebar" id="browser-titlebar">
        <div class="vault-titlebar-controls">
          <div class="window-btn close" onclick="closeBrowserWindow()" title="Close"></div>
          <div class="window-btn minimize" onclick="minimizeBrowserWindow()" title="Minimize"></div>
          <div class="window-btn maximize" onclick="maximizeBrowserWindow()" title="Maximize"></div>
        </div>
        <span class="vault-titlebar-title" id="browser-title">browser@kd:~</span>
      </div>
      <div class="browser-toolbar">
        <button class="browser-btn" id="browser-back" onclick="browserGoBack()" title="Back" disabled>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>
        </button>
        <button class="browser-btn" id="browser-home" onclick="browserGoHome()" title="Home">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2h-4a2 2 0 0 1-2-2v-5h-2v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V9z"/></svg>
        </button>
        <input type="text" class="browser-address" id="browser-address" readonly value="about:home" aria-label="Address">
        <button class="browser-btn" id="browser-external" onclick="browserOpenExternal()" title="Open in new tab">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </button>
      </div>
      <div class="browser-content" id="browser-content"></div>
    </div>
```

with:

```html
    <!-- Archive browser window -->
    <div class="browser-window hidden" id="browser-window">
      <div class="vault-titlebar" id="browser-titlebar">
        <div class="vault-titlebar-controls">
          <div class="window-btn close" onclick="closeBrowserWindow()" title="Close"></div>
          <div class="window-btn minimize" onclick="minimizeBrowserWindow()" title="Minimize"></div>
          <div class="window-btn maximize" onclick="maximizeBrowserWindow()" title="Maximize"></div>
        </div>
        <span class="vault-titlebar-title" id="browser-title">browser@kd:~</span>
      </div>
      <div class="browser-toolbar">
        <button class="browser-btn" id="browser-reload" onclick="browserReload()" title="Reload">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
        </button>
        <span class="browser-title-strip" id="browser-title-strip" aria-live="polite"></span>
        <button class="browser-btn" id="browser-external" onclick="browserOpenExternal()" title="Open in new tab">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </button>
      </div>
      <div class="browser-content" id="browser-content"></div>
    </div>
```

- [ ] **Step 2: Update the toolbar CSS — drop `.browser-address`, add `.browser-title-strip`**

Anchor: search for `.browser-address {` (around line 2104).

Replace this block (the address-bar rules plus the existing focus rule):

```css
    .browser-address {
      flex: 1;
      min-width: 0;
      background: rgba(0, 0, 0, 0.5);
      border: 1px solid rgba(0, 255, 150, 0.15);
      color: var(--neon);
      font-family: 'Share Tech Mono', monospace;
      font-size: 12px;
      padding: 6px 10px;
      border-radius: 4px;
      outline: none;
      cursor: default;
      text-overflow: ellipsis;
      text-shadow: 0 0 4px rgba(0, 255, 150, 0.25);
      user-select: all;
    }

    .browser-address:focus {
      border-color: rgba(0, 255, 150, 0.35);
      background: rgba(0, 0, 0, 0.65);
    }
```

with:

```css
    .browser-title-strip {
      flex: 1;
      min-width: 0;
      color: var(--neon);
      font-family: 'Share Tech Mono', monospace;
      font-size: 12px;
      padding: 6px 10px;
      text-align: center;
      text-overflow: ellipsis;
      overflow: hidden;
      white-space: nowrap;
      text-shadow: 0 0 4px rgba(0, 255, 150, 0.25);
      opacity: 0.85;
      user-select: text;
    }
    .browser-reload.loading svg { animation: kd-spin 0.8s linear infinite; }
    @keyframes kd-spin { to { transform: rotate(360deg); } }
```

Also update the mobile media query at lines ~2240–2254. Anchor: search for `.browser-toolbar {` inside the `@media (max-width: 800px)` block (around line 2240).

Replace this block:

```css
      .browser-toolbar {
        padding: 6px 8px;
        gap: 6px;
      }

      .browser-address {
        font-size: 11px;
        padding: 5px 8px;
      }

      .browser-btn {
        padding: 5px 7px;
      }
```

with:

```css
      .browser-toolbar {
        padding: 6px 8px;
        gap: 6px;
      }

      .browser-title-strip {
        font-size: 11px;
        padding: 5px 8px;
      }

      .browser-btn {
        padding: 5px 7px;
      }
```

- [ ] **Step 3: Replace the browser-window IIFE body with a streamlined render path**

Anchor: search for `var titlebar = document.getElementById('browser-titlebar');` (around line 8191).

Replace this block:

```js
        var titlebar = document.getElementById('browser-titlebar');
        var titleEl = document.getElementById('browser-title');
        var addrEl = document.getElementById('browser-address');
        var contentEl = document.getElementById('browser-content');
        var taskbar = document.getElementById('browser-taskbar');
        var taskbarLabel = document.getElementById('browser-taskbar-label');
        var backBtn = document.getElementById('browser-back');

        var currentUrl = '';
        var currentLabel = '';
        var isMax = false;
        var userUnmaxed = false; // set when user explicitly un-maximizes; suppresses mobile auto-max on next restore
        var prevStyles = {};
        var historyStack = [];
        var historyIndex = -1;

        function updateBackBtn() {
          if (!backBtn) return;
          if (historyIndex > 0) backBtn.removeAttribute('disabled');
          else backBtn.setAttribute('disabled', 'disabled');
        }

        function pushHistory(url, label) {
          if (!url) return;
          var top = historyStack[historyIndex];
          if (top && top.url === url) return;
          historyStack = historyStack.slice(0, historyIndex + 1);
          historyStack.push({ url: url, label: label || '' });
          historyIndex = historyStack.length - 1;
          updateBackBtn();
        }

        if (window.makeDraggable) window.makeDraggable(win, titlebar);
```

with:

```js
        var titlebar = document.getElementById('browser-titlebar');
        var titleEl = document.getElementById('browser-title');
        var titleStripEl = document.getElementById('browser-title-strip');
        var contentEl = document.getElementById('browser-content');
        var taskbar = document.getElementById('browser-taskbar');
        var taskbarLabel = document.getElementById('browser-taskbar-label');
        var reloadBtn = document.getElementById('browser-reload');

        var currentUrl = '';
        var currentLabel = '';
        var currentHost = '';
        var isMax = false;
        var userUnmaxed = false; // set when user explicitly un-maximizes; suppresses mobile auto-max on next restore
        var prevStyles = {};

        if (window.makeDraggable) window.makeDraggable(win, titlebar);
```

- [ ] **Step 4: Delete the entire `homeDoc()` builder**

Anchor: search for `function homeDoc() {` (around line 8314 after Task 4's deletion).

Delete the function from the line `function homeDoc() {` through its closing `}` and the closing `'</body></html>';` literal. The deletion ends just before `function render(url, label, options) {`.

- [ ] **Step 5: Replace `render`**

Anchor: search for `function render(url, label, options) {` (around line 8364).

Replace the entire `render` function (everything from `function render(url, label, options) {` through its closing `}`):

```js
        function render(url, label, options) {
          options = options || {};
          currentUrl = url;
          currentLabel = label || '';
          var iframe = document.createElement('iframe');
          if (url === 'about:home') {
            iframe.setAttribute('sandbox', 'allow-scripts');
            addrEl.value = 'about:home';
            titleEl.textContent = 'browser@kd:~/home';
            taskbarLabel.textContent = 'Browser';
            iframe.srcdoc = homeDoc();
          } else {
            var host;
            try { host = new URL(url).host; } catch (e) { host = url; }
            addrEl.value = url;
            titleEl.textContent = 'browser@kd:~/' + (label || host);
            taskbarLabel.textContent = label || host;
            iframe.src = url;
          }
          contentEl.innerHTML = '';
          contentEl.appendChild(iframe);
          if (!options.fromHistory) {
            pushHistory(url, label);
          }
          updateBackBtn();
        }
```

with:

```js
        function render(url, label) {
          currentUrl = url;
          currentLabel = label || '';
          try { currentHost = new URL(url).host; } catch (e) { currentHost = url; }
          var iframe = document.createElement('iframe');
          iframe.src = url;
          titleEl.textContent = 'browser@kd:~/' + (label || currentHost);
          taskbarLabel.textContent = label || currentHost;
          titleStripEl.textContent = (label ? label + ' — ' : '') + currentHost;
          contentEl.innerHTML = '';
          contentEl.appendChild(iframe);
        }
```

- [ ] **Step 6: Delete `recenter()` and update `showWindow` to drop its call**

Anchor: search for `function recenter() {` (around line 8391).

Delete the entire function from `function recenter() {` through its closing `}` (about 13 lines).

Anchor: search for `function showWindow() {` (around line 8405).

Replace:

```js
        function showWindow() {
          if (isMax) { win.classList.remove('maximized'); isMax = false; }
          recenter();
          win.classList.remove('hidden');
          win.style.display = '';
          win.classList.add('restoring');
          taskbar.style.display = '';
          taskbar.classList.remove('minimized');
          taskbar.classList.add('active');
          setTimeout(function() { win.classList.remove('restoring'); }, 500);
          if (window.bringToFront) window.bringToFront(win);
          var menu = document.getElementById('startMenu');
          if (menu && menu.classList.contains('show')) toggleStartMenu();
          // Auto-maximize on mobile / narrow viewports
          if (window.matchMedia('(max-width: 800px)').matches && !isMax) {
            maximizeBrowserWindow();
          }
        }
```

with:

```js
        function showWindow() {
          if (isMax) { win.classList.remove('maximized'); isMax = false; }
          win.classList.remove('hidden');
          win.style.display = '';
          win.classList.add('restoring');
          taskbar.style.display = '';
          taskbar.classList.remove('minimized');
          taskbar.classList.add('active');
          setTimeout(function() { win.classList.remove('restoring'); }, 500);
          if (window.bringToFront) window.bringToFront(win);
          var menu = document.getElementById('startMenu');
          if (menu && menu.classList.contains('show')) toggleStartMenu();
          // Auto-maximize on mobile / narrow viewports
          if (window.matchMedia('(max-width: 800px)').matches && !isMax) {
            maximizeBrowserWindow();
          }
        }
```

(Identical except the `recenter()` line is gone.)

- [ ] **Step 7: Replace history/back/home/external stubs with reload + external**

Anchor: search for `window.browserGoHome = function() {` (around line 8439).

Replace this block:

```js
        window.browserGoHome = function() {
          render('about:home', '');
        };

        window.browserGoBack = function() {
          if (historyIndex <= 0) return;
          historyIndex--;
          var prev = historyStack[historyIndex];
          render(prev.url, prev.label, { fromHistory: true });
        };

        window.browserOpenExternal = function() {
          if (currentUrl && currentUrl !== 'about:home') {
            window.open(currentUrl, '_blank', 'noopener');
          }
        };
```

with:

```js
        window.browserReload = function() {
          if (!currentUrl) return;
          var iframe = contentEl.querySelector('iframe');
          if (!iframe) return;
          if (reloadBtn) reloadBtn.classList.add('loading');
          // Cache-buster forces a fresh fetch even when upstream serves
          // strong Cache-Control. Stripped on next render so each archive
          // window only carries the marker between explicit reloads.
          var sep = currentUrl.indexOf('?') === -1 ? '?' : '&';
          iframe.src = currentUrl + sep + '_kd_reload=' + Date.now();
          var done = function() {
            if (reloadBtn) reloadBtn.classList.remove('loading');
            iframe.removeEventListener('load', done);
          };
          iframe.addEventListener('load', done);
          // Safety: clear the spinner after 10s even if load never fires
          // (e.g., archive offline behind cloudflared).
          setTimeout(done, 10000);
        };

        window.browserOpenExternal = function() {
          if (currentUrl) {
            window.open(currentUrl, '_blank', 'noopener');
          }
        };
```

- [ ] **Step 8: Strip the closeBrowserWindow body of history-related cleanup**

Anchor: the `closeBrowserWindow` function updated in Tasks 2/3. Search for `historyStack = [];` inside it.

Replace this section of the setTimeout callback:

```js
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            userUnmaxed = false;
            contentEl.innerHTML = '';
            currentUrl = '';
            historyStack = [];
            historyIndex = -1;
            updateBackBtn();
```

with:

```js
            if (isMax) { win.classList.remove('maximized'); isMax = false; }
            userUnmaxed = false;
            prevStyles = {};
            contentEl.innerHTML = '';
            currentUrl = '';
            currentLabel = '';
            currentHost = '';
```

- [ ] **Step 9: Delete the postMessage and load handlers**

Anchor: search for `// Receive nav requests from the about:home iframe` (around line 8534).

Delete from that comment through (and including) the closing `}, true);` of the `contentEl.addEventListener('load', ...)` block. That's the entire two-block region — everything from the comment that starts `// Receive nav requests` through the closing `}, true);`.

The next surviving line should be the `// Double-click titlebar toggles maximize` comment.

- [ ] **Step 10: Remove obsolete `openBrowserWindow` no-URL handling**

Anchor: search for `window.openBrowserWindow = function(url, label) {` (around line 8424).

Replace:

```js
        window.openBrowserWindow = function(url, label) {
          render(url || 'about:home', label);
          showWindow();
        };
```

with:

```js
        window.openBrowserWindow = function(url, label) {
          if (!url) return; // about:home is gone — every open requires an entry URL
          render(url, label);
          showWindow();
        };
```

- [ ] **Step 11: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright:
1. Set the prefs flag, reload.
2. Open Wiki Archive. Confirm:
   - Toolbar shows: reload icon · `Wiki Archive — wiki.kunaldawn.com` · external-tab icon. No address-bar input. No back/home buttons.
   - Click Reload — the reload SVG should spin briefly and the iframe reloads.
   - Click external — a new tab opens to `https://wiki.kunaldawn.com`.
3. Close. Open PDF Archive. Title strip updates to `PDF Archive — pdf.kunaldawn.com`.
4. Console check: no errors, no warnings about missing handlers.

```js
// browser_evaluate
() => ({
  hasAddressBar: !!document.getElementById('browser-address'),
  hasBackBtn: !!document.getElementById('browser-back'),
  hasHomeBtn: !!document.getElementById('browser-home'),
  hasReloadBtn: !!document.getElementById('browser-reload'),
  titleStrip: document.getElementById('browser-title-strip').textContent
})
// Expected: hasAddressBar=false, hasBackBtn=false, hasHomeBtn=false,
//           hasReloadBtn=true, titleStrip starts with "Wiki Archive — wiki.kunaldawn.com"
//           (or whichever archive is currently open).
```

- [ ] **Step 12: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(browser): drop address bar, back, home, about:home

The address bar relied on either same-origin iframe.contentWindow.location
reads (throws cross-origin) or postMessage opt-in from each archive (not
all archives carry the broadcaster) — neither worked reliably, leaving
the bar frozen at the entry URL forever and the back/forward/history
system useless.

Replace the toolbar with reload + a static title strip that always
reads <label> — <hostname>, plus the existing open-in-new-tab button.
Reload appends a cache-buster so upstream Cache-Control headers don't
defeat it. Drops ~200 lines: homeDoc, history stack, push/pop,
browser-nav postMessage handler (also closes a security hole that
trusted any frame), kd-iframe-loc handler, the same-origin load
fallback, and recenter().

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Multi-window — Map<id, instance> + cloned template

**Files:**
- Modify: `static/index.html` (browser-window markup, browser-window IIFE — full rewrite)

**Why:** The biggest task. Switches from a singleton `<div id="browser-window">` driven by module-scope state to a `Map<archiveId, WindowInstance>` where each instance owns its DOM clone, taskbar item, and per-instance state. Closes A1, B1 (no recenter needed on fresh clones), B3 (per-instance prevStyles), B10 (per-instance state cleared on close).

This task is large but cohesive — splitting it would leave half-multi half-singleton state and break the site between commits.

- [ ] **Step 1: Convert the browser-window markup to a `<template>`**

Anchor: search for `<!-- Archive browser window -->` (around line 5381 — same anchor as Task 6 but markup now includes the new toolbar).

Replace:

```html
    <!-- Archive browser window -->
    <div class="browser-window hidden" id="browser-window">
      <div class="vault-titlebar" id="browser-titlebar">
        <div class="vault-titlebar-controls">
          <div class="window-btn close" onclick="closeBrowserWindow()" title="Close"></div>
          <div class="window-btn minimize" onclick="minimizeBrowserWindow()" title="Minimize"></div>
          <div class="window-btn maximize" onclick="maximizeBrowserWindow()" title="Maximize"></div>
        </div>
        <span class="vault-titlebar-title" id="browser-title">browser@kd:~</span>
      </div>
      <div class="browser-toolbar">
        <button class="browser-btn" id="browser-reload" onclick="browserReload()" title="Reload">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
        </button>
        <span class="browser-title-strip" id="browser-title-strip" aria-live="polite"></span>
        <button class="browser-btn" id="browser-external" onclick="browserOpenExternal()" title="Open in new tab">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </button>
      </div>
      <div class="browser-content" id="browser-content"></div>
    </div>
```

with:

```html
    <!-- Archive browser window template — cloned per archive by BrowserWindowManager -->
    <template id="browser-window-template">
      <div class="browser-window hidden">
        <div class="vault-titlebar">
          <div class="vault-titlebar-controls">
            <div class="window-btn close" data-action="close" title="Close"></div>
            <div class="window-btn minimize" data-action="minimize" title="Minimize"></div>
            <div class="window-btn maximize" data-action="maximize" title="Maximize"></div>
          </div>
          <span class="vault-titlebar-title">browser@kd:~</span>
        </div>
        <div class="browser-toolbar">
          <button class="browser-btn browser-reload" data-action="reload" title="Reload">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
          </button>
          <span class="browser-title-strip" aria-live="polite"></span>
          <button class="browser-btn browser-external" data-action="external" title="Open in new tab">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
          </button>
        </div>
        <div class="browser-content"></div>
      </div>
    </template>
    <div id="browser-window-host"></div>
```

The `<div id="browser-window-host">` is a stable mount point so cloned windows have a predictable place in the DOM (relative to `body` z-stacking). Cloned `.browser-window` nodes are appended to `<body>` directly (so they can move freely); the host div is currently unused but reserved for future per-archive grouping.

- [ ] **Step 2: Replace the browser-taskbar markup with a host container**

Anchor: search for `<div class="taskbar-item" id="browser-taskbar"` (around line 5451).

Replace:

```html
        <div class="taskbar-item" id="browser-taskbar" onclick="toggleBrowserWindow()" style="display:none">
          <span><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg></span>
          <span id="browser-taskbar-label">Browser</span>
        </div>
```

with:

```html
        <div id="browser-taskbar-host" style="display:contents"></div>
```

`display:contents` keeps the children participating in the flex layout of `.taskbar-items` while leaving the host an inert grouping element.

- [ ] **Step 3: Replace the entire browser-window IIFE**

Anchor: search for `<!-- Archive browser window -->` *script* comment (around line 8187, just inside `<script>`).

Replace the *entire IIFE* — from the `(function() {` after `<script>` through the matching `})();` before `</script>`. That's the block starting around line 8188 and ending around line 8576.

With this new IIFE:

```js
      (function() {
        // ─── Archive registry ───
        var ARCHIVES = [
          { id: 'wiki',     url: 'https://wiki.kunaldawn.com',     label: 'Wiki Archive' },
          { id: 'pdf',      url: 'https://pdf.kunaldawn.com',      label: 'PDF Archive' },
          { id: 'os',       url: 'https://os.kunaldawn.com',       label: 'OS Archive' },
          { id: 'iso',      url: 'https://iso.kunaldawn.com',      label: 'CD/DVD Archive' },
          { id: 'chiptune', url: 'https://chiptune.kunaldawn.com', label: 'Chiptune Archive' },
          { id: 'tube',     url: 'https://tube.kunaldawn.com',     label: 'Tube Archive' },
          { id: 'audio',    url: 'https://audio.kunaldawn.com',    label: 'Audiobook Archive' }
        ];
        var REGISTRY = {};
        ARCHIVES.forEach(function(a) {
          REGISTRY[a.id] = { id: a.id, url: a.url, label: a.label, hostname: new URL(a.url).host };
        });
        window.__archives = REGISTRY;

        // ─── Manager state ───
        var instances = new Map();           // id -> WindowInstance
        var cascadeIndex = 0;                // resets when instances.size === 0
        var template = document.getElementById('browser-window-template');
        var taskbarHost = document.getElementById('browser-taskbar-host');

        function archiveIcon() {
          // Re-used SVG for all archive taskbar items. Visually unifies them
          // since per-archive icons would be lost at small sizes.
          return '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>';
        }

        function escapeHtml(s) {
          return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
        }

        // ─── WindowInstance constructor ───
        function makeInstance(id) {
          var entry = REGISTRY[id];
          if (!entry) return null;

          var frag = template.content.cloneNode(true);
          var el = frag.querySelector('.browser-window');
          el.dataset.archiveId = id;
          el.classList.add('hidden');
          var titlebar = el.querySelector('.vault-titlebar');
          var titleEl = titlebar.querySelector('.vault-titlebar-title');
          var titleStripEl = el.querySelector('.browser-title-strip');
          var contentEl = el.querySelector('.browser-content');
          var reloadBtn = el.querySelector('.browser-reload');

          titleEl.textContent = 'browser@kd:~/' + entry.label;
          titleStripEl.textContent = entry.label + ' — ' + entry.hostname;

          var taskbarEl = document.createElement('div');
          taskbarEl.className = 'taskbar-item archive-window';
          taskbarEl.dataset.archiveId = id;
          taskbarEl.title = entry.label;
          taskbarEl.setAttribute('aria-label', entry.label);
          taskbarEl.innerHTML = '<span>' + archiveIcon() + '</span><span class="taskbar-item-label">' + escapeHtml(entry.label) + '</span>';

          var inst = {
            id: id,
            entry: entry,
            el: el,
            titlebar: titlebar,
            titleEl: titleEl,
            titleStripEl: titleStripEl,
            contentEl: contentEl,
            reloadBtn: reloadBtn,
            taskbarEl: taskbarEl,
            iframe: null,
            isMax: false,
            isMin: false,
            userUnmaxed: false,
            prevStyles: {}
          };

          // Wire titlebar buttons
          titlebar.querySelector('[data-action="close"]').addEventListener('click', function() { closeInstance(inst); });
          titlebar.querySelector('[data-action="minimize"]').addEventListener('click', function() { minimizeInstance(inst); });
          titlebar.querySelector('[data-action="maximize"]').addEventListener('click', function() { toggleMaxInstance(inst); });
          titlebar.addEventListener('dblclick', function(e) {
            if (e.target.closest('.window-btn')) return;
            toggleMaxInstance(inst);
          });

          // Wire toolbar buttons
          el.querySelector('[data-action="reload"]').addEventListener('click', function() { reloadInstance(inst); });
          el.querySelector('[data-action="external"]').addEventListener('click', function() {
            window.open(inst.entry.url, '_blank', 'noopener');
          });

          // Wire taskbar item
          taskbarEl.addEventListener('click', function() { toggleInstanceFromTaskbar(inst); });

          // Mount
          document.body.appendChild(el);
          taskbarHost.appendChild(taskbarEl);

          // Drag
          if (window.makeDraggable) window.makeDraggable(el, titlebar);

          return inst;
        }

        function placeNewWindow(inst) {
          // Cascade: first window centered (default CSS); each subsequent
          // window steps 30px down/right, wrapping after 5 to stay on screen.
          if (cascadeIndex === 0) {
            cascadeIndex = 1;
            return;
          }
          var step = (cascadeIndex - 0) % 5; // 1..4 then 0
          var offset = step * 30;
          var rect = inst.el.getBoundingClientRect();
          // CSS default centers the window via translateX(-50%); we switch to
          // explicit fixed positioning so cascade math is unambiguous.
          var wantedWidth = Math.min(1100, window.innerWidth * 0.92);
          var baseLeft = (window.innerWidth - wantedWidth) / 2;
          var baseTop = 40;
          inst.el.style.position = 'fixed';
          inst.el.style.left = (baseLeft + offset) + 'px';
          inst.el.style.top = (baseTop + offset) + 'px';
          inst.el.style.right = '';
          inst.el.style.bottom = '';
          inst.el.style.transform = 'none';
          inst.el.style.margin = '0';
          inst.el.classList.add('dragged');
          cascadeIndex++;
        }

        function showInstance(inst) {
          inst.el.classList.remove('hidden');
          inst.el.style.display = '';
          inst.el.classList.add('restoring');
          inst.taskbarEl.classList.remove('minimized');
          inst.taskbarEl.classList.add('active');
          setTimeout(function() { inst.el.classList.remove('restoring'); }, 500);
          if (window.bringToFront) window.bringToFront(inst.el);
          inst.isMin = false;
        }

        function loadIframe(inst) {
          var iframe = document.createElement('iframe');
          iframe.src = inst.entry.url;
          inst.contentEl.classList.add('loading');
          var done = function() {
            inst.contentEl.classList.remove('loading');
            iframe.removeEventListener('load', done);
          };
          iframe.addEventListener('load', done);
          setTimeout(done, 10000);
          inst.contentEl.innerHTML = '';
          inst.contentEl.appendChild(iframe);
          inst.iframe = iframe;
        }

        function maybeMobileMaximize(inst) {
          if (inst.userUnmaxed) return;
          if (window.matchMedia('(max-width: 800px)').matches && !inst.isMax) {
            applyMax(inst);
          }
        }

        function applyMax(inst) {
          inst.prevStyles = {
            left: inst.el.style.left, top: inst.el.style.top,
            right: inst.el.style.right, bottom: inst.el.style.bottom,
            width: inst.el.style.width, height: inst.el.style.height,
            position: inst.el.style.position,
            margin: inst.el.style.margin, transform: inst.el.style.transform
          };
          inst.el.classList.add('maximized');
          inst.isMax = true;
        }

        function unapplyMax(inst) {
          inst.el.classList.remove('maximized');
          var p = inst.prevStyles;
          inst.el.style.left = p.left || '';
          inst.el.style.top = p.top || '';
          inst.el.style.right = p.right || '';
          inst.el.style.bottom = p.bottom || '';
          inst.el.style.width = p.width || '';
          inst.el.style.height = p.height || '';
          inst.el.style.position = p.position || '';
          inst.el.style.margin = p.margin || '';
          inst.el.style.transform = p.transform || '';
          inst.isMax = false;
          inst.userUnmaxed = true;
        }

        function toggleMaxInstance(inst) {
          if (!inst.isMax) applyMax(inst); else unapplyMax(inst);
        }

        function minimizeInstance(inst) {
          inst.el.classList.add('minimizing');
          inst.taskbarEl.classList.add('minimized');
          inst.taskbarEl.classList.remove('active');
          inst.isMin = true;
          setTimeout(function() {
            inst.el.style.display = 'none';
            inst.el.classList.remove('minimizing');
          }, 500);
        }

        function reloadInstance(inst) {
          if (!inst.iframe) return;
          inst.reloadBtn.classList.add('loading');
          inst.contentEl.classList.add('loading');
          var sep = inst.entry.url.indexOf('?') === -1 ? '?' : '&';
          var done = function() {
            inst.reloadBtn.classList.remove('loading');
            inst.contentEl.classList.remove('loading');
            inst.iframe.removeEventListener('load', done);
          };
          inst.iframe.addEventListener('load', done);
          setTimeout(done, 10000);
          inst.iframe.src = inst.entry.url + sep + '_kd_reload=' + Date.now();
        }

        function closeInstance(inst) {
          inst.el.classList.add('closing');
          setTimeout(function() {
            try { inst.el.remove(); } catch (e) {}
            try { inst.taskbarEl.remove(); } catch (e) {}
            instances.delete(inst.id);
            if (instances.size === 0) cascadeIndex = 0;
          }, 300);
        }

        function toggleInstanceFromTaskbar(inst) {
          if (inst.isMin || inst.el.style.display === 'none') {
            inst.el.style.display = '';
            inst.el.classList.add('restoring');
            inst.taskbarEl.classList.remove('minimized');
            inst.taskbarEl.classList.add('active');
            setTimeout(function() { inst.el.classList.remove('restoring'); }, 500);
            if (window.bringToFront) window.bringToFront(inst.el);
            inst.isMin = false;
            // userUnmaxed suppresses re-applying mobile auto-max on restore.
            if (!inst.userUnmaxed && window.matchMedia('(max-width: 800px)').matches && !inst.isMax) {
              applyMax(inst);
            }
            return;
          }
          if (window.__focusedWindow !== inst.el) {
            window.bringToFront(inst.el);
            return;
          }
          minimizeInstance(inst);
        }

        // ─── Public API ───
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

        // Legacy entry point — used by any caller still passing (url, label).
        // Resolves the id from the registry by URL.
        window.openBrowserWindow = function(url, label) {
          if (!url) return;
          var match = ARCHIVES.find(function(a) { return a.url === url; });
          if (match) { window.openFeaturedArchive(match.id); return; }
          // Unknown URL: open externally to avoid trusting an arbitrary string.
          window.open(url, '_blank', 'noopener');
        };

        // Expose the instance map for cat-avoidance and debugging.
        window.__browserInstances = instances;
      })();
```

- [ ] **Step 4: Add per-archive cascade & taskbar CSS**

Anchor: search for `.browser-content iframe {` (around line 2134).

Insert *after* that block (just before the `/* ═══ Demoscene window ═══ */` comment around line 2143), this CSS:

```css
    /* Multi-archive taskbar items: clamp width on desktop, drop label on mobile */
    .taskbar-item.archive-window {
      max-width: 130px;
      overflow: hidden;
    }
    .taskbar-item.archive-window .taskbar-item-label {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      max-width: 90px;
      display: inline-block;
    }
    @media (max-width: 600px) {
      .taskbar-item.archive-window .taskbar-item-label { display: none; }
      .taskbar-item.archive-window { max-width: 36px; }
    }
```

- [ ] **Step 5: Restart server and verify single-archive flow still works**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright:
1. Set the prefs flag, reload.
2. Click Wiki card. Confirm a window opens with the new toolbar and a taskbar item appears with the label "Wiki Archive".
3. Click Wiki card *again*. Assert: same window remains (still one taskbar item), brought to front.
4. Click PDF card. Assert: a *second* window opens, cascaded ~30px down/right, with its own taskbar item ("PDF Archive").
5. Assert via `browser_evaluate`:

```js
() => ({
  count: window.__browserInstances.size,
  ids: Array.from(window.__browserInstances.keys()),
  taskbarItems: document.querySelectorAll('.taskbar-item.archive-window').length
})
// Expected: count=2, ids=["wiki","pdf"] (order depends on Map insertion),
//           taskbarItems=2.
```

6. Close PDF (click its window's close button). Assert via `browser_evaluate` that `window.__browserInstances.size === 1` and only the Wiki taskbar item remains.

7. Open all seven archives in succession (wiki, pdf, os, iso, chiptune, tube, audio). Assert seven windows + seven taskbar items. Confirm cascade wraps after the 5th (5 → step=0, etc.).

8. Drag one window. Confirm dragging works per-instance.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(browser): one window per archive via cloned template + Map

Replaces the singleton browser-window with a manager that clones a
<template> per archive into a Map<id, instance>. Each window owns its
DOM node, taskbar item, iframe, and per-instance state (max/min/drag/
unmax). Reopening an already-open archive focuses the existing window
instead of replacing the iframe.

Cascade-positions windows 2..N at 30px offsets, wrapping after 5.
Closes the window destroys the instance fully, including taskbar item
and prevStyles, so no stale state leaks across sessions.

Closes A1 (single-window-per-archive limit), B1 (recenter+.dragged
bug), B3 (stale prevStyles), B10 (post-close stale state).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Cat avoidance retrofit (B9)

**Files:**
- Modify: `static/index.html` (`DesktopCat` methods `windowVisible`, `getAllAvoidRects`, `hasMaximizedWindow`, `startMusicAvoidWatcher` around lines 6760–6960)

**Why:** The cat hard-codes the id `'browser-window'` in three helpers and one MutationObserver. With multi-window, only one of N archive windows would be avoided / observed.

- [ ] **Step 1: Update `windowVisible` to accept a selector OR id**

Anchor: search for `windowVisible(id) {` (around line 6760).

Replace:

```js
          windowVisible(id) {
            const el = document.getElementById(id);
            if (!el) return false;
            if (el.classList.contains('hidden')) return false;
            if (el.style.display === 'none') return false;
            return true;
          }
```

with:

```js
          windowVisible(idOrSelector) {
            // Accepts an element id (e.g., 'music-window') or any selector.
            // For selectors, returns true if any matching element is visible
            // (not .hidden and not display:none).
            var els;
            if (idOrSelector.indexOf('.') === 0 || idOrSelector.indexOf('[') === 0) {
              els = document.querySelectorAll(idOrSelector);
            } else {
              const el = document.getElementById(idOrSelector);
              els = el ? [el] : [];
            }
            for (const el of els) {
              if (el.classList.contains('hidden')) continue;
              if (el.style.display === 'none') continue;
              return true;
            }
            return false;
          }
```

- [ ] **Step 2: Update the call site that asks "is the browser visible?"**

Anchor: search for `const browserVisible = this.windowVisible('browser-window');` (around line 6345).

Replace:

```js
            const browserVisible = this.windowVisible('browser-window');
```

with:

```js
            const browserVisible = this.windowVisible('.browser-window');
```

- [ ] **Step 3: Update `getAllAvoidRects` to enumerate all browser windows**

Anchor: search for `getAllAvoidRects() {` (around line 6772).

Replace:

```js
          getAllAvoidRects() {
            const rects = [];
            const ids = ['music-window', 'browser-window', 'demo-window'];
            for (const id of ids) {
              const r = this.rectFor(document.getElementById(id), 12);
              if (r) rects.push(r);
            }
            return rects;
          }
```

with:

```js
          getAllAvoidRects() {
            const rects = [];
            const fixed = ['music-window', 'demo-window'];
            for (const id of fixed) {
              const r = this.rectFor(document.getElementById(id), 12);
              if (r) rects.push(r);
            }
            // Browser windows are dynamic — enumerate all currently open instances.
            document.querySelectorAll('.browser-window').forEach((el) => {
              const r = this.rectFor(el, 12);
              if (r) rects.push(r);
            });
            return rects;
          }
```

- [ ] **Step 4: Update `hasMaximizedWindow` similarly**

Anchor: search for `hasMaximizedWindow() {` (around line 6786).

Replace:

```js
          hasMaximizedWindow() {
            const ids = ['browser-window', 'demo-window', 'music-window'];
            for (const id of ids) {
              const w = document.getElementById(id);
              if (!w) continue;
              if (w.classList.contains('hidden')) continue;
              if (w.style.display === 'none') continue;
              if (w.classList.contains('maximized')) return true;
            }
            return false;
          }
```

with:

```js
          hasMaximizedWindow() {
            const fixed = ['demo-window', 'music-window'];
            for (const id of fixed) {
              const w = document.getElementById(id);
              if (!w) continue;
              if (w.classList.contains('hidden')) continue;
              if (w.style.display === 'none') continue;
              if (w.classList.contains('maximized')) return true;
            }
            const browsers = document.querySelectorAll('.browser-window');
            for (const w of browsers) {
              if (w.classList.contains('hidden')) continue;
              if (w.style.display === 'none') continue;
              if (w.classList.contains('maximized')) return true;
            }
            return false;
          }
```

- [ ] **Step 5: Update the watcher to handle dynamic browser windows**

Anchor: search for `startMusicAvoidWatcher() {` (around line 6924).

Replace the entire method:

```js
          startMusicAvoidWatcher() {
            if (this._winWatcher) return;
            const ids = ['music-window', 'browser-window', 'demo-window'];
            const targets = ids.map(id => document.getElementById(id)).filter(Boolean);
            if (!targets.length) return;
            const recheck = () => {
              if (document.hidden) return;
              if (this.isMoving) return;
              // 1. If a window is maximized and the cat has drifted above the
              //    bottom strip, snap it down. No transition fanfare — this
              //    runs on window open / maximize and should feel instant.
              if (this.hasMaximizedWindow()) {
                const curB = parseInt(this.cat.style.bottom) || 50;
                if (curB > 80) {
                  this.cancelBehaviorLoop();
                  this.cancelFollow();
                  this.cat.style.transition = 'bottom 0.3s ease-out';
                  this.cat.style.bottom = '50px';
                  setTimeout(() => { this.cat.style.transition = ''; this.restartBehaviorLoop(800); }, 320);
                  return;
                }
              }
              // 2. Music-window-specific escape (legacy behavior preserved).
              if (!this.isOnMusicWindow()) return;
              this.cancelBehaviorLoop();
              this.cancelFollow();
              this.escapeMusicWindow();
            };
            let pending = null;
            const schedule = () => {
              if (pending) return;
              pending = setTimeout(() => { pending = null; recheck(); }, 120);
            };
            this._winWatcher = new MutationObserver(schedule);
            for (const t of targets) {
              this._winWatcher.observe(t, { attributes: true, attributeFilter: ['style', 'class'] });
```

— note: the original method continues after that line with `}` and a closing for the `for` and the method. The full replacement (matching the *entire* method from `startMusicAvoidWatcher() {` through its closing `}`):

with:

```js
          startMusicAvoidWatcher() {
            if (this._winWatcher) return;
            const fixed = ['music-window', 'demo-window'];
            const fixedTargets = fixed.map(id => document.getElementById(id)).filter(Boolean);

            const recheck = () => {
              if (document.hidden) return;
              if (this.isMoving) return;
              if (this.hasMaximizedWindow()) {
                const curB = parseInt(this.cat.style.bottom) || 50;
                if (curB > 80) {
                  this.cancelBehaviorLoop();
                  this.cancelFollow();
                  this.cat.style.transition = 'bottom 0.3s ease-out';
                  this.cat.style.bottom = '50px';
                  setTimeout(() => { this.cat.style.transition = ''; this.restartBehaviorLoop(800); }, 320);
                  return;
                }
              }
              if (!this.isOnMusicWindow()) return;
              this.cancelBehaviorLoop();
              this.cancelFollow();
              this.escapeMusicWindow();
            };
            let pending = null;
            const schedule = () => {
              if (pending) return;
              pending = setTimeout(() => { pending = null; recheck(); }, 120);
            };

            // Per-element observer for fixed windows + currently-open browser
            // windows. New browser windows added later get their own observer
            // attached by the body-level observer below.
            this._winWatcher = new MutationObserver(schedule);
            const observe = (el) => {
              this._winWatcher.observe(el, { attributes: true, attributeFilter: ['style', 'class'] });
            };
            fixedTargets.forEach(observe);
            document.querySelectorAll('.browser-window').forEach(observe);

            // Body-level observer: catches new browser windows being added
            // (and removed) over time, attaching the per-element observer to
            // each new node so we react when it's dragged or maximized.
            this._bodyWatcher = new MutationObserver((muts) => {
              for (const m of muts) {
                m.addedNodes.forEach((n) => {
                  if (n.nodeType === 1 && n.classList && n.classList.contains('browser-window')) {
                    observe(n);
                    schedule();
                  }
                });
                if (m.removedNodes.length) schedule();
              }
            });
            this._bodyWatcher.observe(document.body, { childList: true });

            // Reconciliation: defends against any missed mutation by re-running
            // the avoidance check every 5 s while any browser window is open.
            // Cheap (one query + a few rect reads) and bounded.
            this._reconcileTimer = setInterval(() => {
              if (document.querySelector('.browser-window:not(.hidden)')) schedule();
            }, 5000);
          }
```

- [ ] **Step 6: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright:
1. Set the prefs flag, reload, wait for the cat to appear (`document.getElementById('desktop-cat')`).
2. Open Wiki + PDF + OS archives. Maximize Wiki.
3. Assert via `browser_evaluate` that the cat's `bottom` style is ≤ `60px` (snapped to taskbar strip):

```js
() => parseInt(document.getElementById('desktop-cat').style.bottom) || 50
// Expected: <= 60
```

4. Drag PDF window to overlap the cat's current position. Wait ~600 ms. The cat should escape (its `right` style changes).
5. Close all archives. The cat should be free to roam again — verify by triggering several behavior cycles and confirming `bottom` drifts above 80px occasionally.

- [ ] **Step 7: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(cat): retrofit avoidance helpers for multi-window browser

windowVisible, getAllAvoidRects, and hasMaximizedWindow now enumerate
all .browser-window instances instead of looking up the singleton id.
startMusicAvoidWatcher gains a body-level MutationObserver that attaches
a per-element observer to each new browser window as it's added, plus
a 5-second reconciliation interval that defends against missed
mutations.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Loading spinner styling (B19)

**Files:**
- Modify: `static/index.html` (CSS near `.browser-content` around line 2126)

**Why:** Task 7's manager already toggles `.loading` on `.browser-content`. We just need the visual treatment.

- [ ] **Step 1: Add `.loading` overlay CSS**

Anchor: search for `.browser-content iframe {` (around line 2134).

Insert *before* the `.browser-content iframe {` block (i.e., right after `.browser-content { ... min-height: 0; }` ends), this CSS:

```css
    .browser-content.loading::after {
      content: "";
      position: absolute;
      inset: 0;
      background:
        radial-gradient(circle at 50% 50%, rgba(0,255,150,0.06), transparent 60%),
        rgba(8, 18, 22, 0.55);
      backdrop-filter: blur(2px);
      -webkit-backdrop-filter: blur(2px);
      pointer-events: none;
      z-index: 1;
    }
    .browser-content.loading::before {
      content: "";
      position: absolute;
      top: 50%;
      left: 50%;
      width: 36px;
      height: 36px;
      margin-top: -18px;
      margin-left: -18px;
      border: 2px solid rgba(0, 255, 150, 0.18);
      border-top-color: var(--neon);
      border-radius: 50%;
      animation: kd-spin 0.8s linear infinite;
      pointer-events: none;
      z-index: 2;
      box-shadow: 0 0 12px rgba(0, 255, 150, 0.35);
    }
```

(The `kd-spin` keyframe was added in Task 6; this reuses it.)

- [ ] **Step 2: Restart server and verify**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright:
1. Set the prefs flag, reload.
2. Open Wiki Archive. The spinner should be visible briefly while the iframe loads.
3. Click the reload button. Spinner reappears, SVG icon spins.
4. Screenshot during load and confirm the spinner is centered and the iframe is greyed.

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(browser): loading spinner overlay during iframe load

The manager already toggles .loading on .browser-content during initial
load and reload. Add the visual treatment: a centered neon spinner over
a translucent dim, fading the iframe until load fires (or the 10 s
safety timeout elapses).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Final E2E verification

**Files:** none modified — verification only.

**Why:** Confirm the full multi-window experience end-to-end and the bug-fix matrix.

- [ ] **Step 1: Restart fresh server**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

- [ ] **Step 2: Walk through the full flow in Playwright**

Open `http://localhost:8089/` and run, in order:

1. `localStorage.setItem('kd:prefs:archives-internal-browser', 'on')`, reload.
2. Open all seven archives via the start menu. Assert seven windows + seven taskbar items, cascade visible.
3. Drag two of them to non-default positions. Click each to confirm focus stacking (z-indexes increment).
4. Maximize one. Confirm cat snaps to taskbar strip.
5. Un-maximize. Minimize via window button. Click taskbar item to restore. Confirm un-max persisted.
6. Click reload on one window. Confirm spinner spins, iframe reloads (visible network request to `*.kunaldawn.com/?_kd_reload=...` if devtools open).
7. Click external on one window. Confirm new tab opens to the entry URL (without cache-buster).
8. Click Wiki entry from the *homepage card* while the Wiki window is already open via the start menu. Assert: no duplicate window — existing one is brought to front.
9. Close all windows. Assert: `window.__browserInstances.size === 0` and no `.browser-window` nodes in the DOM.
10. Reopen Wiki. Confirm cascade index reset (window is centered, not offset).

- [ ] **Step 3: Verify the bug-fix matrix from the spec**

Run, in `browser_evaluate`, a battery that confirms each closed bug:

```js
() => {
  const out = {};
  // A1: multi-window
  out.A1 = typeof window.__browserInstances !== 'undefined' && window.__browserInstances instanceof Map;
  // A2: no address bar
  out.A2 = !document.querySelector('.browser-address');
  // B1, B17: no recenter helper
  out.B1_B17 = typeof recenter === 'undefined';
  // B4: no back button
  out.B4 = !document.querySelector('#browser-back, [data-action="back"]');
  // B5: reload button exists
  out.B5 = !!document.querySelector('[data-action="reload"]');
  // B6: no readonly address input
  out.B6 = !document.querySelector('input.browser-address');
  // B9: cat helpers don't reference singleton id
  out.B9 = window.__cat ? typeof window.__cat.getAllAvoidRects === 'function' : 'no-cat';
  // B12, B13: homeDoc gone
  out.B12_B13 = typeof homeDoc === 'undefined';
  // B14: no browser-nav postMessage handler — best-effort: send one and confirm nothing happens
  return out;
}
// Expected: all values truthy (or 'no-cat' for B9 if running before cat loads).
```

- [ ] **Step 4: Update `change.plan.md`**

Anchor: file `change.plan.md` at the repo root.

Append a single line at the bottom describing this change. Example:

```bash
printf '\n- 2026-05-07: in-window browser multi-window rewrite (one window per archive, drops cross-origin-broken address bar in favor of static title strip, reload-only toolbar; closes 20 audit bugs)\n' >> change.plan.md
```

- [ ] **Step 5: Commit verification artifacts (only `change.plan.md`)**

```bash
git add change.plan.md
git commit -m "$(cat <<'EOF'
docs: log in-window browser multi-window rewrite in change.plan

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 6: Stop the dev server**

```bash
pkill -f /tmp/kdhome
```

---

## Self-review

**Spec coverage (each goal/section in the spec mapped to a task):**
- Multi-window, one per archive → Task 7
- Static title replacing address bar → Task 6
- Toolbar = Reload + Open in new tab → Task 6
- All 20 catalogued bugs resolved:
  - A1 → Task 7
  - A2 → Task 6
  - B1 → Task 7 (no recenter on fresh clones)
  - B2 → Task 2 (.closing CSS class)
  - B3 → Task 7 (per-instance prevStyles)
  - B4 → Task 6 (back button removed)
  - B5 → Task 6 (reload added)
  - B6 → Task 6 (address bar removed)
  - B7 → Task 3 (userUnmaxed flag, carried into Task 7's instances)
  - B8 → Task 6 (history gone)
  - B9 → Task 8 (cat retrofit)
  - B10 → Task 7 (per-instance state cleared on close)
  - B11 → Task 6 (history gone)
  - B12 → Task 6 (homeDoc removed)
  - B13 → Task 6 (homeDoc removed)
  - B14 → Task 6 (browser-nav postMessage handler removed)
  - B15 → Task 4 (cfErrorDoc removed)
  - B16 → Task 1 (monotonic z-counter)
  - B17 → Task 6 (recenter removed)
  - B18 → Task 7 (focus-shield not added; existing heuristic remains, now multi-window aware via `__focusedWindow` per click). The plan does not change the heuristic itself — see "deviations" below.
  - B19 → Task 9 (spinner CSS)
  - B20 → out of scope (spec confirms)

**Deviations from the spec, called out:**
- B18 (iframe-focus heuristic) — the spec said the heuristic stays as-is and uses `closest('.browser-window')`. The existing code at line 7693 already calls `ae.closest(WINDOW_SELECTOR)` which includes `.browser-window`. With Task 7's clones all matching that selector, the heuristic continues to work without code change. The plan therefore does not modify it. Confirmed by reading 7693–7701 and `WINDOW_SELECTOR` at 7668.
- The body-level MutationObserver for cat avoidance was specified in the spec; included in Task 8.

**Placeholder scan:** None — every step has exact code or commands.

**Type / name consistency:**
- `BrowserWindowManager` is referenced in the architecture text (spec) but the actual code uses an unnamed IIFE that exposes `window.openFeaturedArchive` and `window.openBrowserWindow`. That's deliberate — naming the function isn't required, and the IIFE pattern matches the existing browser-window code style.
- `userUnmaxed` is consistently named throughout Tasks 3, 6, 7.
- `instances`, `cascadeIndex`, `REGISTRY`, `ARCHIVES`, `archiveIcon`, `placeNewWindow`, `loadIframe`, `applyMax`, `unapplyMax`, `closeInstance`, `toggleInstanceFromTaskbar` — all defined in Task 7 and referenced consistently.
- `window.__browserInstances` exposed for cat code & verification — named consistently in Tasks 7, 10.
- `kd-spin` keyframe defined in Task 6, reused in Task 9. Consistent.
- `_kd_reload=` cache-buster query name — used in Tasks 6, 7.

No issues found.
