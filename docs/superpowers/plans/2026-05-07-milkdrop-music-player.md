# Milkdrop Music Player Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the home-page chiptune player's 19 hand-rolled canvas viz modes with a butterchurn-powered Milkdrop visualizer adapted from the kopyparty integration.

**Architecture:** Self-contained IIFE module at `static/music/kd-visualizer.js` that wraps butterchurn (~170 presets in 2 packs), exposing `window.kdVisualizer` and consuming `window.kdMusic` for player hand-off. Lazy-loads deps on first play. Existing audio chain (`ScriptProcessor → gainNode → analyser → destination`) is unchanged — butterchurn taps the chiptune source node via `viz.connectAudio()`. The 19 old modes plus their ~400 lines of canvas drawing code are removed entirely.

**Tech Stack:** Vanilla JS (ES5-style, no build step), WebGL via butterchurn, Web Audio API, single Go binary serving `static/`. No test framework — all verification is via manual browser testing against a running dev instance.

**Spec:** `docs/superpowers/specs/2026-05-07-milkdrop-music-player-design.md`

---

## Notes for the implementer

- **No test framework.** This codebase has no JS or Go test suite. Each task ends with a manual verification step against a running server. Read the verification carefully — it tells you what to actually click in the browser.
- **Run server with:** `docker compose up --build` (binds `:8888` → container `:8080`). Or `go run main.go` from repo root if you have Go + sqlite dev libs locally (`apk add gcc musl-dev sqlite-dev` in alpine, or distro equivalent). Either way, open `http://localhost:8888` (or `:8080` for go-run).
- **Reference implementation** at `/home/kunaldawn/workspace/repos/kopyparty/kopyparty/web/kd-visualizer.js` (read-only — do not modify). When a step says "adapt from kopyparty's `<function>`", read that function, port the logic to ES5-style JS, change selectors to use the new ids in this plan, drop kopyparty-specific bits (weekly presets, search dropdown, widget DOM grafting).
- **All JS is ES5-style** in this repo (`var`, function expressions, no arrow functions in the music player block — though arrows are used elsewhere). When in doubt, match the surrounding style.
- **Do NOT edit `main.go`.** Static files in `static/music/deps/` are served by the existing `http.FileServer`. The playlist handler at `main.go:123-161` skips directories and non-tracker extensions, so the `deps/` subdir is invisible to the playlist.
- **Editing the giant `index.html`** (~9237 lines): use `Edit` with enough surrounding context to make `old_string` unique. Many CSS / JS patterns repeat — anchor on adjacent unique strings. The single music IIFE starts at the `<script>` tag at line 7942 and ends at line 9234. Line numbers will shift as you edit — re-grep after each edit.

---

## File Structure

**New files:**
- `static/music/deps/butterchurn.min.js` — engine, ~192 KB, copy verbatim
- `static/music/deps/butterchurnPresets.min.js` — default pack ~120 presets, ~654 KB
- `static/music/deps/butterchurnPresetsExtra.min.js` — extra pack ~50 presets, ~845 KB
- `static/music/kd-visualizer.js` — new IIFE, ~400-450 lines

**Modified files:**
- `static/index.html` — DOM swap (~12 lines), CSS swap (~80 lines), removal of viz code (~400 lines), addition of `window.kdMusic` hand-off + 3 call-site changes (~25 lines), `<script>` tag (1 line).

---

## Task 1: Copy butterchurn dependencies

**Files:**
- Create: `static/music/deps/butterchurn.min.js`
- Create: `static/music/deps/butterchurnPresets.min.js`
- Create: `static/music/deps/butterchurnPresetsExtra.min.js`

- [ ] **Step 1: Create deps directory and copy files**

```bash
mkdir -p static/music/deps
cp /home/kunaldawn/workspace/repos/kopyparty/kopyparty/web/deps/butterchurn.min.js          static/music/deps/
cp /home/kunaldawn/workspace/repos/kopyparty/kopyparty/web/deps/butterchurnPresets.min.js   static/music/deps/
cp /home/kunaldawn/workspace/repos/kopyparty/kopyparty/web/deps/butterchurnPresetsExtra.min.js static/music/deps/
```

- [ ] **Step 2: Verify file sizes look sane**

```bash
ls -la static/music/deps/
```

Expected: 3 files, sizes roughly 192K / 654K / 845K.

- [ ] **Step 3: Verify playlist API still excludes them**

Start the server (`go run main.go` or `docker compose up --build`) and curl the playlist:

```bash
curl -s http://localhost:8080/api/playlist | head -c 400
```

Expected: only chiptune files (`.mod`, `.xm`, `.it` etc), no `.js` files.

- [ ] **Step 4: Commit**

```bash
git add static/music/deps/
git commit -m "viz: vendor butterchurn engine + 2 preset packs

Adds the WebGL Milkdrop port and ~170 bundled presets, copied from the
kopyparty repo. Lazy-loaded by the upcoming visualizer module on first
play, so they don't affect cold-page-load weight.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Visualizer module skeleton + script tag

**Files:**
- Create: `static/music/kd-visualizer.js`
- Modify: `static/index.html` (add `<script>` tag near the existing music scripts)

- [ ] **Step 1: Create the skeleton module**

Write `static/music/kd-visualizer.js`:

```js
// kd-visualizer.js — Milkdrop visualizer for the home-page chiptune
// player. Wraps butterchurn (the JS port of Milkdrop, same library
// Webamp uses). Adapted from the kopyparty integration.
//
// Consumes window.kdMusic (defined in index.html's music IIFE) for
// the AudioContext and current source node. Exposes window.kdVisualizer
// for the player to call on track change.

(function () {
    'use strict';

    var DEPS_BASE = '/music/deps/';

    // ---- module state ----
    var depsLoaded = false;
    var depsLoading = null;
    var viz = null;
    var canvas = null;
    var statusEl = null;
    var menuEl = null;
    var nameEl = null;
    var presets = null;
    var presetKeys = null;
    var presetIdx = 0;
    var connectedSrc = null;
    var rafId = null;
    var menuHideTimer = null;

    // auto-cycle
    var AUTO_INTERVAL_OPTIONS_S = [5, 10, 15, 30, 60, 120, 300];
    var autoIntervalIdx = 3; // default 30s
    var autoCycle = false;
    var autoTimer = null;

    // ---- public API stubs (real impls land in later tasks) ----
    function onAudioChanged() { /* Task 6 */ }
    function openMenu() { /* Task 8 */ }
    function closeMenu() { /* Task 8 */ }
    function toggleMenu() { /* Task 8 */ }
    function prev() { /* Task 8 */ }
    function next() { /* Task 8 */ }
    function random() { /* Task 8 */ }
    function setAutoCycle(on) { /* Task 10 */ }
    function setIntervalSec(s) { /* Task 10 */ }
    function toggleFullscreen() { /* Task 12 */ }
    function onResize() { /* Task 13 */ }

    window.kdVisualizer = {
        onAudioChanged: onAudioChanged,
        openMenu: openMenu,
        closeMenu: closeMenu,
        toggleMenu: toggleMenu,
        prev: prev,
        next: next,
        random: random,
        setAutoCycle: setAutoCycle,
        setInterval: setIntervalSec,
        toggleFullscreen: toggleFullscreen,
        onResize: onResize,
    };
})();
```

- [ ] **Step 2: Add `<script>` tag in index.html**

The two existing music scripts are at `static/index.html:7940-7941`:

```html
    <script src="/music/libopenmpt.js"></script>
    <script src="/music/chiptune2.js"></script>
