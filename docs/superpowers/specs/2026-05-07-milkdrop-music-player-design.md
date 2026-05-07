# Milkdrop visualizer for the home page music player

**Status:** Design — approved
**Date:** 2026-05-07

## Goal

Replace the music player's 19 hand-rolled canvas visualization modes
(`BARS`, `SCOPE`, `MIRROR`, `DOTS`, `RING`, `FLAME`, `GRID`, `HELIX`,
`STARS`, `WAVE3D`, `PIXEL`, `SINE`, `HEX`, `TUNNEL`, `PLASMA`, `KALEIDO`,
`WARP`, `NEBULA`, `VORTEX`) with a Milkdrop-style WebGL visualizer
powered by [butterchurn], the same library Webamp uses and that the
sibling `kopyparty` repo already integrates.

[butterchurn]: https://github.com/jberg/butterchurn

## Non-goals

- Changing the music player's controls (play/pause/seek/shuffle/loop/playlist).
- Changing the audio playback chain (still libopenmpt → ScriptProcessor).
- Backend changes (`main.go` already serves `static/`).
- Replicating kopyparty's "weekly preset" lazy-fetch system (552 extra
  presets served from individual JSON files). The bundled packs are
  enough variety for a home-page widget.
- Replicating kopyparty's preset search dropdown.
- Server-side preset storage or per-user preset preferences beyond
  what `localStorage` already gives us.

## Reference implementation

`/home/kunaldawn/workspace/repos/kopyparty/kopyparty/web/kd-visualizer.js`
(748 lines) contains a working butterchurn integration. We adapt that
file — strip out the weekly-preset pipeline, the preset search
dropdown, the `pctl`/`widget` injection logic specific to kopyparty's
DOM, and the dual-source audio routing for browser-native audio (we
only have chiptune playback). Result: ~400-450 lines.

## File layout

### New files

```
static/music/deps/butterchurn.min.js              (engine, ~280 KB)
static/music/deps/butterchurnPresets.min.js       (~120 default presets)
static/music/deps/butterchurnPresetsExtra.min.js  (~50 extra presets)
static/music/kd-visualizer.js                     (new IIFE module)
```

The three `.min.js` deps are copied verbatim from
`/home/kunaldawn/workspace/repos/kopyparty/kopyparty/web/deps/`.
We bundle two packs (~170 presets total) — the original `Presets`
and `PresetsExtra`. `PresetsExtra2` and `PresetsMD1` are skipped to
keep the payload trim.

### Modified files

- `static/index.html` — see "Changes to index.html" below.

### Untouched files

- `main.go`, `Dockerfile`, `docker-compose.yml`, all other static
  assets.

## Architecture

### Player ↔ visualizer integration

The visualizer is a self-contained IIFE that exposes one global
(`window.kdVisualizer`) and consumes one global from the player
(`window.kdMusic`).

```text
┌──────────────────────────┐         ┌──────────────────────────────┐
│  index.html music block  │         │   static/music/              │
│                          │         │     kd-visualizer.js         │
│  exposes window.kdMusic  │ ───┐    │                              │
│                          │    └──▶ │  reads window.kdMusic        │
│  calls                   │         │                              │
│  kdVisualizer            │ ◀────── │  exposes                     │
│    .onAudioChanged()     │         │    window.kdVisualizer       │
│    on play / stop        │         │                              │
└──────────────────────────┘         └──────────────────────────────┘
```

#### `window.kdMusic` (player → visualizer hand-off)

Defined inside the existing IIFE in `index.html`. Pure pull API —
the visualizer queries it on demand.

```js
window.kdMusic = {
    getContext:      function () { return player ? player.context : null; },
    getSourceNode:   function () { return player ? player.currentPlayingNode : null; },
    isPlaying:       function () { return isPlaying; },
    isWindowVisible: function () {
        return !win.classList.contains('hidden') && win.style.display !== 'none';
    },
};
```

#### `window.kdVisualizer` (visualizer → player hand-off)

```js
window.kdVisualizer = {
    onAudioChanged: function () { /* rewire viz.connectAudio */ },
    openMenu:       function () { /* show preset menu overlay */ },
    closeMenu:      function () { /* hide preset menu overlay */ },
    toggleMenu:     function () { /* … */ },
    prev:           function () { /* prev preset */ },
    next:           function () { /* next preset */ },
    random:         function () { /* random preset */ },
    setAutoCycle:   function (on) { /* persist + start/stop timer */ },
    setInterval:    function (seconds) { /* persist + restart timer */ },
    toggleFullscreen: function () { /* Fullscreen API on viz wrap */ },
    onResize:       function () { /* re-read bbox, call viz.setRendererSize */ },
};
```