```

Add a third line right after them:

```html
    <script defer src="/music/kd-visualizer.js"></script>
```

`defer` is important — the visualizer module references `document.getElementById` for elements created later in the body. With `defer`, the script runs after the DOM parses but the IIFE registers `window.kdVisualizer` before anything tries to call into it.

- [ ] **Step 3: Verify in browser**

Start server, open `http://localhost:8888/` (docker) or `http://localhost:8080/` (go run). Open DevTools console:

```js
typeof window.kdVisualizer
```

Expected: `'object'`. No JS errors in the console.

- [ ] **Step 4: Commit**

```bash
git add static/music/kd-visualizer.js static/index.html
git commit -m "viz: add kd-visualizer.js skeleton

Empty IIFE that registers window.kdVisualizer with stubbed methods.
Loaded by index.html via <script defer>. Real behavior lands in
subsequent commits.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Add `window.kdMusic` hand-off in index.html

**Files:**
- Modify: `static/index.html` (inside the music IIFE, near the top)

- [ ] **Step 1: Locate the insertion point**

```bash
grep -n "var chipTaskbar = document.getElementById('chiptunes-taskbar');" static/index.html
```

Expected: one line around 7975. The hand-off goes immediately after this line — at this point `player`, `isPlaying`, and `win` are all defined as outer-IIFE vars.

- [ ] **Step 2: Insert the hand-off**

Use Edit with `old_string`:

```js
      var chipTaskbar = document.getElementById('chiptunes-taskbar');
```

and `new_string`:

```js
      var chipTaskbar = document.getElementById('chiptunes-taskbar');

      // ─── Player → visualizer hand-off (consumed by kd-visualizer.js) ───
      window.kdMusic = {
        getContext: function () { return player ? player.context : null; },
        getSourceNode: function () { return player ? player.currentPlayingNode : null; },
        isPlaying: function () { return isPlaying; },
        isWindowVisible: function () {
          return !win.classList.contains('hidden') && win.style.display !== 'none';
        },
      };
```

- [ ] **Step 3: Verify in browser**

Reload the page, in DevTools console:

```js
window.kdMusic.getContext()        // null (no player yet)
window.kdMusic.isPlaying()         // false
window.kdMusic.isWindowVisible()   // true (music window is visible by default on desktop)
```

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "viz: expose window.kdMusic player hand-off

Pull-only API the upcoming visualizer queries for the AudioContext,
current source node, playing state, and window visibility. Read by
kd-visualizer.js; player code never imports anything back.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Swap the music-viz markup and CSS

**Files:**
- Modify: `static/index.html` (markup at ~5182, CSS at ~1620-1657)

- [ ] **Step 1: Replace the music-viz-wrap markup**

Use Edit with `old_string`:

```html
        <div class="music-viz-wrap" id="music-viz-wrap" data-mode="BARS" title="Click to change visualization">
          <div class="music-viz" id="music-viz"></div>
          <canvas class="music-viz-canvas" id="music-viz-canvas"></canvas>
        </div>
```

and `new_string`:

```html
        <div class="music-viz-wrap" id="music-viz-wrap" title="Click for presets">
          <canvas class="music-viz-canvas" id="music-viz-canvas"></canvas>
          <div class="music-viz-status" id="music-viz-status">click play…</div>
          <div class="music-viz-menu" id="music-viz-menu" hidden>
            <div class="mvm-name" id="mvm-name">—</div>
            <div class="mvm-row">
              <button class="mvm-btn" id="mvm-prev"  type="button" title="Prev preset (←)">‹</button>
              <button class="mvm-btn" id="mvm-rand"  type="button" title="Random preset (R)">⚄</button>
              <button class="mvm-btn" id="mvm-next"  type="button" title="Next preset (→)">›</button>
              <button class="mvm-btn" id="mvm-auto"  type="button" title="Auto-cycle (A) — long-press for interval">↻</button>
              <span class="mvm-iv" id="mvm-iv" hidden>30s</span>
              <button class="mvm-btn" id="mvm-fs"    type="button" title="Fullscreen (F)">⛶</button>
            </div>
            <div class="mvm-iv-pop" id="mvm-iv-pop" hidden>
              <button class="mvm-iv-opt" type="button" data-s="5">5s</button>
              <button class="mvm-iv-opt" type="button" data-s="10">10s</button>
              <button class="mvm-iv-opt" type="button" data-s="15">15s</button>
              <button class="mvm-iv-opt" type="button" data-s="30">30s</button>
              <button class="mvm-iv-opt" type="button" data-s="60">60s</button>
              <button class="mvm-iv-opt" type="button" data-s="120">2m</button>
              <button class="mvm-iv-opt" type="button" data-s="300">5m</button>
            </div>
          </div>
        </div>
```

- [ ] **Step 2: Replace the CSS rules**

Use Edit with `old_string`:

```css
    .music-viz-wrap {
      position: relative;
      height: 56px;
      margin-top: 6px;
      cursor: pointer;
    }

    .music-viz-wrap::after {
      content: attr(data-mode);
      position: absolute;
      top: 1px;
      right: 2px;
      font-size: 7px;
      color: rgba(0, 255, 150, 0.3);
      font-family: 'Share Tech Mono', monospace;
      pointer-events: none;
    }

    .music-viz {
      display: flex;
      align-items: flex-end;
      gap: 1px;
      height: 56px;
    }

    .music-viz-bar {
      flex: 1;
      background: var(--neon);
      opacity: 0.4;
      border-radius: 1px 1px 0 0;
      min-height: 2px;
    }

    .music-viz-canvas {
      width: 100%;
      height: 56px;
      display: none;
    }
```

and `new_string`:

```css
    .music-viz-wrap {
      position: relative;
      height: 56px;
      margin-top: 6px;
      cursor: pointer;
      overflow: hidden;
      background: rgba(0, 0, 0, 0.5);
      border-radius: 2px;
    }

    .music-viz-canvas {
      width: 100%;
      height: 100%;
      display: block;
    }

    .music-viz-status {
      position: absolute;
      top: 2px;
      right: 4px;
      font-size: 8px;
      color: rgba(0, 255, 150, 0.55);
      font-family: 'Share Tech Mono', monospace;
      pointer-events: none;
      text-shadow: 0 0 4px rgba(0, 0, 0, 0.8);
    }

    .music-viz-status:empty { display: none; }

    .music-viz-menu {
      position: absolute;
      left: 50%;
      bottom: 4px;
      transform: translateX(-50%);
      background: rgba(0, 0, 0, 0.78);
      border: 1px solid rgba(0, 255, 150, 0.25);
      border-radius: 4px;
      padding: 4px 6px;
      font-family: 'Share Tech Mono', monospace;
      color: var(--neon);
      backdrop-filter: blur(4px);
      max-width: calc(100% - 16px);
      box-shadow: 0 0 12px rgba(0, 255, 150, 0.15);
    }

    .music-viz-menu[hidden] { display: none; }

    .mvm-name {
      font-size: 9px;
      opacity: 0.8;
      max-width: 240px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      margin-bottom: 3px;
      text-align: center;
    }

    .mvm-row {
      display: flex;
      align-items: center;
      gap: 4px;
      justify-content: center;
    }

    .mvm-btn {
      background: rgba(0, 0, 0, 0.4);
      border: 1px solid rgba(0, 255, 150, 0.2);
      color: var(--neon);
      font-family: inherit;
      font-size: 12px;
      padding: 2px 8px;
      border-radius: 3px;
      cursor: pointer;
      line-height: 1;
      transition: background 0.1s, border-color 0.1s;
    }

    .mvm-btn:hover { background: rgba(0, 255, 150, 0.15); border-color: rgba(0, 255, 150, 0.5); }
    .mvm-btn.on { background: rgba(0, 255, 150, 0.25); border-color: var(--neon); }

    .mvm-iv {
      font-size: 9px;
      opacity: 0.7;
      padding: 0 2px;
    }

    .mvm-iv-pop {
      display: flex;
      gap: 3px;
      margin-top: 4px;
      flex-wrap: wrap;
      justify-content: center;
    }

    .mvm-iv-pop[hidden] { display: none; }

    .mvm-iv-opt {
      background: rgba(0, 0, 0, 0.4);
      border: 1px solid rgba(0, 255, 150, 0.2);
      color: var(--neon);
      font-family: inherit;
      font-size: 9px;
      padding: 1px 5px;
      border-radius: 2px;
      cursor: pointer;
    }

    .mvm-iv-opt:hover { background: rgba(0, 255, 150, 0.15); }
    .mvm-iv-opt.on { background: rgba(0, 255, 150, 0.25); border-color: var(--neon); }

    .music-viz-wrap.fullscreen {
      width: 100vw;
      height: 100vh;
      max-width: none;
      max-height: none;
      border-radius: 0;
    }
```

- [ ] **Step 3: Reload and visually verify**

Open `http://localhost:8888/`. The music-viz area should be a dark rectangle with `click play…` text in the corner. The bar visualization is gone (player is broken in this intermediate state — that's fine, fixed in Task 5). No menu visible (it's hidden). No JS errors.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "viz: swap music-viz markup + CSS to webgl shell

Replaces the BARS div + canvas with a single canvas plus a hidden
preset-menu overlay and a transient status pill. Drops the data-mode
chip and the bar/canvas display toggling. Old viz code is still in
place but no longer connected — next commit removes it.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Remove the old visualization code

This task deletes ~400 lines and rewires the call sites that used to drive them. After this task, the player works correctly (audio plays) but the canvas is blank — the visualizer module will fill it in subsequent tasks.

**Files:**
- Modify: `static/index.html` (multiple locations across lines ~7977-9210)

- [ ] **Step 1: Re-grep to get current line numbers**

Line numbers in this task drift as you edit. Before each removal, run:

```bash
grep -n "vizMode\|sizeCanvas\|vizLoop\|startViz\|stopViz\|NUM_BARS\|vizCtx\|vizContainer\|vizBars" static/index.html
```

- [ ] **Step 2: Delete the old viz-state declarations**

Use Edit. `old_string`:

```js
      var vizWrap = document.getElementById('music-viz-wrap');
      var vizCanvas = document.getElementById('music-viz-canvas');
      var vizCtx = vizCanvas.getContext('2d');
      var vizModes = ['BARS','SCOPE','MIRROR','DOTS','RING','FLAME','GRID','HELIX','STARS','WAVE3D','PIXEL','SINE','HEX','TUNNEL','PLASMA','KALEIDO','WARP','NEBULA','VORTEX'];
      var vizModeIdx = 0;
      var waveData = null; // for oscilloscope modes
      var mdFrame = 0; // milkdrop frame counter
      var mdFeedback = null; // offscreen canvas for feedback effects

      var NUM_BARS = 24;
      for (var i = 0; i < NUM_BARS; i++) {
        var bar = document.createElement('div');
        bar.className = 'music-viz-bar';
        vizContainer.appendChild(bar);
        vizBars.push(bar);
      }

      // Click viz to cycle modes
      vizWrap.addEventListener('click', function() {
        vizModeIdx = (vizModeIdx + 1) % vizModes.length;
        var mode = vizModes[vizModeIdx];
        vizWrap.setAttribute('data-mode', mode);
        // Reset feedback canvas and frame counter on mode switch
        mdFeedback = null;
        mdFrame = 0;
        if (vizCanvas.width > 0) vizCtx.clearRect(0, 0, vizCanvas.width, vizCanvas.height);
        if (mode === 'BARS') {
          vizContainer.style.display = 'flex';
          vizCanvas.style.display = 'none';
        } else {
          vizContainer.style.display = 'none';
          vizCanvas.style.display = 'block';
          requestAnimationFrame(sizeCanvas);
        }
      });
```

`new_string`:

```js
```

(Delete the whole block — that single empty line replaces it.)

- [ ] **Step 3: Delete the now-orphaned `vizContainer` / `vizBars` declarations**

Earlier in the IIFE there's also a `var vizContainer = document.getElementById('music-viz');` and `var vizBars = [];` near the audio variable declarations. Re-grep:

```bash
grep -n "vizContainer\|vizBars\|freqData\|var analyser" static/index.html
```

Use Edit. `old_string`:

```js
      var analyser = null;
      var gainNode = null;
      var freqData = null;
      var vizBars = [];
      var vizRAF = null;
```

`new_string`:

```js
      var analyser = null;
      var gainNode = null;
      var freqData = null;
```

(Drops `vizBars` and `vizRAF` — neither needed anymore. `analyser` and `freqData` stay per spec.)

Then find:

```bash
grep -n "vizContainer = document" static/index.html
```

Use Edit. `old_string`:

```js
      var vizContainer = document.getElementById('music-viz');
```

`new_string`:

```js
```

(Delete the line.)

- [ ] **Step 4: Replace `startViz()` / `stopViz()` definitions**

Re-grep:

```bash
grep -n "function startViz\|function stopViz" static/index.html
```

Use Edit. `old_string`:

```js
      function startViz() { if (vizRAF) cancelAnimationFrame(vizRAF); sizeCanvas(); vizLoop(); }
      function stopViz() {
        if (vizRAF) { cancelAnimationFrame(vizRAF); vizRAF = null; }
        for (var i = 0; i < NUM_BARS; i++) { vizBars[i].style.height = '2px'; vizBars[i].style.opacity = '0.2'; }
        // Clear canvas too
        if (vizCanvas.width > 0) vizCtx.clearRect(0, 0, vizCanvas.width, vizCanvas.height);
      }
```

`new_string`:

```js
```

(Delete both functions.)

- [ ] **Step 5: Delete the `sizeCanvas()` function**

Re-grep:

```bash
grep -n "function sizeCanvas" static/index.html
```

Read 8-15 lines around the match (the function is small). Use Edit to delete the entire function body.

- [ ] **Step 6: Delete the `vizLoop()` function (huge)**

Re-grep:

```bash
grep -n "function vizLoop\|// ─── Real FFT Visualization" static/index.html
```