Player code calls `window.kdVisualizer && window.kdVisualizer.onAudioChanged()`
at exactly two points (existing locations in `loadAndPlay()` and
`closeChiptunes()`). Calls are guarded so the player works even if the
visualizer script fails to load.

### Audio routing

butterchurn's `viz.connectAudio(node)` taps an AudioNode without
inserting itself into the chain. The existing chain is unchanged:

```text
chiptune ScriptProcessor (player.currentPlayingNode)
    → gainNode  → analyser  → audioContext.destination
                                      ▲
                                      └── viz.connectAudio(player.currentPlayingNode)
```

The pre-existing `analyser` (FFT for the old bar viz) is **kept** —
butterchurn doesn't need it but keeping it costs ~nothing and leaves
room for future non-milkdrop UI (e.g. a small spectrum strip in the
window titlebar). `analyser.fftSize` may be lowered to the default
`512` since `256` was tuned for the 24-bar BARS mode that's going away.

### Lifecycle

1. **Page load.** `<script defer src="/music/kd-visualizer.js">` evaluates
   the IIFE. It registers DOM hooks (click handlers on the new menu,
   keyboard listener with focus gate) but **does not** load butterchurn
   deps. `window.kdVisualizer` is now defined.

2. **First `onAudioChanged()`** (user pressed play, music window is
   open). Visualizer:
   - Shows `"loading viz…"` in the `.music-viz-status` pill.
   - Calls `loadDeps()` — sequential `butterchurn.min.js` first, then
     parallel `butterchurnPresets.min.js` + `butterchurnPresetsExtra.min.js`.
   - On resolve, calls `ensureViz()`:
     - Reads context via `kdMusic.getContext()`.
     - Resizes the canvas to its bounding-rect × devicePixelRatio.
     - `bc.createVisualizer(ctx, canvas, { width, height, pixelRatio, textureRatio: 1 })`.
     - Merges the two preset packs into a single `presets` dict.
     - Picks a random preset key; calls `viz.loadPreset(presets[key], 0)` (no blend on first).
     - Calls `viz.connectAudio(kdMusic.getSourceNode())`.
     - Hides the status pill.
     - Starts `renderLoop()`.
   - On reject: status pill shows `"viz unavailable"`, no retry. The
     rest of the player keeps working.