Find the start (`// ─── Real FFT Visualization ───` comment + `function vizLoop() {`). The function ends at the matching `}` for that opening brace — read the file from the function start until you find the close. The function is ~870 lines. Use Edit to delete from the comment line through the closing `}`.

To make the Edit unique, anchor with surrounding context — include the previous function's closing `}` and the next sibling line in `old_string`, e.g.:

```bash
grep -n "function vizLoop\|function sizeCanvas\|^      // ─── Controls ───" static/index.html
```

Then read between the `// ─── Real FFT Visualization ───` line and the line just before `// ─── Controls ───` (or whichever comment heads the next block). Delete the whole vizLoop function and the comment header.

After this Edit, the file should shrink by ~400 lines. Run `wc -l static/index.html` and confirm.

- [ ] **Step 7: Replace `startViz()` / `stopViz()` call sites**

Re-grep:

```bash
grep -n "startViz()\|stopViz()" static/index.html
```

There are call sites in `closeChiptunes()`, `loadAndPlay()`, the play-button handler. For each:

- `stopViz();` in `closeChiptunes()` → **delete** (the visibility gate in the render loop pauses rendering automatically).
- `startViz();` in `loadAndPlay()` (end of the function, after the analyser-chain wiring) → replace with `if (window.kdVisualizer) window.kdVisualizer.onAudioChanged();`.
- `stopViz();` in the play button's pause branch → **delete**.
- `startViz();` in the play button's resume branch → **delete** (the source didn't change; the visibility/playing gate flips on automatically when `isPlaying` becomes `true`).

Use Edit for each one. Example:

`old_string`:
```js
          updateInfo(); renderPlaylist(); startViz();
```

`new_string`:
```js
          updateInfo(); renderPlaylist();
          if (window.kdVisualizer) window.kdVisualizer.onAudioChanged();
```

`old_string`:
```js
          btn.innerHTML = SVG_PLAY; btn.classList.remove('playing'); btn.classList.add('idle-pulse'); stopViz();
```

`new_string`:
```js
          btn.innerHTML = SVG_PLAY; btn.classList.remove('playing'); btn.classList.add('idle-pulse');
```

`old_string`:
```js
          btn.innerHTML = SVG_PAUSE; btn.classList.add('playing'); btn.classList.remove('idle-pulse'); startViz();
```

`new_string`:
```js
          btn.innerHTML = SVG_PAUSE; btn.classList.add('playing'); btn.classList.remove('idle-pulse');
```

`old_string` (in `closeChiptunes`):
```js
          stopViz();
          seekSlider.value = 0; timeCur.textContent = '0:00';
```

`new_string`:
```js
          if (window.kdVisualizer) window.kdVisualizer.onAudioChanged();
          seekSlider.value = 0; timeCur.textContent = '0:00';
```

(Notify the visualizer that the source is gone — `onAudioChanged` will see `getSourceNode()` returning null and quietly skip the rewire.)

- [ ] **Step 8: Replace `setTimeout(sizeCanvas, 50)` in `maximizeChiptunes()`**

Re-grep:

```bash
grep -n "setTimeout(sizeCanvas" static/index.html
```

Two matches inside `maximizeChiptunes()` (one in each branch of the maximize/restore conditional). For each:

`old_string`:
```js
          setTimeout(sizeCanvas, 50);
```

`new_string`:
```js
          setTimeout(function () {
            if (window.kdVisualizer && window.kdVisualizer.onResize) window.kdVisualizer.onResize();
          }, 50);
```

Use `replace_all: true` since both instances are identical.

- [ ] **Step 9: Final grep — no leftover references**

```bash
grep -n "vizModes\|vizModeIdx\|vizLoop\|sizeCanvas\|startViz\|stopViz\|vizBars\|vizContainer\|vizCtx\|NUM_BARS\|mdFrame\|mdFeedback\|waveData" static/index.html
```

Expected: **no output**. If anything matches, find and remove it.

- [ ] **Step 10: Verify in browser**

Reload the page. Open the music window, hit play. Expected:
- Music plays.
- Music-viz canvas is **blank** (dark rectangle). The status pill still says `click play…` (visualizer module hasn't been wired yet).
- No JS errors in console.
- The `console.log(window.kdMusic.isPlaying())` returns `true` while a track is playing.
- Closing the music window stops audio cleanly.

- [ ] **Step 11: Commit**

```bash
git add static/index.html
git commit -m "viz: remove old 19-mode canvas visualizer

Drops vizModes/vizLoop/sizeCanvas/startViz/stopViz and the BARS div
machinery (~400 lines). Replaces the start/stop call sites with
kdVisualizer.onAudioChanged() hand-offs. The maximize button now
notifies the visualizer to resize its GL viewport via onResize().

After this commit the canvas is blank — the visualizer module fills
it in starting next commit. Player audio is unaffected.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Implement deps loader and audio-changed plumbing

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Add the deps loader**

Inside the IIFE in `kd-visualizer.js`, **above** the public API stubs, add:

```js
    function loadScript(src) {
        return new Promise(function (resolve, reject) {
            var s = document.createElement('script');
            s.src = src;
            s.onload = function () { resolve(); };
            s.onerror = function () { reject(new Error('failed to load ' + src)); };
            document.head.appendChild(s);
        });
    }

    function loadDeps() {
        if (depsLoaded) return Promise.resolve();
        if (depsLoading) return depsLoading;
        // Engine first, then both preset packs in parallel. Tolerate
        // a single pack failing — we just have fewer presets.
        depsLoading = loadScript(DEPS_BASE + 'butterchurn.min.js').then(function () {
            return Promise.all([
                loadScript(DEPS_BASE + 'butterchurnPresets.min.js')
                    .catch(function (e) { console.warn(e); }),
                loadScript(DEPS_BASE + 'butterchurnPresetsExtra.min.js')
                    .catch(function (e) { console.warn(e); })
            ]);
        }).then(function () {
            if (!window.butterchurnPresets) throw new Error('butterchurnPresets unavailable');
            depsLoaded = true;
        });
        return depsLoading;
    }
```

- [ ] **Step 2: Add early DOM lookup**

Inside the IIFE, near the bottom (right before the `window.kdVisualizer` assignment), add:

```js
    function findEls() {
        canvas = document.getElementById('music-viz-canvas');
        statusEl = document.getElementById('music-viz-status');
        menuEl = document.getElementById('music-viz-menu');
        nameEl = document.getElementById('mvm-name');
    }

    function setStatus(text) {
        if (statusEl) statusEl.textContent = text || '';
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', findEls);
    } else {
        findEls();
    }
```

- [ ] **Step 3: Implement `onAudioChanged` to load deps**

Replace the stub:

```js
    function onAudioChanged() { /* T6 */ }
```

with:

```js
    function onAudioChanged() {
        // Called from index.html on track-load and on stop.
        // First call (with a non-null source) triggers dep load + viz init.
        // Subsequent calls just rewire the audio source.
        if (!window.kdMusic) return;
        var src = window.kdMusic.getSourceNode();
        if (!src) {
            // stop()'d — leave viz alone, the render loop's gate handles silence
            return;
        }
        if (depsLoaded && viz) {
            connectAudioSource(src);
            return;
        }
        setStatus('loading viz…');
        loadDeps().then(function () {
            // ensureViz / connectAudio land in Task 7
            setStatus('viz not yet impl');
        }).catch(function (e) {
            console.warn('kdVisualizer load failed:', e && e.message || e);
            setStatus('viz unavailable');
        });
    }

    function connectAudioSource(src) {
        if (!viz || !src || src === connectedSrc) return;
        try {
            viz.connectAudio(src);
            connectedSrc = src;
        } catch (e) {
            console.warn('kdVisualizer connectAudio failed:', e);
        }
    }
```

- [ ] **Step 4: Verify in browser**

Reload, open DevTools → Network tab, filter to "JS". Click play in the music window. Expected:
- Three new requests: `butterchurn.min.js`, `butterchurnPresets.min.js`, `butterchurnPresetsExtra.min.js`. All `200 OK`.
- Status pill text changes to `loading viz…` then to `viz not yet impl`.
- Pressing play on a different track does NOT re-fetch deps (already loaded).

- [ ] **Step 5: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: load butterchurn deps lazily on first play

onAudioChanged() now triggers a one-shot fetch of the engine and the
two preset packs the first time the user starts playback. Status pill
shows progress / failure. Subsequent track changes just rewire the
audio source via viz.connectAudio (engine init lands next commit).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Initialize the visualizer + render loop

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Implement `resizeCanvas` and `ensureViz`**

In `kd-visualizer.js`, above the public-API stubs, add:

```js
    function resizeCanvas() {
        if (!canvas) return;
        var r = canvas.getBoundingClientRect();
        var dpr = window.devicePixelRatio || 1;
        var w = Math.max(64, Math.floor(r.width));
        var h = Math.max(64, Math.floor(r.height));
        var W = Math.floor(w * dpr);
        var H = Math.floor(h * dpr);
        if (canvas.width !== W || canvas.height !== H) {
            canvas.width = W;
            canvas.height = H;
            if (viz && typeof viz.setRendererSize === 'function') {
                try { viz.setRendererSize(W, H); } catch (e) {}
            }
        }
    }

    function ensureViz() {
        if (viz) return true;
        if (!window.kdMusic) return false;
        var ctx = window.kdMusic.getContext();
        if (!ctx) return false;
        if (!window.butterchurn || !window.butterchurnPresets) return false;
        if (!canvas) return false;

        resizeCanvas();

        try {
            var bc = window.butterchurn.default || window.butterchurn;
            viz = bc.createVisualizer(ctx, canvas, {
                width: canvas.width,
                height: canvas.height,
                pixelRatio: window.devicePixelRatio || 1,
                textureRatio: 1
            });
        } catch (e) {
            console.warn('kdVisualizer createVisualizer failed:', e);
            setStatus('webgl unavailable');
            return false;
        }

        // Merge the two bundled packs into a single dict.
        var packs = [window.butterchurnPresets, window.butterchurnPresetsExtra];
        presets = {};
        for (var i = 0; i < packs.length; i++) {
            var pack = packs[i];
            if (!pack) continue;
            var p = pack.default || pack;
            var dict = (typeof p.getPresets === 'function') ? p.getPresets() : p;
            if (dict && typeof dict === 'object') {
                for (var k in dict) {
                    if (Object.prototype.hasOwnProperty.call(dict, k))
                        presets[k] = dict[k];
                }
            }
        }
        presetKeys = Object.keys(presets).sort();
        if (!presetKeys.length) {
            setStatus('no presets');
            return false;
        }
        presetIdx = Math.floor(Math.random() * presetKeys.length);
        applyPreset(0);
        setStatus('');
        return true;
    }

    function applyPreset(blendSec) {
        if (!viz || !presetKeys || !presetKeys.length) return;
        var key = presetKeys[presetIdx];
        var blend = typeof blendSec === 'number' ? blendSec : 1.5;
        try { viz.loadPreset(presets[key], blend); }
        catch (e) { console.warn('kdVisualizer applyPreset failed:', e); }
        if (nameEl) {
            var pretty = key.replace(/^[^-]+ - /, '');
            nameEl.textContent = pretty.length > 70 ? pretty.slice(0, 67) + '…' : pretty;
        }
    }
```

- [ ] **Step 2: Implement the render loop**

Add (still above the stubs):

```js
    function renderLoop() {
        rafId = requestAnimationFrame(renderLoop);
        if (!viz) return;
        if (!window.kdMusic) return;
        if (!window.kdMusic.isWindowVisible()) return;
        if (!window.kdMusic.isPlaying()) return;
        if (document.visibilityState !== 'visible') return;
        try { viz.render(); } catch (e) {}
    }
```

- [ ] **Step 3: Wire `onAudioChanged` to actually start the visualizer**

Replace the body of `onAudioChanged` (currently sets status to "viz not yet impl"):

```js
    function onAudioChanged() {
        if (!window.kdMusic) return;
        var src = window.kdMusic.getSourceNode();
        if (!src) return;
        if (depsLoaded && viz) {
            connectAudioSource(src);
            return;
        }
        setStatus('loading viz…');
        loadDeps().then(function () {
            if (!ensureViz()) return; // setStatus already called by ensureViz on failure
            connectAudioSource(src);
            if (rafId === null) renderLoop();
        }).catch(function (e) {
            console.warn('kdVisualizer load failed:', e && e.message || e);
            setStatus('viz unavailable');
        });
    }
```

- [ ] **Step 4: Verify in browser**

Reload, hit play. Expected:
- Status pill shows `loading viz…` briefly, then disappears.
- Canvas displays a moving Milkdrop visualization that reacts to the audio.
- Switching tracks via the prev/next buttons keeps the viz running (new audio source rewires).
- Pausing the player → viz freezes on last frame.
- Resuming → viz continues animating.
- Closing the music window → audio stops; canvas stays on its last frame (CPU is idle because the visibility gate fires).
- Switching browser tab → viz stops rendering (rAF still ticks, but `viz.render()` is gated by `document.visibilityState`).

If the canvas is blank with no errors: open DevTools → Console, run `window.butterchurn` to confirm it loaded. If `undefined`, the dep-load failed silently — re-check Task 6.

- [ ] **Step 5: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: initialize butterchurn and start rendering

ensureViz() creates the WebGL visualizer, merges the two packs into a
single ~170-preset dict, and picks a random starter. Render loop is a
single rAF gated on player visibility, isPlaying, and the document's
visibility state — CPU drops to idle when the player is hidden,
paused, or the tab is backgrounded.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Preset menu UI — toggle, prev/next/random, click-outside, auto-hide

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Implement preset stepping**

Replace the `prev`, `next`, `random` stubs:

```js
    function prev() {
        if (!presetKeys || !presetKeys.length) return;
        presetIdx = (presetIdx - 1 + presetKeys.length) % presetKeys.length;
        applyPreset();
        scheduleAutoCycle();
    }
    function next() {
        if (!presetKeys || !presetKeys.length) return;
        presetIdx = (presetIdx + 1) % presetKeys.length;
        applyPreset();
        scheduleAutoCycle();
    }
    function random() {
        if (!presetKeys || presetKeys.length < 2) {
            if (presetKeys && presetKeys.length === 1) applyPreset();
            return;
        }
        var prevIdx = presetIdx;
        do {
            presetIdx = Math.floor(Math.random() * presetKeys.length);
        } while (presetIdx === prevIdx);
        applyPreset();
        scheduleAutoCycle();
    }

    // scheduleAutoCycle is implemented in Task 10; provide a no-op stub
    // for now so prev/next/random work without auto-cycle.
    var scheduleAutoCycle = function () {};
```

- [ ] **Step 2: Implement menu open/close/toggle**

Replace the `openMenu`, `closeMenu`, `toggleMenu` stubs:

```js
    function openMenu() {
        if (!menuEl) return;
        menuEl.hidden = false;
        resetMenuHideTimer();
    }
    function closeMenu() {
        if (!menuEl) return;
        menuEl.hidden = true;
        if (menuHideTimer) { clearTimeout(menuHideTimer); menuHideTimer = null; }
        // also collapse the interval popover (added in Task 12)
        var pop = document.getElementById('mvm-iv-pop');
        if (pop) pop.hidden = true;
    }
    function toggleMenu() {
        if (!menuEl) return;
        if (menuEl.hidden) openMenu(); else closeMenu();
    }
    function resetMenuHideTimer() {
        if (menuHideTimer) clearTimeout(menuHideTimer);
        menuHideTimer = setTimeout(function () {
            if (menuEl && !menuEl.hidden) closeMenu();
        }, 4000);
    }
    function isMenuOpen() {
        return !!(menuEl && !menuEl.hidden);
    }
```

- [ ] **Step 3: Wire DOM events**

Extend `findEls` (or add a new function called from the same DOM-ready block):

```js
    function bindMenuEvents() {
        if (!canvas || !menuEl) return;

        // click on canvas → toggle menu
        canvas.addEventListener('click', function (e) {
            if (!viz) return; // viz not initialized — ignore
            e.stopPropagation();
            toggleMenu();
        });

        // click anywhere outside the menu (and not on the canvas which already toggled) → close
        document.addEventListener('click', function (e) {
            if (!isMenuOpen()) return;
            if (menuEl.contains(e.target)) return;
            if (e.target === canvas) return;
            closeMenu();
        }, true);

        // hover/click inside menu refreshes the auto-hide timer
        menuEl.addEventListener('mousemove', resetMenuHideTimer);
        menuEl.addEventListener('click', resetMenuHideTimer);

        var bind = function (id, fn) {
            var el = document.getElementById(id);
            if (el) el.addEventListener('click', function (e) {
                e.preventDefault();
                e.stopPropagation();
                fn();
                resetMenuHideTimer();
            });
        };
        bind('mvm-prev', prev);
        bind('mvm-next', next);
        bind('mvm-rand', random);
        // mvm-auto / mvm-fs wired in Task 11/13
    }
```

Update the DOM-ready block:

```js
    function init() {
        findEls();
        bindMenuEvents();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
```

(Replace the previous `findEls`-only block.)

- [ ] **Step 4: Verify in browser**

Reload, play music, wait for viz to start. Then:
- Click on the canvas → menu pops up at the bottom showing the current preset name and `‹ ⚄ › ↻ ⛶`.
- Click `›` → preset changes, name updates.
- Click `‹` → previous preset.
- Click `⚄` → random preset.
- Click outside the canvas (e.g. on the playlist) → menu closes.
- Open menu, don't touch it for 4 seconds → menu auto-closes.
- Open menu, hover inside → timer resets (still open at 4s+).

- [ ] **Step 5: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: preset menu UI with prev/next/random

Click on the canvas toggles a small overlay with the current preset
name and prev/random/next buttons. Menu auto-hides after 4s of no
interaction; click-outside also dismisses it. Auto-cycle and
fullscreen buttons are present in markup but not yet functional.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Keyboard shortcuts

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Add focus-gated keydown listener**

Append to `bindMenuEvents` (or as a sibling function called from `init`):

```js
    function bindKeyboard() {
        document.addEventListener('keydown', function (e) {
            // Only when the music window is the focused window. The
            // existing window manager in index.html sets
            // window.__focusedWindow on click/focus.
            var win = document.getElementById('music-window');
            if (!win) return;
            if (window.__focusedWindow !== win) return;
            // Ignore typing in inputs.
            if (e.target && /^(input|textarea|select)$/i.test(e.target.tagName)) return;
            if (!viz) return;

            if (e.key === 'Escape') {
                if (document.fullscreenElement) {
                    try { document.exitFullscreen(); } catch (_) {}
                } else if (isMenuOpen()) {
                    closeMenu();
                } else {
                    return; // don't preventDefault if we did nothing
                }
            } else if (e.key === 'ArrowLeft') {
                prev();
            } else if (e.key === 'ArrowRight') {
                next();
            } else if (e.key === 'r' || e.key === 'R') {
                random();
            } else if (e.key === 'a' || e.key === 'A') {
                setAutoCycle(!autoCycle);
            } else if (e.key === 'f' || e.key === 'F') {
                toggleFullscreen();
            } else {
                return;
            }
            e.preventDefault();
        });
    }
```

Call it from `init`:

```js
    function init() {
        findEls();
        bindMenuEvents();
        bindKeyboard();
    }
```

- [ ] **Step 2: Verify in browser**

Reload, play music. Click on the music window (don't focus an input). Press:
- `←` / `→` — preset changes.
- `R` — random preset.
- `Esc` while menu is open — menu closes.
- `Esc` while menu is closed and not fullscreen — nothing happens, doesn't trap typing.

`A` and `F` will silently no-op for now (auto / fullscreen unimplemented). That's expected.

Then click in some text input elsewhere on the page (e.g. browser address bar) and try `R` — preset must NOT change.

- [ ] **Step 3: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: keyboard shortcuts (←/→/R/Esc) gated to focused player

Active only when the music window is the focused window per the
existing window manager. Typing in inputs is ignored. A/F bindings
present but no-op until auto-cycle and fullscreen land.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Auto-cycle (toggle + persistence)

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Hydrate persisted state**

Right after the auto-cycle var declarations near the top of the IIFE, add:

```js
    try {
        if (window.localStorage) {
            var savedAuto = window.localStorage.getItem('kd_music_viz_auto');
            if (savedAuto === '1') autoCycle = true;
            var savedIv = window.localStorage.getItem('kd_music_viz_interval');
            if (savedIv) {
                var n = parseInt(savedIv, 10);
                var idx = AUTO_INTERVAL_OPTIONS_S.indexOf(n);
                if (idx >= 0) autoIntervalIdx = idx;
            }
        }
    } catch (e) {}
```

- [ ] **Step 2: Replace the `scheduleAutoCycle` stub with the real one**

Find:

```js
    var scheduleAutoCycle = function () {};
```

Replace with:

```js
    // Always tears the timer down and rebuilds. Manual nudges
    // (prev/next/random) call this so the user's input doesn't get
    // immediately overridden by the timer firing.
    function scheduleAutoCycle() {
        if (autoTimer) { clearInterval(autoTimer); autoTimer = null; }
        if (!autoCycle) return;
        if (!viz) return;
        if (window.kdMusic && !window.kdMusic.isWindowVisible()) return;
        var ms = AUTO_INTERVAL_OPTIONS_S[autoIntervalIdx] * 1000;
        autoTimer = setInterval(function () {
            if (!viz) return;
            if (window.kdMusic && !window.kdMusic.isWindowVisible()) return;
            if (presetKeys && presetKeys.length > 1) {
                var prevIdx = presetIdx;
                do {
                    presetIdx = Math.floor(Math.random() * presetKeys.length);
                } while (presetIdx === prevIdx);
                applyPreset(2.5);
            }
        }, ms);
    }
```

- [ ] **Step 3: Replace `setAutoCycle` and `setIntervalSec` stubs**

```js
    function setAutoCycle(on) {
        autoCycle = !!on;
        try {
            if (window.localStorage)
                window.localStorage.setItem('kd_music_viz_auto', autoCycle ? '1' : '0');
        } catch (e) {}
        updateAutoUI();
        scheduleAutoCycle();
    }

    function setIntervalSec(s) {
        var idx = AUTO_INTERVAL_OPTIONS_S.indexOf(s);
        if (idx < 0) return;
        autoIntervalIdx = idx;
        try {
            if (window.localStorage)
                window.localStorage.setItem('kd_music_viz_interval', String(s));
        } catch (e) {}
        // changing interval implicitly turns auto on
        if (!autoCycle) {
            setAutoCycle(true);
        } else {
            updateAutoUI();
            scheduleAutoCycle();
        }
    }

    function updateAutoUI() {
        var btn = document.getElementById('mvm-auto');
        var iv = document.getElementById('mvm-iv');
        if (btn) {
            if (autoCycle) btn.classList.add('on'); else btn.classList.remove('on');
        }
        if (iv) {
            iv.hidden = !autoCycle;
            iv.textContent = fmtIntervalLabel(AUTO_INTERVAL_OPTIONS_S[autoIntervalIdx]);
        }
    }

    function fmtIntervalLabel(s) {
        return s < 60 ? (s + 's') : ((s / 60) + 'm');
    }
```

- [ ] **Step 4: Wire the auto button**

In `bindMenuEvents`, after the prev/next/rand bindings, add:

```js
        bind('mvm-auto', function () { setAutoCycle(!autoCycle); });
```

In `init`, after `bindKeyboard()`, add:

```js
        updateAutoUI();
```

so the button reflects persisted state on page load.

Also: when `ensureViz` first succeeds, kick off the cycle (it's a no-op if `autoCycle` is false). Inside `ensureViz`, after `applyPreset(0)`:

```js
        scheduleAutoCycle();
```

- [ ] **Step 5: Verify in browser**

Reload, play music, wait for viz.
- Click the `↻` button → it lights up (`.on` class), `30s` chip appears next to it. Wait 30 seconds → preset changes with a smooth blend.
- Click `↻` again → unlit, no more auto-changes.
- Press `A` → toggles same way.
- Reload page, click play, open menu → `↻` is in the persisted state from before. After the blend interval, a new preset loads.
- Hide the music window (close it), wait → no preset cycling happens (verify by inspecting `autoTimer` in console: should be `null` after close).

- [ ] **Step 6: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: auto-cycle with localStorage persistence

Toggle via the ↻ button or A keybinding. Defaults to off; persists to
localStorage (kd_music_viz_auto / kd_music_viz_interval). Cycle is
gated on visualizer state and music-window visibility, so a closed
or minimized player burns no timer ticks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Auto-cycle interval submenu (long-press / right-click)

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Wire the submenu open / close**

In `bindMenuEvents`, replace the existing `bind('mvm-auto', ...)` binding with a richer set of handlers:

```js
        var autoBtn = document.getElementById('mvm-auto');
        var ivPop = document.getElementById('mvm-iv-pop');
        if (autoBtn && ivPop) {
            var pressTimer = null;
            var longPress = false;

            function openIvPop() {
                ivPop.hidden = false;
                // mark the currently selected interval
                var opts = ivPop.querySelectorAll('.mvm-iv-opt');
                for (var i = 0; i < opts.length; i++) {
                    var s = parseInt(opts[i].getAttribute('data-s'), 10);
                    if (s === AUTO_INTERVAL_OPTIONS_S[autoIntervalIdx]) opts[i].classList.add('on');
                    else opts[i].classList.remove('on');
                }
                resetMenuHideTimer();
            }

            autoBtn.addEventListener('mousedown', function () {
                longPress = false;
                pressTimer = setTimeout(function () { longPress = true; openIvPop(); }, 400);
            });
            autoBtn.addEventListener('mouseup', function () {
                if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; }
            });
            autoBtn.addEventListener('mouseleave', function () {
                if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; }
            });
            autoBtn.addEventListener('contextmenu', function (e) {
                e.preventDefault();
                openIvPop();
            });
            autoBtn.addEventListener('click', function (e) {
                e.preventDefault();
                e.stopPropagation();
                if (longPress) { longPress = false; return; } // long-press already handled
                setAutoCycle(!autoCycle);
                resetMenuHideTimer();
            });

            // Interval option clicks
            var opts = ivPop.querySelectorAll('.mvm-iv-opt');
            for (var i = 0; i < opts.length; i++) {
                opts[i].addEventListener('click', function (e) {
                    e.preventDefault();
                    e.stopPropagation();
                    var s = parseInt(this.getAttribute('data-s'), 10);
                    if (!isNaN(s)) setIntervalSec(s);
                    ivPop.hidden = true;
                    resetMenuHideTimer();
                });
            }
        }