3. **Subsequent `onAudioChanged()`** (track change, stop). Re-reads
   `kdMusic.getSourceNode()` and calls `viz.connectAudio(node)` if
   non-null. **Does NOT change preset** (per design decision: "never
   auto-swap on track change"). If the source is null (after `stop()`),
   simply leaves the last source connected — butterchurn falls silent
   when the source goes silent, no extra cleanup needed.

4. **Render loop.** Single `requestAnimationFrame` loop. Each tick:
   ```js
   if (viz
       && kdMusic.isWindowVisible()
       && kdMusic.isPlaying()
       && document.visibilityState === 'visible') {
       viz.render();
   }
   ```
   The rAF tick keeps running so resume is instant; only `viz.render()`
   is gated. CPU-idle when the player is hidden, paused, or the tab is
   in the background. (Practical consequence: when paused, the viz
   freezes on the last frame even if auto-cycle is on. Auto-cycle's
   internal preset-swap timer keeps ticking but blends are invisible
   until rendering resumes — acceptable for a paused player.)

5. **Auto-cycle.** Off by default. When on:
   - `setInterval` at the chosen interval randomly picks a new preset
     and calls `viz.loadPreset(p, 2.5)` (2.5s blend).
   - Manual prev/next/random restarts the timer (so a user nudge
     doesn't immediately get overridden).
   - Pauses when music window is hidden; resumes on show.
   - State and interval persisted to `localStorage` keys
     `kd_music_viz_auto` (`'1'`/`'0'`) and `kd_music_viz_interval`
     (seconds, one of `5/10/15/30/60/120/300`).

6. **Window resize / maximize.** Visualizer hooks `window.resize`
   (debounced 120ms) and re-reads its bounding rect. The existing
   `maximizeChiptunes()` function in index.html also calls
   `window.kdVisualizer && window.kdVisualizer.onResize()` 50ms after
   toggling, so the GL viewport tracks the maximized layout.

## DOM & CSS

### Replaced markup (in `static/index.html`)

```html
<!-- OLD -->
<div class="music-viz-wrap" id="music-viz-wrap" data-mode="BARS" title="Click to change visualization">
  <div class="music-viz" id="music-viz"></div>
  <canvas class="music-viz-canvas" id="music-viz-canvas"></canvas>
</div>
```

becomes

```html
<!-- NEW -->
<div class="music-viz-wrap" id="music-viz-wrap" title="Click for presets">
  <canvas class="music-viz-canvas" id="music-viz-canvas"></canvas>
  <div class="music-viz-status" id="music-viz-status">click play…</div>
  <div class="music-viz-menu" id="music-viz-menu" hidden>
    <div class="mvm-name" id="mvm-name">—</div>
    <div class="mvm-row">
      <button class="mvm-btn" id="mvm-prev"  title="Prev preset (←)">‹</button>
      <button class="mvm-btn" id="mvm-rand"  title="Random preset (R)">⚄</button>
      <button class="mvm-btn" id="mvm-next"  title="Next preset (→)">›</button>
      <button class="mvm-btn" id="mvm-auto"  title="Auto-cycle (A)">↻</button>
      <button class="mvm-btn" id="mvm-fs"    title="Fullscreen (F)">⛶</button>
    </div>
  </div>
</div>
```

### Replaced styles

The existing block of rules covering `.music-viz` (BARS container),
`.music-viz-bar` (individual bars), and the `.music-window.maximized
.music-viz` carve-out goes away entirely. The `.music-viz-canvas` and
`.music-window.maximized .music-viz-canvas` rules stay (sized for the
WebGL canvas) and a small new block adds:

- `.music-viz-status` — small pill in the corner of the viz, hidden
  via `display: none` when empty.
- `.music-viz-menu` — overlay positioned over the canvas, hidden via
  `[hidden]`.
- `.mvm-name`, `.mvm-row`, `.mvm-btn`, `.mvm-btn.on` — small flex row
  of icon-buttons; `.on` lights up the auto-cycle button when active.
- `.music-viz-wrap.fullscreen` — when the wrap is the active
  fullscreen element, fill the screen; the existing fullscreen CSS
  rules from kopyparty are a fine reference.

### Removed JS (from `index.html`)

- `vizModes` array (line ~7980) and `vizModeIdx`.
- `mdFrame`, `mdFeedback`, `waveData` (variables for the old modes).
- `vizContainer` and the 24-bar creation loop (lines ~7986-7992).
- The click handler on `vizWrap` that cycled modes (lines ~7995-8011).
- `vizLoop()` and `sizeCanvas()` — the entire ~400-line block
  implementing the 19 modes (`BARS`/`SCOPE`/`MIRROR`/...). The
  visualizer module now manages its own canvas size and rAF loop.
- `startViz()` / `stopViz()` calls in `loadAndPlay()`, `pause()`,
  `closeChiptunes()` etc. — replaced with `onAudioChanged()`
  hand-offs as described above.

### Kept JS

- The audio chain wiring inside `loadAndPlay()`
  (`gainNode.connect(analyser); analyser.connect(destination)`).
- `analyser`, `freqData` — unused after this change but kept for the
  optional future titlebar spectrum.
- `vizWrap` reference — pointed at the same id, used only by the
  visualizer module via `document.getElementById('music-viz-wrap')`.
- `volSlider` / `seekSlider` / `loopBtn` / `shuffleBtn` and the rest
  of the player controls — untouched.

### Removed references in the music IIFE

- `vizCanvas` and `vizCtx` (the 2D context) — the visualizer module
  fetches the canvas itself by id; the music IIFE no longer touches
  it.
- The `getContext('2d')` call. Canvas type flips from 2D to WebGL on
  the visualizer side.

### Player → visualizer call sites

Inside the existing IIFE, in `loadAndPlay()`, after the analyser-chain
wiring block:

```js
if (window.kdVisualizer) window.kdVisualizer.onAudioChanged();
```

Inside `closeChiptunes()`, after `player.stop()`:

```js
if (window.kdVisualizer) window.kdVisualizer.onAudioChanged();
```

Inside `maximizeChiptunes()`, **replacing** the existing
`setTimeout(sizeCanvas, 50)` calls (both branches of the
maximize/restore conditional):

```js
setTimeout(function () {
    if (window.kdVisualizer && window.kdVisualizer.onResize) window.kdVisualizer.onResize();
}, 50);
```

The visualizer's `onResize()` re-reads the canvas bounding rect and
calls `viz.setRendererSize(w * dpr, h * dpr)` so the GL viewport
tracks the new layout.

## UI behavior

### Click on canvas

Toggles the preset menu overlay. The overlay sits centered-bottom of
the canvas. Clicking outside the overlay (anywhere else, including
the canvas itself behind the overlay) closes it. The overlay
auto-hides after 4 seconds of no interaction (timer resets on hover
or button click).

### Menu buttons

| Button | Action |
|---|---|
| `‹` prev | step preset index by −1 |
| `⚄` rand | jump to a random preset (different from current if `presetKeys.length > 1`) |
| `›` next | step preset index by +1 |
| `↻` auto | toggle auto-cycle. `.on` class when active. |
| `⛶` fs | call `requestFullscreen()` on `.music-viz-wrap`; if already fullscreen, `exitFullscreen()` |

The current preset name shows in `.mvm-name`, truncated to 70 chars
with an ellipsis (same logic as kopyparty's `applyPreset`).

### Auto-cycle interval submenu

Clicking the `↻` button **toggles** auto on/off. **Long-press** (≥400ms)
or **right-click** opens a small dropdown of intervals
(`5s / 10s / 15s / 30s / 60s / 2m / 5m`). Choosing an interval implicitly
turns auto on. The chosen value is shown next to the button as a small
chip (e.g. `30s`) when auto is on.

### Keyboard shortcuts

Active **only** when the music window is the focused window
(`window.__focusedWindow === document.getElementById('music-window')`,
which the existing window manager already tracks). Inactive when
typing in inputs.

| Key | Action |
|---|---|
| `←` | prev preset |
| `→` | next preset |
| `R` | random preset |
| `A` | toggle auto-cycle |
| `F` | toggle fullscreen |
| `Esc` | close menu (if open) or exit fullscreen |

### Status pill (`.music-viz-status`)

Shown in the top-right corner of the viz. Hidden via `display: none`
when text is empty. States:

- `"click play…"` — initial idle state, before first play. Hidden once
  playback starts (deps load shows next state).
- `"loading viz…"` — while butterchurn deps are downloading.
- `""` (hidden) — viz is running.
- `"webgl unavailable"` — `createVisualizer` threw or returned null.
- `"viz unavailable"` — deps failed to load (network error).

## Failure handling

- **WebGL / butterchurn init failure** — status pill shows
  `"webgl unavailable"`. Click on canvas does nothing
  (`toggleMenu` is a no-op when `viz` is null). All other player
  controls keep working. Music plays normally.
- **Single preset pack fails to load** — `console.warn`, continue with
  whatever loaded. If neither pack loaded, fail with `"viz unavailable"`.
- **Network error fetching deps** — `"viz unavailable"`, no retry.
- **`getSourceNode()` returns null when `onAudioChanged` is called** —
  no-op (skip `viz.connectAudio`). Next call when source is non-null
  will wire up.

## Testing

Manual testing in browser (no test framework in this repo):

1. **Smoke** — load `/`, open music window, hit play. Verify:
   - Status pill shows `"loading viz…"` briefly.
   - WebGL canvas starts rendering a milkdrop preset.
   - Audio plays normally.
2. **Preset menu** — click on canvas. Menu appears. Click `›` →
   preset changes. Click `⚄` → different preset. Click outside →
   menu closes. Wait 4s after opening → menu auto-hides.
3. **Auto-cycle** — click `↻`. After the configured interval (default
   30s) preset changes. Click `↻` again → stops.
4. **Track change** — let a track end (or click next). Verify viz
   keeps rendering across the gap, audio source rewires, **preset
   does not change**.
5. **Window hide / show** — minimize music window. Open dev tools,
   verify rAF callbacks no longer call `viz.render()`. Restore →
   rendering resumes.
6. **Maximize** — click maximize. Canvas resizes to fill the larger
   wrap. No stretched/blurry pixels.
7. **Fullscreen** — click `⛶`. Canvas takes over the screen at proper
   pixel ratio. `Esc` exits.
8. **Keyboard** — focus the music window, press `←/→/R/A/F`. Verify
   each works. Type in an input → keyboard nav doesn't fire.
9. **Persistence** — toggle auto on, change interval, reload. Verify
   auto-cycle resumes with the chosen interval.
10. **Failure modes** — temporarily break the script src
    (e.g. typo `butterchurn.min.js` → `xxx.min.js`) and verify the
    pill shows `"viz unavailable"` and the player still plays.
11. **No-WebGL fallback** — disable WebGL in browser flags. Verify
    `"webgl unavailable"` shows and player still works.

## Open questions

None — all surfaced during brainstorming were resolved.

## Out-of-scope follow-ups (not part of this work)

- Preset search dropdown (kopyparty has it; ~100 lines to port).
- "Weekly" 552-preset lazy-fetch system.
- Saving favorite presets / starring.
- A small FFT spectrum strip in the music titlebar using the kept
  `analyser`.
- Dropping the now-unused `analyser`/`freqData` if profiling shows
  cost.