```

(Make sure to **remove** the previous one-line `bind('mvm-auto', …)` from Task 10 step 4 — the new block replaces it.)

- [ ] **Step 2: Verify in browser**

Reload, play music, open menu.
- Long-press (≥0.4s) the `↻` button → interval popover appears with `5s 10s 15s 30s 60s 2m 5m`; `30s` is highlighted.
- Right-click the button → same popover.
- Click `60s` → popover closes, button lights up (auto turned on), chip shows `60s`. Reload → chip still says `60s`.
- Short-click the button → toggles auto on/off without opening the popover.

- [ ] **Step 3: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: auto-cycle interval submenu via long-press

Holding ↻ for 400ms (or right-clicking) reveals a 7-option interval
chooser. Picking an interval implicitly enables auto-cycle and
persists the choice. Short-click keeps its existing toggle behavior.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Fullscreen toggle

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Implement `toggleFullscreen`**

Replace the stub:

```js
    function toggleFullscreen() {
        var wrap = document.getElementById('music-viz-wrap');
        if (!wrap) return;
        var fs = document.fullscreenElement || document.webkitFullscreenElement;
        if (fs) {
            try { (document.exitFullscreen || document.webkitExitFullscreen).call(document); } catch (e) {}
        } else {
            var req = wrap.requestFullscreen || wrap.webkitRequestFullscreen;
            if (!req) return;
            req.call(wrap).then(function () {
                setTimeout(resizeCanvas, 200);
            }).catch(function (e) { console.warn('fullscreen denied:', e); });
        }
    }

    function onFsChange() {
        var wrap = document.getElementById('music-viz-wrap');
        if (!wrap) return;
        var fs = document.fullscreenElement || document.webkitFullscreenElement;
        if (fs === wrap) wrap.classList.add('fullscreen');
        else wrap.classList.remove('fullscreen');
        // give the layout a tick to settle, then resize the GL viewport
        setTimeout(resizeCanvas, 60);
    }
```

- [ ] **Step 2: Wire button + fs change listener**

In `bindMenuEvents`, after the auto-button block, add:

```js
        bind('mvm-fs', toggleFullscreen);
        document.addEventListener('fullscreenchange', onFsChange);
        document.addEventListener('webkitfullscreenchange', onFsChange);
```

- [ ] **Step 3: Verify in browser**

Reload, play music, open menu.
- Click `⛶` → viz takes over the screen. Resolution looks crisp (no upscale blur). Audio keeps playing.
- Press `Esc` → exits fullscreen, viz returns to its normal size in the player window.
- Press `F` while music window is focused → enters fullscreen.

- [ ] **Step 4: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: fullscreen toggle on the canvas wrap

⛶ button and F keybinding call requestFullscreen on .music-viz-wrap.
The .fullscreen CSS rule (added with the markup swap) sizes it to
fill the viewport. resizeCanvas runs after both enter and exit so
the GL viewport matches the new layout pixel-for-pixel.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Resize handling

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Implement `onResize` and the debounced window listener**

Replace the `onResize` stub:

```js
    function onResize() {
        resizeCanvas();
    }

    var resizeT = 0;
    window.addEventListener('resize', function () {
        if (!viz) return;
        clearTimeout(resizeT);
        resizeT = setTimeout(resizeCanvas, 120);
    });
```

(Place the listener at the bottom of the IIFE, near the other `window.addEventListener` calls if any, otherwise just before the public-API block.)

- [ ] **Step 2: Verify in browser**

Reload, play music, open menu.
- Resize the browser window → canvas pixel size keeps up (no stretched/blurry pixels after a brief debounce).
- Click `maximize` (the green window button) → canvas grows to fill the maximized music window. Click again to restore → canvas shrinks back.
- Move the music window via its titlebar → no flicker.

- [ ] **Step 3: Commit**

```bash
git add static/music/kd-visualizer.js
git commit -m "viz: track canvas size on window + maximize resizes

Public onResize() (called from index.html's maximizeChiptunes after
the layout settles) plus a 120ms-debounced window-resize listener
keep the GL viewport pixel-aligned with the canvas's CSS size.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Failure-path polish

**Files:**
- Modify: `static/music/kd-visualizer.js`

- [ ] **Step 1: Status pill cleanup on success**

In `ensureViz`, the success path already calls `setStatus('')` after `applyPreset(0)`. Confirm that's there.

- [ ] **Step 2: Failure messages are already wired** in Task 7's `ensureViz` (`'webgl unavailable'` on createVisualizer throw, `'no presets'` if both packs were empty) and in Task 6's `onAudioChanged` catch (`'viz unavailable'` on dep load failure). Verify both by tampering tests below.

- [ ] **Step 3: Tampering test — simulate dep failure**

Edit `kd-visualizer.js` temporarily: in `loadDeps`, change the engine URL to a bogus one:

```js
        depsLoading = loadScript(DEPS_BASE + 'butterchurn-bogus.min.js').then(function () {
```

Reload, click play. Expected:
- Status pill shows `loading viz…` then transitions to `viz unavailable`.
- Music plays normally.
- No JS errors that crash the player (one `console.warn` is fine).

Revert the change.

- [ ] **Step 4: Tampering test — simulate WebGL failure**

In `ensureViz`, temporarily replace the `bc.createVisualizer(...)` line with `throw new Error('synthetic webgl fail');` to force the catch path. Reload, play. Expected:
- Status pill ends on `webgl unavailable`.
- Clicking the canvas does nothing (toggleMenu is a no-op when `viz` is null — verify this via the `if (!viz) return;` in `bindMenuEvents`'s click handler).
- Music plays normally.

Revert the change.

- [ ] **Step 5: Commit (verification-only commit if no code changed)**

If this task only confirmed existing behavior without changing code, skip the commit. If you needed to add a missing `if (!viz) return;` guard or similar, commit those.

---

## Task 15: Manual end-to-end smoke test

**Files:** none (verification only)

- [ ] **Step 1: Walk through the spec's testing checklist**

Run through each item in the spec under `## Testing` (lines 1-11). For each, briefly note PASS or describe what failed.

The 11 items, summarized:
1. Smoke (load, play, viz renders, audio normal).
2. Preset menu (click → opens, prev/next/rand work, click-outside closes, 4s auto-hide).
3. Auto-cycle (toggle on, blend after interval, off stops it).
4. Track change (preset stays, audio rewires across tracks).
5. Window hide / show (rAF gate stops viz.render when hidden).
6. Maximize (canvas grows to fill, no blur).
7. Fullscreen (⛶ takes over, Esc exits).
8. Keyboard (←/→/R/A/F work when focused, ignored in inputs).
9. Persistence (auto + interval survive reload).
10. Failure mode — bad dep URL → `viz unavailable`.
11. No-WebGL fallback — disable WebGL → `webgl unavailable`, player still works.

- [ ] **Step 2: If anything fails, file a follow-up**

For any FAIL: either fix it in a small follow-up task on this branch, or write up a short bug note and decide with the user whether to ship as-is.

- [ ] **Step 3: Final commit (optional)**

If steps 1-2 needed any small fixes:

```bash
git add -p
git commit -m "viz: smoke-test polish ($brief description)"
```

---

## Self-review summary

- **Spec coverage:**
  - File layout → Task 1 (deps), Task 2 (module skeleton + script), Task 4 (DOM/CSS), Task 5 (removals).
  - `window.kdMusic` → Task 3.
  - `window.kdVisualizer` API → Task 2 (stubs), Tasks 6-13 (real impls).
  - Audio routing → Task 6 (`connectAudioSource`).
  - Lifecycle → Task 6 (lazy load), Task 7 (init + render loop with the spec's exact gate), Task 5 (call-site rewiring).
  - DOM & CSS → Task 4.
  - UI behavior (click, menu, auto-hide, status pill states) → Tasks 8, 14.
  - Auto-cycle (toggle + interval submenu) → Tasks 10, 11.
  - Keyboard shortcuts → Task 9.
  - Status pill states → Tasks 6, 7, 14.
  - Failure handling → Task 14 + already-wired catches in Tasks 6 / 7.
  - Testing checklist → Task 15.

- **No placeholders / type drift:** function names are consistent across tasks (`scheduleAutoCycle`, `setAutoCycle`, `setIntervalSec`, `applyPreset`, `connectAudioSource`, `ensureViz`, `resizeCanvas`, `renderLoop`, `onAudioChanged`, `onResize`, `toggleFullscreen`, `onFsChange`, `prev`, `next`, `random`, `openMenu`, `closeMenu`, `toggleMenu`, `isMenuOpen`, `resetMenuHideTimer`, `findEls`, `init`, `bindMenuEvents`, `bindKeyboard`, `setStatus`, `updateAutoUI`, `fmtIntervalLabel`, `loadScript`, `loadDeps`). DOM ids consistent (`music-viz-wrap`, `music-viz-canvas`, `music-viz-status`, `music-viz-menu`, `mvm-name`, `mvm-prev`, `mvm-next`, `mvm-rand`, `mvm-auto`, `mvm-fs`, `mvm-iv`, `mvm-iv-pop`, `mvm-iv-opt`).

- **Bite-sized:** every task is 4-10 small steps, each step is a single action or short code block.
