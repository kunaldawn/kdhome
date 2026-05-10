# Demoscene Engine v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 12 rendering bugs in the existing 26 demoscene effects and add 20 new effects (46 total) to the engine in `static/index.html`, with Playwright-verified screenshots for every scene.

**Architecture:** All work in a single closure: `window.createFxInstance` at `static/index.html` lines ~3890–5305. Each effect is a `draw*(now, dt)` function that mutates the shared `buf` Uint8Array; the scene director rotates them on a bag-shuffled timer. Per-frame allocations are hoisted to closure scope. A new `CHAR_ASPECT` constant replaces hardcoded `1.8`. A `ResizeObserver` rebuilds COLS/ROWS and lookup tables when the canvas changes size.

**Tech Stack:** Vanilla JS (ES6+) inside a `<script>` tag, no build step. Go 1.22 server (`main.go`) serves the static file. Playwright MCP (`mcp__playwright__*`) for verification — no test framework, no Node tooling.

**Spec:** `docs/superpowers/specs/2026-05-10-demoscene-engine-v2-design.md`

**Conventions:**

- Source line numbers reference the file *before* edits start. Lines drift after each task — use the surrounding code in `old_string` snippets as the authoritative anchor.
- "Restart server" step:
  ```
  pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
  sleep 1
  ```
- "Verify" steps assume the server is up on `http://localhost:8089/`.
- "Open Demo window" Playwright sequence (re-used across tasks):
  ```
  mcp__playwright__browser_navigate → http://localhost:8089/
  mcp__playwright__browser_evaluate → () => { const btn = document.querySelector('.entry-gate-btn'); if (btn) btn.click(); }
  mcp__playwright__browser_evaluate → () => window.openDemoWindow && window.openDemoWindow()
  // wait ~500ms for the window to open
  ```
- For per-scene verification, the entry-gate FX instance exposes debug hooks (`__fxJump`, `__fxFreeze`, `__fxList`). The Demo window FX instance does NOT (no `installDebug: true`). Tasks below verify against the entry-gate instance: dismiss the gate, then re-open the page in a new tab via `browser_navigate` so the gate FX is still mounted with debug hooks. Actually simpler: the entry-gate instance keeps running until the gate is dismissed via the Enter button — *don't* dismiss it during verification. Instead, in the verification flow, navigate to `/` and skip the dismissal click.
- Each task ends with one commit. Commit-message style follows the repo: `fix(fx): ...`, `feat(fx): ...`, `refactor(fx): ...`. Trailer: `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`.
- Verification screenshots are *local artifacts*. Save to `.cat-audit/demoscene/<NAME>.png` (gitignored — already-existing convention from prior plans).

---

## Phase 0 — Verification harness

This phase sets up reusable verification primitives. Every later task will use them.

### Task 1: Add a `verifyFxScene` helper to the FX instance debug hooks

**Goal:** A single `window.__fxVerify(name)` function that jumps to a scene, waits for settle, and returns `{name, nonSpacePct, distinctChars, error}` so verification is one Playwright `browser_evaluate` call instead of an ad-hoc script.

**Files:**
- Modify: `static/index.html` around the `if (cfg.installDebug)` block (~line 5224)

**Why first:** Every later task verifies a specific scene. Centralizing the verify primitive keeps later steps tight.

- [ ] **Step 1: Add the verify hook**

Anchor: search for `window.__fxList = () => SCENES.map(s => s.name);` (around line 5249).

Insert immediately after `window.__fxBagSize = () => sceneBag.length;` (still inside the `if (cfg.installDebug)` block):

```js
        // Verify a specific scene: jump to it, wait `settleMs`, then summarize
        // the buf contents. Returns a Promise<{name, nonSpacePct, distinctChars, ok}>.
        window.__fxVerify = function(name, settleMs) {
          settleMs = settleMs == null ? 1500 : settleMs;
          const ok = window.__fxJump(name);
          if (!ok) return Promise.resolve({ name, ok: false, error: 'unknown scene' });
          return new Promise(function(resolve) {
            setTimeout(function() {
              let nonSpace = 0;
              const seen = Object.create(null);
              for (let i = 0; i < buf.length; i++) {
                const c = buf[i];
                if (c !== SPACE) nonSpace++;
                seen[c] = 1;
              }
              resolve({
                name,
                ok: true,
                nonSpacePct: +(nonSpace / buf.length * 100).toFixed(1),
                distinctChars: Object.keys(seen).length,
                cols: COLS,
                rows: ROWS
              });
            }, settleMs);
          });
        };
```

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify the hook works**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('PLASMA', 800)
```

Expected: returns `{name: 'PLASMA', ok: true, nonSpacePct: >50, distinctChars: >=8, cols: 46, rows: 22}` (desktop user-agent in Playwright).

- [ ] **Step 4: Verify it rejects unknown scene names**

```
mcp__playwright__browser_evaluate → () => window.__fxVerify('NONESUCH', 100)
```

Expected: `{name: 'NONESUCH', ok: false, error: 'unknown scene'}`.

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
test(fx): __fxVerify hook for scene verification

Returns {nonSpacePct, distinctChars, cols, rows} after a settle delay so
Playwright can assert per-scene rendering correctness in one round-trip.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 1 — Engine refactor

Foundational changes that every later task builds on. Do these in order.

### Task 2: Hoist per-frame allocations to closure scope

**Goal:** Eliminate GC churn. Sphere3D allocates `Float32Array(COLS*ROWS)` every frame; Voxel allocates `Int16Array(COLS)`; Vectors allocates `Array(VEC_COUNT)` + 3× `Float32Array(VEC_COUNT)`; Cellular allocates `Uint8Array(COLS)` per step.

**Files:**
- Modify: `static/index.html` (Sphere3D ~4839, Voxel ~4684, Vectors ~4768, Cellular ~4592)

- [ ] **Step 1: Hoist Sphere3D z-buffer**

Anchor: search for `/* ─── Scene: Sphere3D` (around line 4831).

Replace this block (the function definition):

```js
      function drawSphere3d(now) {
        clearBuf();
        const A = now * 0.0009, B = now * 0.0013;
        const cosA = Math.cos(A), sinA = Math.sin(A);
        const cosB = Math.cos(B), sinB = Math.sin(B);
        const cx = COLS / 2, cy = ROWS / 2;
        const R = Math.min(COLS, ROWS * 1.8) * 0.52;
        const zBufS = new Float32Array(COLS * ROWS);
```

with (note the hoisted `sphereZBuf`, declared *just before* the function):

```js
      const sphereZBuf = new Float32Array(COLS * ROWS);
      function drawSphere3d(now) {
        clearBuf();
        sphereZBuf.fill(0);
        const A = now * 0.0009, B = now * 0.0013;
        const cosA = Math.cos(A), sinA = Math.sin(A);
        const cosB = Math.cos(B), sinB = Math.sin(B);
        const cx = COLS / 2, cy = ROWS / 2;
        const R = Math.min(COLS, ROWS * 1.8) * 0.52;
```

Then replace the two inner references `zBufS[...]` → `sphereZBuf[...]` (there are 4 occurrences: read+write in the lat loop, read+write in the lng loop). Use search/replace within the Sphere3D function only.

- [ ] **Step 2: Hoist Voxel y-buffer**

Anchor: search for `/* ─── Scene: Voxel` (around line 4651).

Inside `drawVoxel`, find:

```js
        const yBuf = new Int16Array(COLS);
        for (let x = 0; x < COLS; x++) yBuf[x] = ROWS;
```

and replace with:

```js
        for (let x = 0; x < COLS; x++) voxelYBuf[x] = ROWS;
```

Then immediately *before* `function drawVoxel(now) {`, add:

```js
      const voxelYBuf = new Int16Array(COLS);
```

Inside the function, rename all 3 `yBuf[...]` references to `voxelYBuf[...]`.

- [ ] **Step 3: Hoist Vectors arrays**

Anchor: search for `/* ─── Scene: Vectors` (around line 4744).

Inside `drawVectors`, find:

```js
        const order = new Array(VEC_COUNT);
        const zs = new Float32Array(VEC_COUNT);
        const projX = new Float32Array(VEC_COUNT);
        const projY = new Float32Array(VEC_COUNT);
```

Delete those four lines.

Immediately *before* `function drawVectors(now, dt) {`, add:

```js
      const vecOrder = new Int16Array(VEC_COUNT);
      const vecZs = new Float32Array(VEC_COUNT);
      const vecProjX = new Float32Array(VEC_COUNT);
      const vecProjY = new Float32Array(VEC_COUNT);
      for (let i = 0; i < VEC_COUNT; i++) vecOrder[i] = i;
      const vecOrderArr = Array.from(vecOrder); // for .sort below
```

Inside the function, replace:
- `order` → `vecOrderArr` (used for `.sort` and indexing; keep as `Array` since `Int16Array.sort` doesn't accept a comparator the same way — verify: actually `TypedArray.prototype.sort` does accept a comparator since ES2015, so `vecOrder.sort(...)` works. Use `vecOrder` directly and drop `vecOrderArr`.)
- `zs` → `vecZs`
- `projX` → `vecProjX`
- `projY` → `vecProjY`

After the projection loop, the sort line becomes:

```js
        vecOrder.sort((a, b) => vecZs[b] - vecZs[a]);
```

Note: `Int16Array.prototype.sort` works with comparator functions. If verification shows weird ordering, replace `vecOrder` with `Array.from({length: VEC_COUNT}, (_, i) => i)` once at closure scope.

Remove the `vecOrderArr` line above; the simpler form is:

```js
      const vecOrder = new Array(VEC_COUNT);
      for (let i = 0; i < VEC_COUNT; i++) vecOrder[i] = i;
      const vecZs = new Float32Array(VEC_COUNT);
      const vecProjX = new Float32Array(VEC_COUNT);
      const vecProjY = new Float32Array(VEC_COUNT);
```

(Plain `Array` for the order; sort comparator is reliable.)

But **do not re-initialize `vecOrder[i] = i`** at the start of each frame — the sort will keep the indices 0..N-1, just reordered. The next frame's sort starts from the previous order, which is *not* a problem for `.sort()` since the comparator is total-ordering.

- [ ] **Step 4: Hoist Cellular `next` row**

Anchor: search for `/* ─── Scene: Cellular` (around line 4573).

Inside `stepCellular`, find:

```js
        // next row via rule 30
        const next = new Uint8Array(COLS);
        for (let x = 0; x < COLS; x++) {
```

Replace with:

```js
        // next row via rule 30
        for (let x = 0; x < COLS; x++) {
```

And replace `next[x] = (30 >> pat) & 1;` → `caNext[x] = (30 >> pat) & 1;`.

After the loop, replace `caRow = next;` with:

```js
        const tmp = caRow; caRow = caNext; caNext = tmp;
```

Immediately before `function seedCellular()` (around line 4578), add:

```js
      let caNext = new Uint8Array(COLS);
```

(Note: `let` not `const` because the swap reassigns it.)

- [ ] **Step 5: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 6: Verify all four touched effects still render**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['SPHERE3D', 'VOXEL', 'VECTORS', 'CELLULAR']) {
    out.push(await window.__fxVerify(n, 1500));
  }
  return out;
}
```

Expected: each entry has `ok: true`, `nonSpacePct > 5`, `distinctChars >= 3`. (Sparse scenes like VECTORS may be ~5%.)

If any return `nonSpacePct === 0` or hang, revert and inspect — likely the buffer reference is wrong or `caNext`/`vecOrder` initialization is off.

- [ ] **Step 7: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(fx): hoist per-frame allocations to closure scope

Sphere3D z-buffer, Voxel y-buffer, Vectors order/zs/projX/projY arrays,
and Cellular's next-row buffer were all being allocated every frame.
Hoisting eliminates GC pressure on the FX hot path.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Replace hardcoded `*1.8` aspect with computed `CHAR_ASPECT`

**Goal:** The `1.8` factor compensates for monospace cells being taller than wide. The actual ratio for the current font (`11px / 1.2 line-height`, Share Tech Mono) is closer to 2.0; on mobile (`9.5px / 1.15`) it's ~1.92. Compute it once from font metrics.

**Files:**
- Modify: `static/index.html` (declaration site near line 3902, 13 callsites listed below)

- [ ] **Step 1: Add the constant**

Anchor: search for `const COLS = isMobile ? 34 : 46;` (around line 3902).

Replace the two-line declaration:

```js
      const COLS = isMobile ? 34 : 46;
      const ROWS = isMobile ? 17 : 22;
```

with:

```js
      let COLS = isMobile ? 34 : 46;
      let ROWS = isMobile ? 17 : 22;

      // Cell aspect ratio (height / width). Monospace glyphs are taller than
      // wide; multiply y-deltas by this so circular math reads circular.
      // Computed from canvas font metrics on first paint, with a static fallback.
      let CHAR_ASPECT = isMobile ? 1.92 : 2.0;
```

(`let` instead of `const` because Task 4 will mutate them on resize.)

- [ ] **Step 2: Replace 13 `*1.8` callsites**

Search the FX block (lines ~3890–5305) and replace each `* 1.8` (with surrounding context) per the table below. **Skip** the two `Math.random() * 1.8` lines (Warp particle speed — unrelated to aspect) and the `Math.sin(u * 1.8 + t * 2.2)` line (Ribbon parametric — unrelated):

| Line (approx) | Original | Replacement |
|---|---|---|
| 4158 | `const dx = x - cx, dy = (y - cy) * 1.8;` | `const dx = x - cx, dy = (y - cy) * CHAR_ASPECT;` |
| 4247 | `const dy = (y - cy) * 1.8;` | `const dy = (y - cy) * CHAR_ASPECT;` |
| 4376 | `const dy = (y - cy) * 1.8;` | `const dy = (y - cy) * CHAR_ASPECT;` |
| 4450 | `const scale = Math.min(COLS, ROWS * 1.8) * 0.5;` | `const scale = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.5;` |
| 4486 | `let dy = Math.abs((y - cy) * 1.8);` | `let dy = Math.abs((y - cy) * CHAR_ASPECT);` |
| 4513 | `const dy = (y - by[i]) * 1.8;` | `const dy = (y - by[i]) * CHAR_ASPECT;` |
| 4765 | `const scale = Math.min(COLS, ROWS * 1.8) * 0.78;` | `const scale = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.78;` |
| 4815 | `const dx = x - cx, dy = (y - cy) * 1.8;` | `const dx = x - cx, dy = (y - cy) * CHAR_ASPECT;` |
| 4838 | `const R = Math.min(COLS, ROWS * 1.8) * 0.52;` | `const R = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.52;` |
| 4939 | `const maxR = Math.sqrt(cx * cx + (cy * 1.8) * (cy * 1.8));` | `const maxR = Math.sqrt(cx * cx + (cy * CHAR_ASPECT) * (cy * CHAR_ASPECT));` |
| 4977 | `const dxA = x - cx + ox1, dyA = (y - cy + oy1) * 1.8;` | `const dxA = x - cx + ox1, dyA = (y - cy + oy1) * CHAR_ASPECT;` |
| 4978 | `const dxB = x - cx - ox1, dyB = (y - cy - oy1) * 1.8;` | `const dxB = x - cx - ox1, dyB = (y - cy - oy1) * CHAR_ASPECT;` |

Also update the Tunnel lookup table builder (line ~4242 inside `buildTunnelTables`):

```js
            const dy = (y - cy) * 1.8;
```

→

```js
            const dy = (y - cy) * CHAR_ASPECT;
```

- [ ] **Step 3: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 4: Spot-check 4 scenes that use the constant**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['PLASMA', 'TUNNEL', 'METABALLS', 'SPHERE3D']) {
    out.push(await window.__fxVerify(n, 1200));
  }
  return out;
}
```

Expected: all `ok: true`, `nonSpacePct > 30` (these are dense effects). The visual change is subtle — circles will look very slightly more circular.

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
refactor(fx): replace hardcoded 1.8 with CHAR_ASPECT constant

Cell aspect compensation was hardcoded to 1.8 across 13 callsites; the
actual cell ratio for the current font is ~2.0 desktop / ~1.92 mobile.
Single constant makes circular math read circular and lets Task 4 mutate
it on resize.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

(Continued in next section.)

### Task 4: Add ResizeObserver for live canvas dimensions

**Goal:** Today the engine measures `isMobile`, `COLS`, `ROWS`, and builds Tunnel lookup tables once at boot. Rotating the device or switching the Demo window between maximized and windowed leaves all that stale.

**Files:**
- Modify: `static/index.html` near the end of `createFxInstance` (just before the `return { stop: teardown }` at ~5291)

- [ ] **Step 1: Wrap COLS/ROWS-dependent buffers in a `rebuild` helper**

This is more involved than a simple find/replace because nearly every effect's helper buffer is sized off COLS×ROWS. To avoid a giant rewrite, the simpler approach is: on resize, compute new COLS/ROWS, mutate the `let` bindings, and reallocate the *director-owned* buffers (`buf`, `prevBuf`) and rebuild Tunnel tables. Per-effect buffers (Sphere3D z-buf, Voxel y-buf, Vectors arrays, Cellular history, Life buffers, etc.) get reallocated lazily on next call by introducing a `geometryRev` integer that each effect checks.

Anchor: search for `const buf = new Uint8Array(COLS * ROWS);` (around line 3909).

Replace:

```js
      const buf = new Uint8Array(COLS * ROWS);
      const prevBuf = new Uint8Array(COLS * ROWS);
      let hasPrev = false;
      const rowStrs = new Array(ROWS);
```

with:

```js
      let buf = new Uint8Array(COLS * ROWS);
      let prevBuf = new Uint8Array(COLS * ROWS);
      let hasPrev = false;
      let rowStrs = new Array(ROWS);
      let geometryRev = 0;
```

(`let` so resize can reassign.)

- [ ] **Step 2: Add the resize handler**

Anchor: search for `// Pause when tab hidden to save CPU` (around line 5277).

Insert immediately *before* that block:

```js
      // Live geometry tracking — recompute COLS/ROWS and rebuild tables when
      // the canvas changes size (orientation flip, window maximize, font load).
      function measureGeometry() {
        const cs = window.getComputedStyle(canvas);
        const fontPx = parseFloat(cs.fontSize) || 11;
        const lineH = parseFloat(cs.lineHeight) || (fontPx * 1.2);
        // Char advance estimate: Share Tech Mono advance ≈ 0.6em.
        const charW = fontPx * 0.6;
        CHAR_ASPECT = lineH / charW;
        const widthPx = canvas.clientWidth || canvas.offsetWidth || 280;
        const heightPx = (canvas.clientHeight || canvas.offsetHeight || 200);
        const newCols = Math.max(20, Math.min(120, Math.floor(widthPx / charW)));
        const newRows = Math.max(10, Math.min(60, Math.floor(heightPx / lineH)));
        if (newCols === COLS && newRows === ROWS) return false;
        COLS = newCols;
        ROWS = newRows;
        buf = new Uint8Array(COLS * ROWS);
        prevBuf = new Uint8Array(COLS * ROWS);
        rowStrs = new Array(ROWS);
        hasPrev = false;
        geometryRev++;
        // Rebuild director-owned lookup tables.
        if (typeof rebuildTunnelTables === 'function') rebuildTunnelTables();
        return true;
      }
      // First measurement after the first paint so font is loaded.
      requestAnimationFrame(measureGeometry);
      let resizeTimer = 0;
      const ro = new ResizeObserver(function() {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(measureGeometry, 100);
      });
      ro.observe(canvas);
```

- [ ] **Step 3: Make tunnel tables rebuildable**

Anchor: search for `const tunU = new Float32Array(COLS * ROWS);` (around line 4239).

Replace this block:

```js
      const tunU = new Float32Array(COLS * ROWS);
      const tunV = new Float32Array(COLS * ROWS);
      const tunDist = new Float32Array(COLS * ROWS);
      function buildTunnelTables() {
```

with:

```js
      let tunU = new Float32Array(COLS * ROWS);
      let tunV = new Float32Array(COLS * ROWS);
      let tunDist = new Float32Array(COLS * ROWS);
      function rebuildTunnelTables() {
        tunU = new Float32Array(COLS * ROWS);
        tunV = new Float32Array(COLS * ROWS);
        tunDist = new Float32Array(COLS * ROWS);
        buildTunnelTables();
      }
      function buildTunnelTables() {
```

This adds a `rebuild*` wrapper that resizes then refills, leaving the existing fill loop untouched.

- [ ] **Step 4: Make per-effect buffers self-heal on geometryRev change**

Effects that hold their own COLS/ROWS-sized buffers must re-allocate when `geometryRev` advances. Add a per-effect `lastRev` local at module scope and check at the start of each affected `draw*`. Affected: Fire, Stars, Sphere3D, Voxel, Vectors, Life, Cellular, Spiral, Warp, plus any new effect that holds buffers.

Insert after `let geometryRev = 0;`:

```js
      // Per-effect geometry-rev cache. Effects with COLS/ROWS-sized state
      // increment this when they reallocate their helpers.
      const geomRev = {
        fire: 0, life: 0, cell: 0, sphere: 0, voxel: 0
      };
```

Then at the top of each affected draw function add a check. Example for Fire — search for `const fireH = ROWS + 1;` (line 4170) and replace:

```js
      const fireH = ROWS + 1;
      const fireBuf = new Float32Array(COLS * fireH);
      function drawFire(now) {
```

with:

```js
      let fireH = ROWS + 1;
      let fireBuf = new Float32Array(COLS * fireH);
      function drawFire(now) {
        if (geomRev.fire !== geometryRev) {
          fireH = ROWS + 1;
          fireBuf = new Float32Array(COLS * fireH);
          geomRev.fire = geometryRev;
        }
```

Apply the same pattern (add `if (geomRev.X !== geometryRev) { ...realloc...; geomRev.X = geometryRev; }` at function entry) to:

- **Life**: realloc `lifeCurr`, `lifeNext` to `Uint8Array(COLS * ROWS)`; reset `lifeAcc=0, lifeAge=0` and call `seedLife()`. Mark `geomRev.life`.
- **Cellular**: realloc `caRow = new Uint8Array(COLS)`, `caHistory = new Uint8Array(COLS * ROWS)`, `caNext = new Uint8Array(COLS)`; call `seedCellular()`. Mark `geomRev.cell`.
- **Sphere3D**: realloc `sphereZBuf = new Float32Array(COLS * ROWS)`. Mark `geomRev.sphere`.
- **Voxel**: realloc `voxelYBuf = new Int16Array(COLS)`. Mark `geomRev.voxel`.

For Stars/Spiral/Warp/Vectors, the particle arrays are sized off `starCount`/`spiralN`/`warpN`/`VEC_COUNT` (constants at instance scope), not COLS/ROWS — so they don't need realloc, only their projection uses the new COLS/ROWS, which they already read freely via the closure. **No code change required for those.**

- [ ] **Step 5: Teardown should disconnect the observer**

Anchor: search for `function teardown() {` (around line 5218).

Replace:

```js
      function teardown() {
        running = false;
        if (rafId) { cancelAnimationFrame(rafId); rafId = 0; }
        document.removeEventListener('visibilitychange', onVisibility);
      }
```

with:

```js
      function teardown() {
        running = false;
        if (rafId) { cancelAnimationFrame(rafId); rafId = 0; }
        document.removeEventListener('visibilitychange', onVisibility);
        if (ro) ro.disconnect();
        if (resizeTimer) clearTimeout(resizeTimer);
      }
```

- [ ] **Step 6: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 7: Verify resize triggers rebuild**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_resize → width:1280, height:900
mcp__playwright__browser_evaluate → () => window.__fxVerify('PLASMA', 800)
```

Capture `cols`/`rows` in the result.

```
mcp__playwright__browser_resize → width:480, height:800
mcp__playwright__browser_evaluate → async () => {
  await new Promise(r => setTimeout(r, 250));
  return window.__fxVerify('PLASMA', 800);
}
```

Expected: second result has *different* `cols`/`rows` from first. (The actual numbers depend on canvas CSS — desktop is ~46×22, mobile-width may be ~34×17 or similar.) `nonSpacePct` should remain >50 in both.

- [ ] **Step 8: Verify Tunnel re-builds tables (no stale lookup artifacts)**

```
mcp__playwright__browser_resize → width:1280, height:900
mcp__playwright__browser_evaluate → async () => {
  await new Promise(r => setTimeout(r, 300));
  return window.__fxVerify('TUNNEL', 1500);
}
```

Expected: `nonSpacePct > 70` (tunnel fills almost everything) and no JS errors.

- [ ] **Step 9: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): ResizeObserver for live canvas dimensions

COLS/ROWS, CHAR_ASPECT, and the director's buf/prevBuf are now mutable
and rebuild on canvas resize (orientation flip, window maximize, font
swap). Per-effect helpers self-heal via a geometryRev integer.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 2 — Bug fixes (one task per bug)

### Task 5: Fix WAVE3D draw order (far-to-near)

**Files:**
- Modify: `static/index.html` `drawWave3d` (around line 4350)

- [ ] **Step 1: Reverse the gz loop**

Anchor: search for `function drawWave3d(now) {` (around line 4350).

In the body, find:

```js
        for (let gz = 1; gz <= GRID; gz++) {
```

Replace with:

```js
        for (let gz = GRID; gz >= 1; gz--) {
```

(`gz=1` is near with the highest zScale; we now draw far first so near overwrites.)

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Visual verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('WAVE3D', 2000)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/WAVE3D.png
```

Expected: `nonSpacePct > 8` (sparse dots), `distinctChars >= 4`. Visually, the brightest characters should be in the foreground (closer to bottom-center) with dimmer dots receding to the horizon.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,wave3d): paint far-to-near so foreground reads as foreground

The gz loop ran near→far, which let distant grid points overwrite near
ones. Reversed so the painter's algorithm is correct.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Fix MATRIX trail decay

**Goal:** Replace the fixed-ladder tail with a per-cell brightness buffer that decays each frame; allow up to 2 active drops per column.

**Files:**
- Modify: `static/index.html` (Matrix block ~4314–4347)

- [ ] **Step 1: Replace the Matrix block**

Anchor: search for `/* ─── Scene: Matrix rain ─── */` (around line 4314).

Replace the entire block from that comment through the end of `drawMatrix` (about 33 lines) with:

```js
      /* ─── Scene: Matrix rain (per-cell decay buffer + 2 drops per col) ─── */
      const M_DROPS_PER_COL = 2;
      let mDrops = new Float32Array(COLS * M_DROPS_PER_COL);
      let mChars = new Uint16Array(COLS * ROWS);
      let mGlow = new Uint8Array(COLS * ROWS); // 0..255 fade buffer
      const MATRIX_POOL = "01_/\\|<>{}[]()KDHOMEARCHIVEMOD@#%*+=.";
      function initMatrix() {
        for (let i = 0; i < mDrops.length; i++) mDrops[i] = -Math.random() * ROWS * 2;
        for (let i = 0; i < mChars.length; i++) {
          mChars[i] = MATRIX_POOL.charCodeAt((Math.random() * MATRIX_POOL.length) | 0);
        }
        mGlow.fill(0);
      }
      initMatrix();
      function drawMatrix(now, dt) {
        // Geometry self-heal
        if (mChars.length !== COLS * ROWS) {
          mDrops = new Float32Array(COLS * M_DROPS_PER_COL);
          mChars = new Uint16Array(COLS * ROWS);
          mGlow = new Uint8Array(COLS * ROWS);
          initMatrix();
        }
        const step = (dt || 16) / 60;
        // Decay every cell
        for (let i = 0; i < mGlow.length; i++) {
          const v = mGlow[i] - 22;
          mGlow[i] = v < 0 ? 0 : v;
        }
        // Advance drops, paint heads
        for (let x = 0; x < COLS; x++) {
          for (let d = 0; d < M_DROPS_PER_COL; d++) {
            const di = x * M_DROPS_PER_COL + d;
            mDrops[di] += step * (0.45 + Math.random() * 0.45);
            if (mDrops[di] > ROWS + 4) {
              mDrops[di] = -Math.random() * ROWS * 1.5;
            }
            const head = Math.floor(mDrops[di]);
            if (head >= 0 && head < ROWS) {
              if (Math.random() < 0.35) {
                mChars[head * COLS + x] = MATRIX_POOL.charCodeAt((Math.random() * MATRIX_POOL.length) | 0);
              }
              mGlow[head * COLS + x] = 255;
            }
          }
        }
        // Render: bright head = mChars; trail = scaled ramp
        for (let i = 0; i < mGlow.length; i++) {
          const g = mGlow[i];
          if (g === 0) { buf[i] = SPACE; continue; }
          if (g >= 220) { buf[i] = mChars[i]; continue; }
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, ((g / 255) * RAMP_LEN) | 0));
          buf[i] = RAMP.charCodeAt(idx);
        }
      }
```

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('MATRIX', 2500)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/MATRIX.png
```

Expected: `nonSpacePct` between 25 and 60, `distinctChars >= 8` (head chars + ramp gradients). Visually: bright heads at varied vertical positions per column, tails fading downward (actually upward, since drops go down — the tail above the head fades).

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,matrix): true trail decay + multi-drop columns

Per-cell glow buffer decays 22/frame so trails fade smoothly through
the brightness ramp instead of a fixed 9-step ladder. Two drops per
column produce denser, more authentic Matrix rain.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Fix LANDSCAPE (twinkle, drift moon, trees, shooting stars)

**Files:**
- Modify: `static/index.html` `drawLandscape` (around line 4990)

- [ ] **Step 1: Replace `drawLandscape`**

Anchor: search for `/* ─── Scene: Landscape` (around line 4990).

Replace the function (about 38 lines) with:

```js
      /* ─── Scene: Landscape (twinkling stars, drifting moon, parallax peaks) ─── */
      let landShootStartT = -100; // shooting star event start time (seconds)
      function drawLandscape(now) {
        clearBuf();
        const t = now * 0.0004;
        const tSec = now * 0.001;
        const horizon = Math.floor(ROWS * 0.55);
        // Stars with per-star twinkle phase
        const starN = isMobile ? 14 : 24;
        for (let i = 0; i < starN; i++) {
          const sx = ((i * 37) + ((tSec * 1.5) | 0)) % COLS;
          const sy = (i * 5 + 1) % Math.max(1, horizon);
          // Each star twinkles at its own phase
          const tw = Math.sin(tSec * 2.5 + i * 1.3);
          if (tw > 0.2) buf[sy * COLS + sx] = tw > 0.7 ? 42 /*'*'*/ : 46 /*'.'*/;
        }
        // Drifting moon (slow horizontal sweep)
        const moonX = Math.floor(((Math.sin(t * 0.3) * 0.4 + 0.5) * (COLS - 4)) + 1);
        const moonY = Math.floor(horizon * 0.4);
        if (moonX + 2 < COLS && moonY < ROWS) {
          buf[moonY * COLS + moonX]     = 40; // '('
          buf[moonY * COLS + moonX + 1] = 41; // ')'
        }
        // Shooting star: random event, ~once every 8 seconds
        if (tSec - landShootStartT > 8 && Math.random() < 0.005) {
          landShootStartT = tSec;
        }
        const sse = tSec - landShootStartT;
        if (sse >= 0 && sse < 0.5) {
          const sx0 = (((landShootStartT * 13) | 0) % (COLS - 6)) + 1;
          const sy0 = ((landShootStartT * 7) | 0) % Math.max(1, horizon - 1);
          const sx = Math.floor(sx0 + sse * 30);
          const sy = Math.floor(sy0 + sse * 6);
          for (let k = 0; k < 4; k++) {
            const px = sx - k, py = sy - ((k * 0.4) | 0);
            if (px >= 0 && px < COLS && py >= 0 && py < ROWS) {
              buf[py * COLS + px] = k === 0 ? 42 /*'*'*/ : 45 /*'-'*/;
            }
          }
        }
        // Parallax mountain layers, FAR → NEAR
        const layers = [
          { amp: 2.2, freq: 0.30, phase: 1.2, speed: 5,  ch: 46 /*.*/ },
          { amp: 3.4, freq: 0.20, phase: 0.6, speed: 9,  ch: 43 /*+*/ },
          { amp: 5.0, freq: 0.13, phase: 0.1, speed: 14, ch: 35 /*#*/ },
        ];
        for (let l = 0; l < layers.length; l++) {
          const L = layers[l];
          for (let x = 0; x < COLS; x++) {
            const wx = x * L.freq + L.phase + t * L.speed;
            const raw = Math.sin(wx) * 0.55 + Math.sin(wx * 0.47 + 1.3) * 0.35 + Math.sin(wx * 1.9 + 0.4) * 0.25;
            const h = (raw * 0.5 + 0.5) * L.amp;
            const peakY = Math.max(0, Math.min(ROWS - 1, Math.round(horizon - h)));
            for (let y = peakY; y < ROWS; y++) {
              buf[y * COLS + x] = L.ch;
            }
          }
        }
        // Foreground tree silhouettes on the bottom row
        const treeRow = ROWS - 1;
        const treePos = [Math.floor(COLS * 0.18), Math.floor(COLS * 0.45), Math.floor(COLS * 0.78)];
        for (const tx of treePos) {
          if (tx >= 0 && tx < COLS) {
            buf[treeRow * COLS + tx] = 89 /*'Y'*/;
            if (treeRow - 1 >= 0) buf[(treeRow - 1) * COLS + tx] = 124 /*'|'*/;
          }
        }
      }
```

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify (capture two screenshots ~3s apart so the moon visibly drifts)**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxJump('LANDSCAPE')
mcp__playwright__browser_evaluate → () => window.__fxFreeze('LANDSCAPE')
```

(Without freeze the bag picker would advance.)

```
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/LANDSCAPE-a.png
mcp__playwright__browser_evaluate → async () => { await new Promise(r => setTimeout(r, 3000)); return true; }
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/LANDSCAPE-b.png
mcp__playwright__browser_evaluate → () => window.__fxVerify('LANDSCAPE', 800)
```

Expected: `nonSpacePct > 50`, `distinctChars >= 6` (`.`, `*`, `(`, `)`, `+`, `#`, `Y`, `|`, space). Visually: Y/| trees on the bottom row in 3 spots, mountains above, twinkling stars and a moon in the sky.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,landscape): twinkling stars, drifting moon, trees, shooting stars

Stars now have per-star phase twinkle; moon sweeps horizontally; three
foreground tree silhouettes anchor the bottom; an occasional shooting
star adds life. Mountains unchanged.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Fix TWISTER (widen + body fill)

**Files:**
- Modify: `static/index.html` `drawTwister` (around line 4895)

- [ ] **Step 1: Replace `drawTwister`**

Anchor: search for `/* ─── Scene: Twister` (around line 4894).

Replace the function (about 28 lines) with:

```js
      /* ─── Scene: Twister (twisting 3D band that fills the canvas height) ─── */
      function drawTwister(now) {
        clearBuf();
        const cx = COLS / 2;
        const t = now * 0.001;
        const halfWidth = COLS * 0.32;
        const RAMP_HOT = RAMP_LEN - 1;
        for (let y = 0; y < ROWS; y++) {
          const angle = t * 1.6 + y * 0.28;
          // Four corner positions (the band's quad cross-section seen edge-on)
          const corners = [0, 0, 0, 0]; // x positions
          const depths  = [0, 0, 0, 0]; // sin(a) → -1..1
          for (let c = 0; c < 4; c++) {
            const a = angle + c * (Math.PI / 2);
            corners[c] = cx + Math.cos(a) * halfWidth;
            depths[c]  = Math.sin(a);
          }
          // Sort corners left-to-right
          const order = [0, 1, 2, 3];
          order.sort((a, b) => corners[a] - corners[b]);
          // Paint horizontal spans between consecutive corners
          for (let s = 0; s < 3; s++) {
            const ia = order[s], ib = order[s + 1];
            const x0 = corners[ia], x1 = corners[ib];
            const d0 = depths[ia],  d1 = depths[ib];
            const lo = Math.max(0, Math.ceil(x0));
            const hi = Math.min(COLS - 1, Math.floor(x1));
            const span = Math.max(1, x1 - x0);
            for (let sx = lo; sx <= hi; sx++) {
              const u = (sx - x0) / span;
              const depth = d0 + (d1 - d0) * u; // -1..1, front is +1
              const bright = (depth * 0.5 + 0.5);
              const idx = Math.max(2, Math.min(RAMP_HOT, (bright * RAMP_LEN) | 0));
              buf[y * COLS + sx] = RAMP.charCodeAt(idx);
            }
          }
          // Bright corner dots overlay so the band edges read crisp
          for (let c = 0; c < 4; c++) {
            const sx = Math.round(corners[c]);
            if (sx >= 0 && sx < COLS) {
              buf[y * COLS + sx] = depths[c] > 0 ? 64 /*'@'*/ : 35 /*'#'*/;
            }
          }
        }
      }
```

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('TWISTER', 1500)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/TWISTER.png
```

Expected: `nonSpacePct > 30` (now spans ~64% of canvas width per row), `distinctChars >= 4`. Visually: a vertical twisting band with four edges and shaded body fill — clearly reads as a 3D ribbon, not a sparse line.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,twister): widen band and shade-fill the body

Old twister was 18 cols wide of a 46-col canvas with empty interiors.
Now spans ~64% canvas width with depth-shaded fill between the four
rotating corners and bright edge markers — reads as a true 3D band.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Fix STARFIELD (streak draw for near stars)

**Files:**
- Modify: `static/index.html` `drawStars` (around line 4214)

- [ ] **Step 1: Replace the projection block in `drawStars`**

Anchor: search for `function drawStars(now, dt) {` (around line 4214).

Replace the body (everything after `clearBuf();` and before the closing `}`) with:

```js
        const cx = COLS / 2, cy = ROWS / 2;
        const scaleX = COLS * 0.55;
        const scaleY = ROWS * 0.55;
        const speed = Math.min(0.05, (dt || 16) / 1000 * 0.55);
        for (let i = 0; i < starCount; i++) {
          const b = i * 3;
          const z0 = stars[b + 2];
          stars[b + 2] -= speed;
          if (stars[b + 2] <= 0.05) {
            stars[b + 0] = (Math.random() - 0.5) * 2;
            stars[b + 1] = (Math.random() - 0.5) * 2;
            stars[b + 2] = 1.5;
            continue;
          }
          const z = stars[b + 2];
          const sx = Math.round(stars[b + 0] / z * scaleX + cx);
          const sy = Math.round(stars[b + 1] / z * scaleY + cy);
          // Streak: project the star's previous position to draw a short trail
          const sxPrev = Math.round(stars[b + 0] / z0 * scaleX + cx);
          const syPrev = Math.round(stars[b + 1] / z0 * scaleY + cy);
          const bright = Math.min(1, (1.5 - z) / 1.4);
          const idx = Math.max(1, Math.min(RAMP_LEN - 1, (bright * bright * RAMP_LEN) | 0));
          // Bresenham-ish line from prev to current
          let x0 = sxPrev, y0 = syPrev, x1 = sx, y1 = sy;
          const dx = Math.abs(x1 - x0), dy = -Math.abs(y1 - y0);
          const sxd = x0 < x1 ? 1 : -1, syd = y0 < y1 ? 1 : -1;
          let err = dx + dy, steps = 0;
          while (steps++ < 6) { // cap streak length
            if (x0 >= 0 && x0 < COLS && y0 >= 0 && y0 < ROWS) {
              const cur = buf[y0 * COLS + x0];
              if (cur === SPACE || cur < RAMP.charCodeAt(idx)) {
                buf[y0 * COLS + x0] = RAMP.charCodeAt(Math.max(1, idx - steps));
              }
            }
            if (x0 === x1 && y0 === y1) break;
            const e2 = 2 * err;
            if (e2 >= dy) { err += dy; x0 += sxd; }
            if (e2 <= dx) { err += dx; y0 += syd; }
          }
          // Final bright pixel
          if (sx >= 0 && sx < COLS && sy >= 0 && sy < ROWS) {
            buf[sy * COLS + sx] = RAMP.charCodeAt(idx);
          }
        }
```

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('STARFIELD', 2500)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/STARFIELD.png
```

Expected: `nonSpacePct` between 5 and 25 (sparse), `distinctChars >= 4`. Visually: stars near edges have visible streaks; central stars are dots.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,starfield): streak near stars instead of vanishing off-canvas

Project each star's previous-frame position and draw a short Bresenham
streak from prev → current. Brighter near edges where motion is fast,
dim/dot near center.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Fix TUNNEL (soften center hot-spot, ensure resize works)

**Files:**
- Modify: `static/index.html` `buildTunnelTables` (around line 4242)

- [ ] **Step 1: Soften the distance floor**

Anchor: search for `function buildTunnelTables() {` (around line 4242).

Inside the body, find:

```js
            const d = Math.sqrt(dx * dx + dy * dy) + 0.001;
            tunU[y * COLS + x] = 14 / d;
```

Replace with:

```js
            const d = Math.max(0.6, Math.sqrt(dx * dx + dy * dy));
            tunU[y * COLS + x] = 14 / d;
```

(`max(0.6, …)` clamps the very-near distance so the texture lookup at exact center doesn't shoot to a single hot u value.)

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('TUNNEL', 1500)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/TUNNEL.png
```

Expected: `nonSpacePct > 70`. Visually: no single bright pixel at the center; checkerboard texture flows continuously inward.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,tunnel): clamp center distance to soften hot-spot

d = max(0.6, ...) prevents the lookup from collapsing to a single u
value at the exact canvas center.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: Fix CELLULAR (ring buffer history)

**Files:**
- Modify: `static/index.html` `stepCellular` and `drawCellular` (around line 4585)

- [ ] **Step 1: Replace stepCellular and drawCellular with ring-buffer version**

Anchor: search for `function stepCellular() {` (around line 4585).

Replace this block (from `function stepCellular()` through the closing `}` of `drawCellular`, ~30 lines) with:

```js
      // Ring buffer: caHead is the row index of the *most recent* generation;
      // older generations live above it modulo ROWS. Render maps logical row
      // (top of canvas) to physical row in caHistory.
      let caHead = 0;
      function stepCellular() {
        for (let x = 0; x < COLS; x++) {
          const l = caRow[(x - 1 + COLS) % COLS];
          const c = caRow[x];
          const r = caRow[(x + 1) % COLS];
          const pat = (l << 2) | (c << 1) | r;
          caNext[x] = (30 >> pat) & 1;
        }
        caHead = (caHead + 1) % ROWS;
        for (let x = 0; x < COLS; x++) caHistory[caHead * COLS + x] = caRow[x];
        const tmp = caRow; caRow = caNext; caNext = tmp;
        caAge++;
        if (caAge > ROWS * 2) seedCellular();
      }
      function drawCellular(now, dt) {
        caAcc += dt;
        while (caAcc > 75) {
          caAcc -= 75;
          stepCellular();
        }
        // Render: oldest at top, newest at bottom of canvas.
        // Physical row for canvas row y = (caHead - (ROWS - 1 - y) + ROWS) % ROWS
        for (let y = 0; y < ROWS; y++) {
          const phys = (caHead - (ROWS - 1 - y) + ROWS) % ROWS;
          for (let x = 0; x < COLS; x++) {
            buf[y * COLS + x] = caHistory[phys * COLS + x] ? 35 /* # */ : 32;
          }
        }
      }
```

- [ ] **Step 2: Reset caHead in seedCellular**

Anchor: search for `function seedCellular() {` (around line 4578).

Inside the body, before the closing `}`, add:

```js
        if (typeof caHead !== 'undefined') caHead = 0;
      }
```

(The `typeof` guard handles the call-before-declaration case at module init.)

Wait — `caHead` is declared *after* `seedCellular` is called the first time (line `seedCellular();` immediately follows the function). To be safe, move the `let caHead = 0;` declaration to *before* `function seedCellular()`.

Anchor: search for `let caRow = new Uint8Array(COLS);` (around line 4574).

Replace:

```js
      let caRow = new Uint8Array(COLS);
      let caHistory = new Uint8Array(COLS * ROWS);
      let caAcc = 0;
      let caAge = 0;
```

with:

```js
      let caRow = new Uint8Array(COLS);
      let caHistory = new Uint8Array(COLS * ROWS);
      let caNext = new Uint8Array(COLS);
      let caAcc = 0;
      let caAge = 0;
      let caHead = 0;
```

(This consolidates `caNext` declaration here too, removing the separate insertion from Task 2 Step 4. If Task 2 already inserted `let caNext = new Uint8Array(COLS);` separately, delete that prior line as part of this task's diff.)

Then `seedCellular` becomes:

```js
      function seedCellular() {
        caRow.fill(0);
        caRow[COLS >> 1] = 1;
        caHistory.fill(0);
        caAge = 0;
        caHead = 0;
      }
```

- [ ] **Step 3: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 4: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('CELLULAR', 3000)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/CELLULAR.png
```

Expected: `nonSpacePct` between 20 and 50 (rule 30 fills ~half), `distinctChars` exactly 2 (`#` and space). Visually: a Sierpinski-like triangular pattern growing from the center, with newest generation at the bottom.

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,cellular): ring-buffer history (drop O(N²) per-step scroll)

caHead pointer advances each step; render maps logical canvas rows to
physical history rows modulo ROWS. Eliminates the per-step memcpy and
the per-step Uint8Array allocation.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Fix MANDELBROT interior visibility

**Files:**
- Modify: `static/index.html` `drawMandelbrot` (around line 4525)

- [ ] **Step 1: Render set interior with `#`**

Anchor: search for `function drawMandelbrot(now) {` (around line 4525).

Find:

```js
            let idx;
            if (i === maxIter) {
              idx = 0;
            } else {
```

Replace with:

```js
            let ch;
            if (i === maxIter) {
              ch = 35 /* '#' for the bulb interior */;
              buf[y * COLS + x] = ch;
              continue;
            }
            let idx;
            {
```

This requires adjusting the closing brace too. Find the section right after the smooth coloring block:

```js
              idx = Math.max(1, Math.min(RAMP_LEN - 1, ((smooth / maxIter) * RAMP_LEN) | 0));
            }
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
```

Replace with:

```js
              idx = Math.max(1, Math.min(RAMP_LEN - 1, ((smooth / maxIter) * RAMP_LEN) | 0));
            }
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
```

(No change to that line — the `continue` above handles the interior case. Verify the structure compiles by re-reading the function.)

For clarity, the final form of the inner loop body should be:

```js
            let a = a0, b = b0, i = 0, r2 = 0;
            while (i < maxIter && (r2 = a * a + b * b) < 4) {
              const ta = a * a - b * b + a0;
              b = 2 * a * b + b0;
              a = ta;
              i++;
            }
            if (i === maxIter) {
              buf[y * COLS + x] = 35 /* '#' bulb interior */;
              continue;
            }
            // Smooth continuous escape count for banding-free shading.
            const nu = Math.log(Math.log(Math.sqrt(r2 || 4)) / LOG2) / LOG2;
            const smooth = Math.max(0, i + 1 - nu);
            const idx = Math.max(1, Math.min(RAMP_LEN - 1, ((smooth / maxIter) * RAMP_LEN) | 0));
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
```

Replace the entire inner block from `let a = a0` through `buf[y * COLS + x] = RAMP.charCodeAt(idx);` with this cleaner form.

- [ ] **Step 2: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 3: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxVerify('MANDELBROT', 2000)
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/MANDELBROT.png
```

Expected: `nonSpacePct > 80` (interior + outside both visible), `distinctChars >= 6`. Visually: an iconic Mandelbrot bulb in `#` chars with smooth escape gradient around it.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx,mandelbrot): render set interior so the iconic bulb is visible

Inside-set cells were rendered as space, making the most recognizable
part of the Mandelbrot set invisible. Use '#' for interior; outside
keeps the smooth-escape gradient.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 13: Cross-fade scene colors during dissolve

**Goal:** Today the scene class swaps to `next.cls` instantly at transition start so colors flip even though chars dissolve. Fix: keep the previous class style applied via inline overrides for DISSOLVE_MS, interpolating `color` and `text-shadow` toward the new class.

**Files:**
- Modify: `static/index.html` scene-director block (around line 5156, 5170-5180, and inside `frame`)

- [ ] **Step 1: Add a color-snapshot helper**

Anchor: search for `function setSceneClass(cls) {` (around line 5156).

Replace this block:

```js
      function setSceneClass(cls) {
        canvas.className = 'fx-canvas ' + cls;
      }
      setSceneClass(SCENES[0].cls);
```

with:

```js
      // Color cross-fade state
      let xfadeFrom = null; // {color, shadow}
      let xfadeTo = null;
      let xfadeStart = 0;
      function snapshotStyle() {
        const cs = window.getComputedStyle(canvas);
        return { color: cs.color, shadow: cs.textShadow };
      }
      function setSceneClass(cls, withCrossfade) {
        if (withCrossfade) {
          xfadeFrom = snapshotStyle();
          canvas.className = 'fx-canvas ' + cls;
          xfadeTo = snapshotStyle();
          xfadeStart = performance.now();
          // Apply 'from' inline so frame 0 of the new scene still looks old
          canvas.style.color = xfadeFrom.color;
          canvas.style.textShadow = xfadeFrom.shadow;
        } else {
          canvas.className = 'fx-canvas ' + cls;
          canvas.style.color = '';
          canvas.style.textShadow = '';
          xfadeFrom = null;
          xfadeTo = null;
        }
      }
      setSceneClass(SCENES[0].cls, false);
```

- [ ] **Step 2: Use crossfade on transition + step it in `frame`**

Anchor: search for `setSceneClass(next.cls);` inside the scene advance block (around line 5177).

Replace `setSceneClass(next.cls);` with `setSceneClass(next.cls, true);`.

Then anchor: search for `// Dissolve from previous scene during first DISSOLVE_MS` (around line 5193).

Insert *before* that comment:

```js
        // Color cross-fade matched to the char dissolve duration
        if (xfadeFrom && xfadeTo) {
          const xe = now - xfadeStart;
          if (xe < DISSOLVE_MS) {
            const t01 = xe / DISSOLVE_MS;
            // Linearly interpolate via CSS color-mix() (modern browsers).
            // color-mix(in srgb, A pct, B (100-pct)).
            const fromPct = (1 - t01) * 100;
            canvas.style.color = `color-mix(in srgb, ${xfadeFrom.color} ${fromPct}%, ${xfadeTo.color})`;
            // Text-shadow doesn't support color-mix interpolation; use cross-fade via opacity overlay
            // approximation: drop shadow during transition, restore at end.
            canvas.style.textShadow = t01 > 0.5 ? xfadeTo.shadow : xfadeFrom.shadow;
          } else {
            canvas.style.color = '';
            canvas.style.textShadow = '';
            xfadeFrom = null;
            xfadeTo = null;
          }
        }
```

Also do the same for the `__fxJump` debug hook. Anchor: search for `setSceneClass(SCENES[i].cls);` inside `window.__fxJump` (around line 5233).

Replace with:

```js
          setSceneClass(SCENES[i].cls, false);
```

(Debug jumps shouldn't cross-fade — they're for instant inspection.)

- [ ] **Step 3: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 4: Verify (scene-director timing — let it natural-cycle)**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxSpeedMs(1500) // every scene 1.5s for fast cycling
mcp__playwright__browser_evaluate → async () => {
  // Capture computed color over 4 seconds at 50 ms intervals — should change smoothly during transitions
  const samples = [];
  for (let i = 0; i < 80; i++) {
    samples.push(window.getComputedStyle(document.querySelector('#fx-canvas')).color);
    await new Promise(r => setTimeout(r, 50));
  }
  return samples;
}
```

Expected: at least 5 distinct color values appear over the 4-second window (each scene has its own color, and transitions interpolate).

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx): cross-fade color through scene transitions

Snapshot previous and next computed color/text-shadow at transition
start; interpolate via color-mix(in srgb, ...) for DISSOLVE_MS so the
color shift moves with the char dissolve instead of snapping.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

(Phase 2 complete — 9 bug fixes landed. Phase 3 begins below.)

## Phase 3 — New effects: Particle family

### Task 14: Add SHADEBOBS, BOBS, FOUNTAIN

**Goal:** Three particle effects that all share an additive trail buffer that decays per frame.

**Files:**
- Modify: `static/index.html` — CSS classes (~line 3420), effect functions (insert before "BBS ticker scroller" comment, ~line 5061), SCENES entry (~line 5096)

- [ ] **Step 1: Add three CSS color classes**

Anchor: search for `.fx-canvas.fx-ribbon {` (around line 3418).

Insert *after* the closing `}` of `.fx-canvas.fx-ribbon`:

```css
    .fx-canvas.fx-shadebobs {
      color: #ff8ad5;
      text-shadow: -1px 0 rgba(255,200,0,0.4), 1px 0 rgba(0,200,255,0.4), 0 0 8px rgba(255,140,200,0.5);
    }
    .fx-canvas.fx-bobs {
      color: #ffd066;
      text-shadow: -1px 0 rgba(255,80,160,0.35), 1px 0 rgba(0,220,255,0.35), 0 0 8px rgba(255,200,80,0.45);
    }
    .fx-canvas.fx-fountain {
      color: #b0f0ff;
      text-shadow: -1px 0 rgba(255,100,200,0.4), 1px 0 rgba(0,200,255,0.5), 0 0 8px rgba(180,220,255,0.5);
    }
```

- [ ] **Step 2: Add a shared trail buffer + decay helper**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */` (around line 5062).

Insert *immediately before* that comment:

```js
      /* ─── Shared additive trail buffer (used by shadebobs/fountain/lightning/rain) ─── */
      let trailBuf = new Uint8Array(COLS * ROWS);
      function ensureTrailGeom() {
        if (trailBuf.length !== COLS * ROWS) {
          trailBuf = new Uint8Array(COLS * ROWS);
        }
      }
      function decayTrail(amount) {
        for (let i = 0; i < trailBuf.length; i++) {
          const v = trailBuf[i] - amount;
          trailBuf[i] = v < 0 ? 0 : v;
        }
      }
      function addTrail(x, y, energy) {
        if (x < 0 || x >= COLS || y < 0 || y >= ROWS) return;
        const i = y * COLS + x;
        const v = trailBuf[i] + energy;
        trailBuf[i] = v > 255 ? 255 : v;
      }
      function paintTrailToBuf() {
        for (let i = 0; i < trailBuf.length; i++) {
          const g = trailBuf[i];
          if (g === 0) { buf[i] = SPACE; continue; }
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, ((g / 255) * RAMP_LEN) | 0));
          buf[i] = RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Shadebobs (16 sine-orbiting bobs with additive trail) ─── */
      const sbN = isMobile ? 10 : 16;
      const sbPhase = new Float32Array(sbN * 4);
      function initShadebobs() {
        for (let i = 0; i < sbN; i++) {
          sbPhase[i * 4 + 0] = Math.random() * Math.PI * 2;
          sbPhase[i * 4 + 1] = Math.random() * Math.PI * 2;
          sbPhase[i * 4 + 2] = 0.6 + Math.random() * 1.2; // freq x
          sbPhase[i * 4 + 3] = 0.5 + Math.random() * 1.1; // freq y
        }
      }
      initShadebobs();
      function drawShadebobs(now) {
        ensureTrailGeom();
        decayTrail(28);
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS / 2;
        const ax = (COLS - 4) * 0.5;
        const ay = (ROWS - 2) * 0.5;
        for (let i = 0; i < sbN; i++) {
          const b = i * 4;
          const x = Math.round(cx + Math.sin(t * sbPhase[b + 2] + sbPhase[b + 0]) * ax);
          const y = Math.round(cy + Math.cos(t * sbPhase[b + 3] + sbPhase[b + 1]) * ay);
          // Splat a 3x3 falloff bob
          for (let dy = -1; dy <= 1; dy++) {
            for (let dx = -1; dx <= 1; dx++) {
              const e = 110 - (Math.abs(dx) + Math.abs(dy)) * 35;
              if (e > 0) addTrail(x + dx, y + dy, e);
            }
          }
        }
        paintTrailToBuf();
      }

      /* ─── Scene: Bobs (Lissajous bob cluster, depth-sorted) ─── */
      const bobN = isMobile ? 18 : 32;
      function drawBobs(now) {
        clearBuf();
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS / 2;
        // Lissajous parameters slowly morph
        const a = 3 + Math.sin(t * 0.3) * 1.5;
        const b = 4 + Math.cos(t * 0.27) * 1.5;
        const phaseShift = t * 0.6;
        // Build positions and z-depth, sort back-to-front.
        const xs = new Array(bobN), ys = new Array(bobN), zs = new Array(bobN);
        for (let i = 0; i < bobN; i++) {
          const u = (i / bobN) * Math.PI * 2;
          xs[i] = Math.round(cx + Math.sin(a * u + phaseShift) * (COLS * 0.42));
          ys[i] = Math.round(cy + Math.sin(b * u) * (ROWS * 0.42));
          zs[i] = Math.cos(a * u + phaseShift); // -1..1 fake z
        }
        const order = xs.map((_, i) => i).sort((p, q) => zs[p] - zs[q]);
        for (const i of order) {
          const x = xs[i], y = ys[i];
          const bright = (zs[i] * 0.5 + 0.5);
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
          // Splat 3x3 with center brightest
          for (let dy = -1; dy <= 1; dy++) {
            for (let dx = -1; dx <= 1; dx++) {
              const px = x + dx, py = y + dy;
              if (px < 0 || px >= COLS || py < 0 || py >= ROWS) continue;
              const fall = (Math.abs(dx) + Math.abs(dy));
              const li = Math.max(1, idx - fall * 8);
              if (buf[py * COLS + px] === SPACE || buf[py * COLS + px] < RAMP.charCodeAt(li)) {
                buf[py * COLS + px] = RAMP.charCodeAt(li);
              }
            }
          }
        }
      }

      /* ─── Scene: Fountain (gravity-driven particle fountain) ─── */
      const foN = isMobile ? 60 : 120;
      const foPart = new Float32Array(foN * 5); // x, y, vx, vy, life
      let foSpawnAcc = 0;
      function spawnFountainParticle(i) {
        const b = i * 5;
        foPart[b + 0] = COLS / 2 + (Math.random() - 0.5) * 2;
        foPart[b + 1] = ROWS - 1;
        const ang = -Math.PI / 2 + (Math.random() - 0.5) * 1.0; // mostly upward
        const speed = 12 + Math.random() * 10;
        foPart[b + 2] = Math.cos(ang) * speed;
        foPart[b + 3] = Math.sin(ang) * speed;
        foPart[b + 4] = 1.4 + Math.random() * 0.8; // life seconds
      }
      for (let i = 0; i < foN; i++) {
        foPart[i * 5 + 4] = 0; // start dead, spawn lazily
      }
      function drawFountain(now, dt) {
        ensureTrailGeom();
        decayTrail(20);
        const step = (dt || 16) / 1000;
        foSpawnAcc += step;
        // Spawn ~30 particles/sec
        while (foSpawnAcc > 0.033) {
          foSpawnAcc -= 0.033;
          for (let i = 0; i < foN; i++) {
            if (foPart[i * 5 + 4] <= 0) {
              spawnFountainParticle(i);
              break;
            }
          }
        }
        // Update + draw
        for (let i = 0; i < foN; i++) {
          const b = i * 5;
          if (foPart[b + 4] <= 0) continue;
          foPart[b + 3] += 18 * step; // gravity
          foPart[b + 0] += foPart[b + 2] * step;
          foPart[b + 1] += foPart[b + 3] * step;
          foPart[b + 4] -= step;
          if (foPart[b + 4] <= 0 || foPart[b + 1] >= ROWS) continue;
          const x = Math.round(foPart[b + 0]);
          const y = Math.round(foPart[b + 1]);
          addTrail(x, y, 180);
        }
        paintTrailToBuf();
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'SPIRAL',     fn: drawSpiral,     dur: 6000, cls: 'fx-spiral' },` (around line 5122).

Insert *after* that line, before the closing `];`:

```js
        { name: 'SHADEBOBS',  fn: drawShadebobs,  dur: 7000, cls: 'fx-shadebobs' },
        { name: 'BOBS',       fn: drawBobs,       dur: 6000, cls: 'fx-bobs' },
        { name: 'FOUNTAIN',   fn: drawFountain,   dur: 7000, cls: 'fx-fountain' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify all three**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['SHADEBOBS', 'BOBS', 'FOUNTAIN']) {
    out.push(await window.__fxVerify(n, 2000));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('SHADEBOBS')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/SHADEBOBS.png
mcp__playwright__browser_evaluate → () => window.__fxJump('BOBS')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/BOBS.png
mcp__playwright__browser_evaluate → () => window.__fxJump('FOUNTAIN')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/FOUNTAIN.png
```

Expected: SHADEBOBS `nonSpacePct > 25`, FOUNTAIN `nonSpacePct > 5` (sparse), BOBS `nonSpacePct > 4` (very sparse). All `distinctChars >= 4`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): SHADEBOBS, BOBS, FOUNTAIN (particle family + shared trail buf)

Three new particle effects sharing a decaying trail buffer:
- Shadebobs: 16 sine-orbiting splats, additive trail
- Bobs: Lissajous-path cluster, depth-sorted, 3x3 splats
- Fountain: gravity-driven particles spawned from bottom-center

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 15: Add SNAKE, LIGHTNING, RAIN

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add three CSS color classes**

Anchor: search for `.fx-canvas.fx-fountain {` (added in Task 14).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-snake {
      color: #b5ff8a;
      text-shadow: -1px 0 rgba(0,200,120,0.35), 1px 0 rgba(180,255,80,0.45), 0 0 8px rgba(140,255,120,0.45);
    }
    .fx-canvas.fx-lightning {
      color: #f0f0ff;
      text-shadow: -1px 0 rgba(180,80,255,0.5), 1px 0 rgba(80,200,255,0.5), 0 0 12px rgba(220,220,255,0.7);
    }
    .fx-canvas.fx-rain {
      color: #8acfff;
      text-shadow: -1px 0 rgba(0,180,255,0.4), 1px 0 rgba(120,200,255,0.4), 0 0 8px rgba(140,200,255,0.45);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */` (now slightly later after Task 14's insertion).

Insert *immediately before* that comment:

```js
      /* ─── Scene: Snake (procedural worm chasing its tail) ─── */
      const snakeMaxLen = isMobile ? 40 : 70;
      const snakeBody = new Int16Array(snakeMaxLen * 2);
      let snakeLen = 0;
      let snakeHeadX = 0, snakeHeadY = 0;
      let snakeDirX = 1, snakeDirY = 0;
      let snakeAcc = 0;
      function seedSnake() {
        snakeHeadX = (COLS / 2) | 0;
        snakeHeadY = (ROWS / 2) | 0;
        snakeDirX = 1; snakeDirY = 0;
        snakeLen = 1;
        snakeBody[0] = snakeHeadX; snakeBody[1] = snakeHeadY;
      }
      seedSnake();
      function drawSnake(now, dt) {
        snakeAcc += dt;
        while (snakeAcc > 90) {
          snakeAcc -= 90;
          // 12% chance to randomize direction (without 180° flip)
          if (Math.random() < 0.18) {
            const choices = [[1,0],[-1,0],[0,1],[0,-1]].filter(([dx,dy]) => !(dx === -snakeDirX && dy === -snakeDirY));
            const pick = choices[(Math.random() * choices.length) | 0];
            snakeDirX = pick[0]; snakeDirY = pick[1];
          }
          snakeHeadX = (snakeHeadX + snakeDirX + COLS) % COLS;
          snakeHeadY = (snakeHeadY + snakeDirY + ROWS) % ROWS;
          // Self-collision check → reseed
          let crashed = false;
          for (let i = 0; i < snakeLen; i++) {
            if (snakeBody[i * 2] === snakeHeadX && snakeBody[i * 2 + 1] === snakeHeadY) { crashed = true; break; }
          }
          if (crashed) { seedSnake(); continue; }
          // Append head
          if (snakeLen < snakeMaxLen) {
            for (let i = snakeLen; i > 0; i--) {
              snakeBody[i * 2] = snakeBody[(i - 1) * 2];
              snakeBody[i * 2 + 1] = snakeBody[(i - 1) * 2 + 1];
            }
            snakeBody[0] = snakeHeadX; snakeBody[1] = snakeHeadY;
            snakeLen++;
          } else {
            for (let i = snakeLen - 1; i > 0; i--) {
              snakeBody[i * 2] = snakeBody[(i - 1) * 2];
              snakeBody[i * 2 + 1] = snakeBody[(i - 1) * 2 + 1];
            }
            snakeBody[0] = snakeHeadX; snakeBody[1] = snakeHeadY;
          }
        }
        clearBuf();
        for (let i = 0; i < snakeLen; i++) {
          const x = snakeBody[i * 2], y = snakeBody[i * 2 + 1];
          if (x < 0 || x >= COLS || y < 0 || y >= ROWS) continue;
          const bright = 1 - (i / snakeLen);
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
          buf[y * COLS + x] = i === 0 ? 64 /*'@'*/ : RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Lightning (Brownian-motion bolts with screen flash) ─── */
      let boltAcc = 0;
      let flashUntil = 0;
      function drawLightningBolt(rootX) {
        let x = rootX, y = 0;
        const maxY = ROWS - 1;
        while (y < maxY) {
          addTrail(x, y, 220);
          // Maybe fork
          if (Math.random() < 0.08 && x > 1 && x < COLS - 2) {
            let fx = x, fy = y;
            const fdir = Math.random() < 0.5 ? -1 : 1;
            for (let k = 0; k < 6 + ((Math.random() * 6) | 0); k++) {
              fx += fdir + ((Math.random() * 3) | 0) - 1;
              fy += Math.random() < 0.7 ? 1 : 0;
              if (fy >= ROWS || fx < 0 || fx >= COLS) break;
              addTrail(fx, fy, 160);
            }
          }
          y++;
          x += ((Math.random() * 3) | 0) - 1;
          if (x < 0) x = 0; else if (x >= COLS) x = COLS - 1;
        }
      }
      function drawLightning(now, dt) {
        ensureTrailGeom();
        decayTrail(35);
        boltAcc += dt;
        if (boltAcc > 600 + Math.random() * 700) {
          boltAcc = 0;
          drawLightningBolt(((Math.random() * (COLS - 4)) | 0) + 2);
          flashUntil = now + 90;
        }
        paintTrailToBuf();
        // Flash overlay: fill empty cells with '.' briefly
        if (now < flashUntil) {
          for (let i = 0; i < buf.length; i++) {
            if (buf[i] === SPACE && Math.random() < 0.25) {
              buf[i] = 46 /*'.'*/;
            }
          }
        }
      }

      /* ─── Scene: Rain (slanted fall + bottom splash row) ─── */
      const rainN = isMobile ? 50 : 90;
      const rainDrops = new Float32Array(rainN * 3); // x, y, speed
      let rainSplashRow = new Uint8Array(COLS);
      function initRain() {
        for (let i = 0; i < rainN; i++) {
          rainDrops[i * 3 + 0] = Math.random() * COLS;
          rainDrops[i * 3 + 1] = Math.random() * ROWS;
          rainDrops[i * 3 + 2] = 6 + Math.random() * 8;
        }
      }
      initRain();
      function drawRain(now, dt) {
        if (rainSplashRow.length !== COLS) rainSplashRow = new Uint8Array(COLS);
        clearBuf();
        const step = (dt || 16) / 1000;
        // Decay splash row
        for (let x = 0; x < COLS; x++) {
          const v = rainSplashRow[x] - 12;
          rainSplashRow[x] = v < 0 ? 0 : v;
        }
        for (let i = 0; i < rainN; i++) {
          const b = i * 3;
          rainDrops[b + 0] += step * rainDrops[b + 2] * 0.4; // horizontal drift (slant)
          rainDrops[b + 1] += step * rainDrops[b + 2];
          if (rainDrops[b + 1] >= ROWS - 1) {
            const sx = ((rainDrops[b + 0] | 0) + COLS) % COLS;
            rainSplashRow[sx] = 200;
            rainDrops[b + 0] = Math.random() * COLS;
            rainDrops[b + 1] = -Math.random() * 4;
            rainDrops[b + 2] = 6 + Math.random() * 8;
            continue;
          }
          const x = ((rainDrops[b + 0] | 0) + COLS) % COLS;
          const y = rainDrops[b + 1] | 0;
          if (y >= 0 && y < ROWS - 1 && x >= 0 && x < COLS) {
            buf[y * COLS + x] = 47 /*'/'*/;
          }
        }
        // Bottom row splash
        const baseRow = ROWS - 1;
        for (let x = 0; x < COLS; x++) {
          const g = rainSplashRow[x];
          if (g > 150) buf[baseRow * COLS + x] = 42 /*'*'*/;
          else if (g > 70) buf[baseRow * COLS + x] = 46 /*'.'*/;
          else if (g > 0) buf[baseRow * COLS + x] = 95 /*'_'*/;
          else buf[baseRow * COLS + x] = 95 /*'_'*/;
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'FOUNTAIN',   fn: drawFountain,   dur: 7000, cls: 'fx-fountain' },` (added in Task 14).

Insert *after* that line:

```js
        { name: 'SNAKE',      fn: drawSnake,      dur: 7000, cls: 'fx-snake' },
        { name: 'LIGHTNING',  fn: drawLightning,  dur: 6000, cls: 'fx-lightning' },
        { name: 'RAIN',       fn: drawRain,       dur: 7000, cls: 'fx-rain' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify all three**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['SNAKE', 'LIGHTNING', 'RAIN']) {
    out.push(await window.__fxVerify(n, 2500));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('SNAKE')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/SNAKE.png
mcp__playwright__browser_evaluate → () => window.__fxJump('LIGHTNING')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/LIGHTNING.png
mcp__playwright__browser_evaluate → () => window.__fxJump('RAIN')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/RAIN.png
```

Expected: SNAKE `nonSpacePct` between 5 and 30 (depends on length at sample time), LIGHTNING `nonSpacePct > 5`, RAIN `nonSpacePct > 20` (drops + splash row). All `distinctChars >= 3`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): SNAKE, LIGHTNING, RAIN (procedural particle effects)

- Snake: procedural worm with random direction changes; reseeds on
  self-collision
- Lightning: timed Brownian bolts root → fork events with brief
  '.'-flash overlay
- Rain: slanted '/' drops with bottom-row splash decay

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 4 — New effects: Math / fractal family

### Task 16: Add CLIFFORD, LORENZ, FRACTAL_TREE

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add three CSS color classes**

Anchor: search for `.fx-canvas.fx-rain {` (added in Task 15).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-clifford {
      color: #d8a8ff;
      text-shadow: -1px 0 rgba(0,200,255,0.4), 1px 0 rgba(255,80,200,0.4), 0 0 8px rgba(200,160,255,0.5);
    }
    .fx-canvas.fx-lorenz {
      color: #ffb678;
      text-shadow: -1px 0 rgba(255,40,120,0.4), 1px 0 rgba(255,200,40,0.4), 0 0 8px rgba(255,160,80,0.5);
    }
    .fx-canvas.fx-tree {
      color: #c2ff8a;
      text-shadow: -1px 0 rgba(120,80,40,0.45), 1px 0 rgba(160,255,100,0.45), 0 0 8px rgba(180,255,140,0.4);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */`.

Insert *immediately before* that comment:

```js
      /* ─── Scene: Clifford (strange attractor scatter, additive trail) ─── */
      const cliffStateN = isMobile ? 600 : 1200;
      let cliffParams = { a: -1.4, b: 1.6, c: 1.0, d: 0.7 };
      let cliffMorphT = 0;
      function drawClifford(now, dt) {
        ensureTrailGeom();
        decayTrail(8);
        cliffMorphT += dt * 0.0001;
        cliffParams.a = -1.4 + Math.sin(cliffMorphT * 1.1) * 0.6;
        cliffParams.b =  1.6 + Math.cos(cliffMorphT * 0.9) * 0.5;
        cliffParams.c =  1.0 + Math.sin(cliffMorphT * 0.7 + 1) * 0.4;
        cliffParams.d =  0.7 + Math.cos(cliffMorphT * 1.3 + 2) * 0.4;
        let x = 0.1, y = 0.1;
        const cx = COLS / 2, cy = ROWS / 2;
        const sx = COLS * 0.3, sy = ROWS * 0.3;
        for (let i = 0; i < cliffStateN; i++) {
          const xn = Math.sin(cliffParams.a * y) + cliffParams.c * Math.cos(cliffParams.a * x);
          const yn = Math.sin(cliffParams.b * x) + cliffParams.d * Math.cos(cliffParams.b * y);
          x = xn; y = yn;
          if (i < 30) continue; // burn-in
          const px = Math.round(cx + x * sx);
          const py = Math.round(cy + y * sy);
          addTrail(px, py, 50);
        }
        paintTrailToBuf();
      }

      /* ─── Scene: Lorenz (butterfly attractor with auto-rotation) ─── */
      let lorenzX = 0.1, lorenzY = 0, lorenzZ = 0;
      function drawLorenz(now, dt) {
        ensureTrailGeom();
        decayTrail(10);
        const rot = now * 0.0003;
        const cosR = Math.cos(rot), sinR = Math.sin(rot);
        const cx = COLS / 2, cy = ROWS / 2;
        const stepN = isMobile ? 250 : 500;
        const h = 0.008;
        const sigma = 10, rho = 28, beta = 8 / 3;
        for (let i = 0; i < stepN; i++) {
          const dx = sigma * (lorenzY - lorenzX);
          const dy = lorenzX * (rho - lorenzZ) - lorenzY;
          const dz = lorenzX * lorenzY - beta * lorenzZ;
          lorenzX += h * dx;
          lorenzY += h * dy;
          lorenzZ += h * dz;
          // Project: rotate XY by rot, drop Z (or use as brightness)
          const px = lorenzX * cosR - lorenzY * sinR;
          const py = lorenzX * sinR + lorenzY * cosR;
          const sx = Math.round(cx + px * (COLS * 0.018));
          const sy = Math.round(cy - (lorenzZ - 25) * (ROWS * 0.022));
          addTrail(sx, sy, 80);
        }
        paintTrailToBuf();
      }

      /* ─── Scene: Fractal tree (recursive L-system with wind sway) ─── */
      function treeBranch(x, y, len, angle, depth) {
        if (depth <= 0 || len < 0.5) return;
        const dx = Math.cos(angle) * len;
        const dy = Math.sin(angle) * len * 0.5; // chars are ~2x tall — squash y
        const x1 = x + dx, y1 = y + dy;
        // Bresenham line draw with depth-based char
        const ch = depth > 4 ? 35 /*'#'*/ : depth > 2 ? 124 /*'|'*/ : 47 /*'/'*/;
        let cx0 = Math.round(x), cy0 = Math.round(y);
        const cx1 = Math.round(x1), cy1 = Math.round(y1);
        const ldx = Math.abs(cx1 - cx0), ldy = -Math.abs(cy1 - cy0);
        const sx = cx0 < cx1 ? 1 : -1, sy = cy0 < cy1 ? 1 : -1;
        let err = ldx + ldy;
        while (true) {
          if (cx0 >= 0 && cx0 < COLS && cy0 >= 0 && cy0 < ROWS) {
            buf[cy0 * COLS + cx0] = ch;
          }
          if (cx0 === cx1 && cy0 === cy1) break;
          const e2 = 2 * err;
          if (e2 >= ldy) { err += ldy; cx0 += sx; }
          if (e2 <= ldx) { err += ldx; cy0 += sy; }
        }
        // Recurse two branches
        treeBranch(x1, y1, len * 0.72, angle - 0.4, depth - 1);
        treeBranch(x1, y1, len * 0.72, angle + 0.4, depth - 1);
      }
      function drawFractalTree(now) {
        clearBuf();
        const t = now * 0.001;
        const sway = Math.sin(t * 0.8) * 0.15;
        const rootX = COLS / 2;
        const rootY = ROWS - 1;
        const len = ROWS * 0.45;
        const angle = -Math.PI / 2 + sway;
        const maxDepth = isMobile ? 6 : 7;
        treeBranch(rootX, rootY, len, angle, maxDepth);
        // Ground
        for (let x = 0; x < COLS; x++) {
          if (buf[(ROWS - 1) * COLS + x] === SPACE) buf[(ROWS - 1) * COLS + x] = 95 /*'_'*/;
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'RAIN',       fn: drawRain,       dur: 7000, cls: 'fx-rain' },` (added in Task 15).

Insert *after* that line:

```js
        { name: 'CLIFFORD',   fn: drawClifford,   dur: 7000, cls: 'fx-clifford' },
        { name: 'LORENZ',     fn: drawLorenz,     dur: 8000, cls: 'fx-lorenz' },
        { name: 'TREE',       fn: drawFractalTree,dur: 7000, cls: 'fx-tree' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['CLIFFORD', 'LORENZ', 'TREE']) {
    out.push(await window.__fxVerify(n, 2500));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('CLIFFORD')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/CLIFFORD.png
mcp__playwright__browser_evaluate → () => window.__fxJump('LORENZ')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/LORENZ.png
mcp__playwright__browser_evaluate → () => window.__fxJump('TREE')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/TREE.png
```

Expected: CLIFFORD `nonSpacePct > 15`, LORENZ `nonSpacePct > 8` (tracing line), TREE `nonSpacePct > 15`. All `distinctChars >= 3`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): CLIFFORD, LORENZ, TREE (math/fractal family)

- Clifford: strange attractor scatter with morphing parameters
- Lorenz: butterfly attractor with auto-rotation, additive trail
- Tree: recursive L-system fractal tree with wind sway

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 17: Add MANDELBOX, HILBERT, JULIA

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add three CSS color classes**

Anchor: search for `.fx-canvas.fx-tree {` (added in Task 16).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-mbox {
      color: #ffb0d5;
      text-shadow: -1px 0 rgba(255,80,200,0.5), 1px 0 rgba(0,200,255,0.4), 0 0 8px rgba(255,160,200,0.5);
    }
    .fx-canvas.fx-hilbert {
      color: #b0d0ff;
      text-shadow: -1px 0 rgba(180,80,255,0.4), 1px 0 rgba(0,200,255,0.5), 0 0 8px rgba(160,200,255,0.45);
    }
    .fx-canvas.fx-julia {
      color: #ffd28a;
      text-shadow: -1px 0 rgba(255,40,180,0.4), 1px 0 rgba(255,200,40,0.4), 0 0 8px rgba(255,180,80,0.5);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */`.

Insert *immediately before* that comment:

```js
      /* ─── Scene: Mandelbox (animated 2D slice) ─── */
      function drawMandelbox(now) {
        const t = now * 0.0005;
        const scale = -1.5 + Math.sin(t) * 0.7;
        const minRad2 = 0.5, fixedRad2 = 1.0;
        const cx = -0.05 + Math.sin(t * 0.4) * 0.3;
        const cy =  0.05 + Math.cos(t * 0.3) * 0.3;
        const maxIter = 12;
        for (let py = 0; py < ROWS; py++) {
          for (let px = 0; px < COLS; px++) {
            let x = (px - COLS / 2) / COLS * 4;
            let y = (py - ROWS / 2) / ROWS * 4;
            const cX = cx, cY = cy;
            let i = 0;
            for (; i < maxIter; i++) {
              // Box fold
              if (x > 1) x = 2 - x; else if (x < -1) x = -2 - x;
              if (y > 1) y = 2 - y; else if (y < -1) y = -2 - y;
              // Sphere fold
              const r2 = x * x + y * y;
              if (r2 < minRad2) { const m = fixedRad2 / minRad2; x *= m; y *= m; }
              else if (r2 < fixedRad2) { const m = fixedRad2 / r2; x *= m; y *= m; }
              x = x * scale + cX;
              y = y * scale + cY;
              if (x * x + y * y > 100) break;
            }
            const norm = i / maxIter;
            const idx = Math.max(1, Math.min(RAMP_LEN - 1, (norm * RAMP_LEN) | 0));
            buf[py * COLS + px] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Hilbert curve (progressive draw + unwind) ─── */
      // Snake the canvas: build a Hilbert-like ordering that fits COLS×ROWS.
      // For ASCII clarity we approximate with a recursive Z-curve at order n
      // sized to fit, then animate progressive reveal.
      function hilbertPath() {
        // Build a path using Hilbert curve over the largest power-of-2 square
        // that fits in COLS×ROWS, then translate to canvas coords.
        const side = 1 << Math.floor(Math.log2(Math.min(COLS, ROWS)));
        function hilbert(n, x, y, xi, xj, yi, yj, out) {
          if (n <= 0) {
            out.push([x + (xi + yi) / 2, y + (xj + yj) / 2]);
          } else {
            hilbert(n - 1, x, y, yi/2, yj/2, xi/2, xj/2, out);
            hilbert(n - 1, x + xi/2, y + xj/2, xi/2, xj/2, yi/2, yj/2, out);
            hilbert(n - 1, x + xi/2 + yi/2, y + xj/2 + yj/2, xi/2, xj/2, yi/2, yj/2, out);
            hilbert(n - 1, x + xi/2 + yi, y + xj/2 + yj, -yi/2, -yj/2, -xi/2, -xj/2, out);
          }
        }
        const path = [];
        const order = Math.log2(side);
        hilbert(order, 0, 0, side, 0, 0, side, path);
        // Center the path inside the canvas
        const ox = ((COLS - side) / 2) | 0;
        const oy = ((ROWS - side) / 2) | 0;
        return path.map(([x, y]) => [Math.round(x) + ox, Math.round(y) + oy]);
      }
      let hilbertCache = null;
      let hilbertGeomRev = -1;
      function drawHilbert(now) {
        if (hilbertGeomRev !== geometryRev || !hilbertCache) {
          hilbertCache = hilbertPath();
          hilbertGeomRev = geometryRev;
        }
        clearBuf();
        const path = hilbertCache;
        const total = path.length;
        // Animate: 0→1 reveal, then 1→0 unwind, ping-pong
        const t = (now * 0.001) % 8;
        const phase = t < 4 ? t / 4 : 1 - (t - 4) / 4;
        const visible = Math.floor(phase * total);
        for (let i = 0; i < visible; i++) {
          const [x, y] = path[i];
          if (x < 0 || x >= COLS || y < 0 || y >= ROWS) continue;
          const bright = 1 - (visible - i) / total;
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
          buf[y * COLS + x] = i === visible - 1 ? 64 /*'@'*/ : RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Julia set (animated complex parameter) ─── */
      function drawJulia(now) {
        const t = now * 0.0004;
        // c traces a closed loop in the complex plane
        const cR = 0.7885 * Math.cos(t);
        const cI = 0.7885 * Math.sin(t);
        const maxIter = 36;
        for (let py = 0; py < ROWS; py++) {
          for (let px = 0; px < COLS; px++) {
            let x = (px - COLS / 2) / COLS * 3.2;
            let y = (py - ROWS / 2) / ROWS * 2.4;
            let i = 0, r2 = 0;
            while (i < maxIter && (r2 = x * x + y * y) < 4) {
              const xn = x * x - y * y + cR;
              y = 2 * x * y + cI;
              x = xn;
              i++;
            }
            if (i === maxIter) {
              buf[py * COLS + px] = 35 /*'#'*/;
            } else {
              const norm = i / maxIter;
              const idx = Math.max(1, Math.min(RAMP_LEN - 1, (norm * RAMP_LEN) | 0));
              buf[py * COLS + px] = RAMP.charCodeAt(idx);
            }
          }
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'TREE',       fn: drawFractalTree,dur: 7000, cls: 'fx-tree' },` (added in Task 16).

Insert *after* that line:

```js
        { name: 'MANDELBOX',  fn: drawMandelbox,  dur: 7000, cls: 'fx-mbox' },
        { name: 'HILBERT',    fn: drawHilbert,    dur: 8000, cls: 'fx-hilbert' },
        { name: 'JULIA',      fn: drawJulia,      dur: 7000, cls: 'fx-julia' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['MANDELBOX', 'HILBERT', 'JULIA']) {
    out.push(await window.__fxVerify(n, 3000));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('MANDELBOX')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/MANDELBOX.png
mcp__playwright__browser_evaluate → () => window.__fxJump('HILBERT')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/HILBERT.png
mcp__playwright__browser_evaluate → () => window.__fxJump('JULIA')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/JULIA.png
```

Expected: MANDELBOX `nonSpacePct > 70`, HILBERT `nonSpacePct` between 5 and 80 (depends on phase at sample), JULIA `nonSpacePct > 60`. All `distinctChars >= 4`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): MANDELBOX, HILBERT, JULIA (math/fractal family)

- Mandelbox: animated 2D Mandelbox slice with sweeping scale
- Hilbert: progressive Hilbert curve, ping-pong reveal/unwind
- Julia: animated Julia set with c tracing a closed loop

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 5 — New effects: Pattern / grid family

### Task 18: Add REACTION_DIFFUSION, VORONOI

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add two CSS color classes**

Anchor: search for `.fx-canvas.fx-julia {` (added in Task 17).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-rd {
      color: #a8ffd0;
      text-shadow: -1px 0 rgba(0,200,180,0.45), 1px 0 rgba(120,255,200,0.45), 0 0 8px rgba(140,255,200,0.5);
    }
    .fx-canvas.fx-voronoi {
      color: #ffc8a0;
      text-shadow: -1px 0 rgba(255,80,160,0.4), 1px 0 rgba(255,200,80,0.4), 0 0 8px rgba(255,180,120,0.5);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */`.

Insert *immediately before* that comment:

```js
      /* ─── Scene: Reaction-Diffusion (Gray-Scott on COLS×ROWS) ─── */
      let rdU = new Float32Array(COLS * ROWS);
      let rdV = new Float32Array(COLS * ROWS);
      let rdU2 = new Float32Array(COLS * ROWS);
      let rdV2 = new Float32Array(COLS * ROWS);
      let rdAcc = 0;
      let rdAge = 0;
      let rdGeomRev = -1;
      function seedRD() {
        rdU.fill(1); rdV.fill(0);
        // Seed several blobs
        const blobs = 5;
        for (let k = 0; k < blobs; k++) {
          const cx = ((Math.random() * (COLS - 8)) | 0) + 4;
          const cy = ((Math.random() * (ROWS - 6)) | 0) + 3;
          for (let dy = -2; dy <= 2; dy++) {
            for (let dx = -2; dx <= 2; dx++) {
              const x = cx + dx, y = cy + dy;
              if (x < 0 || x >= COLS || y < 0 || y >= ROWS) continue;
              rdV[y * COLS + x] = 1;
            }
          }
        }
        rdAge = 0;
      }
      function rdStep() {
        const Du = 0.16, Dv = 0.08, F = 0.035, K = 0.06;
        for (let y = 1; y < ROWS - 1; y++) {
          for (let x = 1; x < COLS - 1; x++) {
            const i = y * COLS + x;
            const u = rdU[i], v = rdV[i];
            const lapU = (rdU[i - 1] + rdU[i + 1] + rdU[i - COLS] + rdU[i + COLS]) - 4 * u;
            const lapV = (rdV[i - 1] + rdV[i + 1] + rdV[i - COLS] + rdV[i + COLS]) - 4 * v;
            const uvv = u * v * v;
            rdU2[i] = u + (Du * lapU - uvv + F * (1 - u));
            rdV2[i] = v + (Dv * lapV + uvv - (K + F) * v);
          }
        }
        const tu = rdU; rdU = rdU2; rdU2 = tu;
        const tv = rdV; rdV = rdV2; rdV2 = tv;
      }
      function drawRD(now, dt) {
        if (rdGeomRev !== geometryRev) {
          rdU = new Float32Array(COLS * ROWS);
          rdV = new Float32Array(COLS * ROWS);
          rdU2 = new Float32Array(COLS * ROWS);
          rdV2 = new Float32Array(COLS * ROWS);
          seedRD();
          rdGeomRev = geometryRev;
        }
        rdAcc += dt;
        while (rdAcc > 18) {
          rdAcc -= 18;
          rdStep();
          rdAge++;
          if (rdAge > 600) { seedRD(); }
        }
        for (let i = 0; i < rdU.length; i++) {
          const v = rdV[i];
          const idx = Math.max(0, Math.min(RAMP_LEN - 1, (v * RAMP_LEN * 1.6) | 0));
          buf[i] = idx === 0 ? SPACE : RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Voronoi (drifting seed points, cell-id ramp) ─── */
      const vorN = isMobile ? 8 : 14;
      const vorSeeds = new Float32Array(vorN * 4); // x, y, vx, vy
      function initVoronoi() {
        for (let i = 0; i < vorN; i++) {
          vorSeeds[i * 4 + 0] = Math.random() * COLS;
          vorSeeds[i * 4 + 1] = Math.random() * ROWS;
          vorSeeds[i * 4 + 2] = (Math.random() - 0.5) * 6;
          vorSeeds[i * 4 + 3] = (Math.random() - 0.5) * 4;
        }
      }
      initVoronoi();
      function drawVoronoi(now, dt) {
        const step = (dt || 16) / 1000;
        for (let i = 0; i < vorN; i++) {
          const b = i * 4;
          vorSeeds[b + 0] += vorSeeds[b + 2] * step;
          vorSeeds[b + 1] += vorSeeds[b + 3] * step;
          if (vorSeeds[b + 0] < 0 || vorSeeds[b + 0] >= COLS) vorSeeds[b + 2] *= -1;
          if (vorSeeds[b + 1] < 0 || vorSeeds[b + 1] >= ROWS) vorSeeds[b + 3] *= -1;
        }
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            let nearest = 0, bestD = 1e9, second = 1e9;
            for (let i = 0; i < vorN; i++) {
              const b = i * 4;
              const dx = x - vorSeeds[b + 0];
              const dy = (y - vorSeeds[b + 1]) * CHAR_ASPECT;
              const d = dx * dx + dy * dy;
              if (d < bestD) { second = bestD; bestD = d; nearest = i; }
              else if (d < second) { second = d; }
            }
            // Edge intensity: ratio of nearest to second-nearest
            const edge = bestD / Math.max(0.001, second);
            // Hot cells near edges read as low brightness; interior cells map to id
            let idx;
            if (edge > 0.8) {
              idx = RAMP_LEN - 1; // bright edge
            } else {
              const id = (nearest * 7) % RAMP_LEN;
              idx = Math.max(2, Math.min(RAMP_LEN - 4, id));
            }
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'JULIA',      fn: drawJulia,      dur: 7000, cls: 'fx-julia' },` (added in Task 17).

Insert *after* that line:

```js
        { name: 'REACTION',   fn: drawRD,         dur: 8000, cls: 'fx-rd' },
        { name: 'VORONOI',    fn: drawVoronoi,    dur: 7000, cls: 'fx-voronoi' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['REACTION', 'VORONOI']) {
    out.push(await window.__fxVerify(n, 3000));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('REACTION')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/REACTION.png
mcp__playwright__browser_evaluate → () => window.__fxJump('VORONOI')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/VORONOI.png
```

Expected: REACTION `nonSpacePct > 20` (Turing pattern formation), VORONOI `nonSpacePct > 70`. Both `distinctChars >= 5`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): REACTION, VORONOI (pattern/grid family)

- Reaction: Gray-Scott reaction-diffusion on COLS×ROWS grid; multi-seed
  with periodic reseed
- Voronoi: drifting seed points, cell-id ramp with bright cell-edge
  detection via second-nearest-distance ratio

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 19: Add HEX_GRID, CONWAY_HIGHLIFE

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add two CSS color classes**

Anchor: search for `.fx-canvas.fx-voronoi {` (added in Task 18).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-hex {
      color: #ff8aa6;
      text-shadow: -1px 0 rgba(0,200,255,0.4), 1px 0 rgba(255,180,40,0.4), 0 0 8px rgba(255,140,160,0.5);
    }
    .fx-canvas.fx-highlife {
      color: #f0e890;
      text-shadow: -1px 0 rgba(255,80,160,0.35), 1px 0 rgba(180,255,80,0.35), 0 0 8px rgba(220,200,80,0.45);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */`.

Insert *immediately before* that comment:

```js
      /* ─── Scene: HexGrid (hexagonal pulse waves outward from center) ─── */
      function drawHexGrid(now) {
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS / 2;
        const HEX_W = 4; // approx hex tile width in chars
        const HEX_H = 3;
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            // Convert pixel to axial-ish hex coord
            const q = Math.round((x - cx) / HEX_W);
            const r = Math.round(((y - cy) / HEX_H) - q * 0.5);
            // Distance in hex coords
            const hexDist = (Math.abs(q) + Math.abs(r) + Math.abs(-q - r)) / 2;
            const wave = Math.sin(hexDist * 0.8 - t * 3) * 0.5 + 0.5;
            const wave2 = Math.cos(hexDist * 0.4 - t * 1.5) * 0.4 + 0.5;
            const v = wave * 0.7 + wave2 * 0.3;
            const idx = Math.max(0, Math.min(RAMP_LEN - 1, (v * RAMP_LEN) | 0));
            buf[y * COLS + x] = idx === 0 ? SPACE : RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: HighLife (B36/S23 variant of Conway's Life) ─── */
      let hlCurr = new Uint8Array(COLS * ROWS);
      let hlNext = new Uint8Array(COLS * ROWS);
      let hlAcc = 0;
      let hlAge = 0;
      let hlGeomRev = -1;
      function seedHighLife() {
        for (let i = 0; i < hlCurr.length; i++) hlCurr[i] = Math.random() < 0.28 ? 1 : 0;
        hlAge = 0;
      }
      function stepHighLife() {
        for (let y = 0; y < ROWS; y++) {
          const ym1 = (y - 1 + ROWS) % ROWS;
          const yp1 = (y + 1) % ROWS;
          for (let x = 0; x < COLS; x++) {
            const xm1 = (x - 1 + COLS) % COLS;
            const xp1 = (x + 1) % COLS;
            const n = hlCurr[ym1 * COLS + xm1] + hlCurr[ym1 * COLS + x] + hlCurr[ym1 * COLS + xp1] +
                      hlCurr[y   * COLS + xm1] +                              hlCurr[y   * COLS + xp1] +
                      hlCurr[yp1 * COLS + xm1] + hlCurr[yp1 * COLS + x] + hlCurr[yp1 * COLS + xp1];
            const alive = hlCurr[y * COLS + x];
            // B36/S23: born on 3 or 6, survive on 2 or 3
            hlNext[y * COLS + x] = alive ? ((n === 2 || n === 3) ? 1 : 0) : ((n === 3 || n === 6) ? 1 : 0);
          }
        }
        const tmp = hlCurr; hlCurr = hlNext; hlNext = tmp;
      }
      function drawHighLife(now, dt) {
        if (hlGeomRev !== geometryRev) {
          hlCurr = new Uint8Array(COLS * ROWS);
          hlNext = new Uint8Array(COLS * ROWS);
          seedHighLife();
          hlGeomRev = geometryRev;
        }
        hlAcc += dt;
        if (hlAcc > 130) {
          hlAcc = 0;
          stepHighLife();
          hlAge++;
          if (hlAge > 50) seedHighLife();
        }
        for (let i = 0; i < hlCurr.length; i++) {
          buf[i] = hlCurr[i] ? 35 /*'#'*/ : 32;
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'VORONOI',    fn: drawVoronoi,    dur: 7000, cls: 'fx-voronoi' },` (added in Task 18).

Insert *after* that line:

```js
        { name: 'HEXGRID',    fn: drawHexGrid,    dur: 6000, cls: 'fx-hex' },
        { name: 'HIGHLIFE',   fn: drawHighLife,   dur: 7000, cls: 'fx-highlife' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['HEXGRID', 'HIGHLIFE']) {
    out.push(await window.__fxVerify(n, 2500));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('HEXGRID')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/HEXGRID.png
mcp__playwright__browser_evaluate → () => window.__fxJump('HIGHLIFE')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/HIGHLIFE.png
```

Expected: HEXGRID `nonSpacePct > 60`, HIGHLIFE `nonSpacePct` between 5 and 50. Both `distinctChars >= 4` (HIGHLIFE only 2: `#` and space).

Note: HIGHLIFE may have `distinctChars === 2`. Acceptable for cellular automata.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): HEXGRID, HIGHLIFE (pattern/grid family)

- HexGrid: hexagonal pulse waves expanding outward from center,
  composited from two phase-offset sines
- HighLife: B36/S23 Life variant; produces self-replicators

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 6 — New effects: 3D / scene family

### Task 20: Add RAYMARCH_SDF, DNA_HELIX

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add two CSS color classes**

Anchor: search for `.fx-canvas.fx-highlife {` (added in Task 19).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-sdf {
      color: #c8e0ff;
      text-shadow: -1px 0 rgba(180,80,255,0.4), 1px 0 rgba(0,200,255,0.5), 0 0 8px rgba(200,220,255,0.55);
    }
    .fx-canvas.fx-dna {
      color: #ff9eea;
      text-shadow: -1px 0 rgba(0,220,255,0.4), 1px 0 rgba(255,80,180,0.45), 0 0 8px rgba(255,160,220,0.5);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */`.

Insert *immediately before* that comment:

```js
      /* ─── Scene: Raymarch SDF (sphere + torus distance field, lambert shading) ─── */
      function sdSphere(px, py, pz, r) {
        return Math.sqrt(px*px + py*py + pz*pz) - r;
      }
      function sdTorus(px, py, pz, R, r) {
        const qx = Math.sqrt(px*px + pz*pz) - R;
        return Math.sqrt(qx*qx + py*py) - r;
      }
      function sdfScene(px, py, pz, t) {
        // Rotate space around Y axis
        const c = Math.cos(t * 0.7), s = Math.sin(t * 0.7);
        const rx = px * c - pz * s;
        const rz = px * s + pz * c;
        const a = sdSphere(rx, py, rz, 0.7);
        const b = sdTorus(rx, py, rz, 1.2, 0.25);
        return Math.min(a, b);
      }
      function drawRaymarch(now) {
        const t = now * 0.001;
        const camZ = -3.5;
        const lightX = Math.cos(t) * 0.7, lightY = -0.5, lightZ = Math.sin(t) * 0.7;
        const lLen = Math.sqrt(lightX*lightX + lightY*lightY + lightZ*lightZ);
        const lx = lightX / lLen, ly = lightY / lLen, lz = lightZ / lLen;
        for (let py = 0; py < ROWS; py++) {
          for (let px = 0; px < COLS; px++) {
            // Ray dir
            const u = (px - COLS / 2) / COLS * 2;
            const v = -(py - ROWS / 2) / ROWS * 2 * (ROWS / COLS) * (CHAR_ASPECT * 0.5);
            const rdLen = Math.sqrt(u*u + v*v + 1);
            const rdX = u / rdLen, rdY = v / rdLen, rdZ = 1 / rdLen;
            let dist = 0, hit = false, hx = 0, hy = 0, hz = 0;
            for (let step = 0; step < 24; step++) {
              hx = rdX * dist;
              hy = rdY * dist;
              hz = camZ + rdZ * dist;
              const d = sdfScene(hx, hy, hz, t);
              if (d < 0.01) { hit = true; break; }
              dist += d;
              if (dist > 8) break;
            }
            if (!hit) { buf[py * COLS + px] = SPACE; continue; }
            // Estimate normal via central differences
            const e = 0.05;
            const nx = sdfScene(hx + e, hy, hz, t) - sdfScene(hx - e, hy, hz, t);
            const ny = sdfScene(hx, hy + e, hz, t) - sdfScene(hx, hy - e, hz, t);
            const nz = sdfScene(hx, hy, hz + e, t) - sdfScene(hx, hy, hz - e, t);
            const nLen = Math.sqrt(nx*nx + ny*ny + nz*nz) || 1;
            const dot = (nx / nLen) * lx + (ny / nLen) * ly + (nz / nLen) * lz;
            const bright = Math.max(0.1, dot * 0.5 + 0.5);
            const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
            buf[py * COLS + px] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: DNA helix (rotating double helix with rungs) ─── */
      function drawDnaHelix(now) {
        clearBuf();
        const t = now * 0.001;
        const cx = COLS / 2;
        const amp = COLS * 0.32;
        const turn = 0.8; // y units per radian
        for (let y = 0; y < ROWS; y++) {
          const phase = y * turn + t * 2;
          const x1 = cx + Math.cos(phase) * amp;
          const x2 = cx + Math.cos(phase + Math.PI) * amp;
          const z1 = Math.sin(phase);             // -1..1
          const z2 = Math.sin(phase + Math.PI);
          const sx1 = Math.round(x1), sx2 = Math.round(x2);
          // Brightness from depth
          const idx1 = Math.max(2, Math.min(RAMP_LEN - 1, ((z1 * 0.5 + 0.5) * RAMP_LEN) | 0));
          const idx2 = Math.max(2, Math.min(RAMP_LEN - 1, ((z2 * 0.5 + 0.5) * RAMP_LEN) | 0));
          if (sx1 >= 0 && sx1 < COLS) buf[y * COLS + sx1] = RAMP.charCodeAt(idx1);
          if (sx2 >= 0 && sx2 < COLS) buf[y * COLS + sx2] = RAMP.charCodeAt(idx2);
          // Rung every 3 rows: connect with '-'
          if (y % 3 === 0) {
            const lo = Math.min(sx1, sx2), hi = Math.max(sx1, sx2);
            for (let x = Math.max(0, lo + 1); x < Math.min(COLS, hi); x++) {
              if (buf[y * COLS + x] === SPACE) buf[y * COLS + x] = 45 /*'-'*/;
            }
          }
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'HIGHLIFE',   fn: drawHighLife,   dur: 7000, cls: 'fx-highlife' },` (added in Task 19).

Insert *after* that line:

```js
        { name: 'RAYMARCH',   fn: drawRaymarch,   dur: 8000, cls: 'fx-sdf' },
        { name: 'DNA',        fn: drawDnaHelix,   dur: 7000, cls: 'fx-dna' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['RAYMARCH', 'DNA']) {
    out.push(await window.__fxVerify(n, 2500));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('RAYMARCH')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/RAYMARCH.png
mcp__playwright__browser_evaluate → () => window.__fxJump('DNA')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/DNA.png
```

Expected: RAYMARCH `nonSpacePct > 20` (sphere+torus silhouette), DNA `nonSpacePct > 25`. Both `distinctChars >= 5`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): RAYMARCH, DNA (3D family)

- Raymarch: sphere + torus union via SDF, normal from central
  differences, lambertian shading from a rotating directional light
- DNA: rotating double helix with depth-shaded strands and periodic
  '-' rungs

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 21: Add GALAXY_SPIRAL, GLITCH_TEXT

**Files:**
- Modify: `static/index.html` (CSS, effect functions, SCENES entries)

- [ ] **Step 1: Add two CSS color classes**

Anchor: search for `.fx-canvas.fx-dna {` (added in Task 20).

Insert *after* its closing `}`:

```css
    .fx-canvas.fx-galaxy {
      color: #d8c8ff;
      text-shadow: -1px 0 rgba(180,80,255,0.4), 1px 0 rgba(0,200,255,0.4), 0 0 10px rgba(200,180,255,0.55);
    }
    .fx-canvas.fx-glitch {
      color: #00ff99;
      text-shadow: -2px 0 rgba(255,0,128,0.6), 2px 0 rgba(0,200,255,0.6), 0 0 6px rgba(0,255,150,0.5);
    }
```

- [ ] **Step 2: Add effect functions**

Anchor: search for `/* ─── BBS ticker scroller (bottom -> top) ─── */`.

Insert *immediately before* that comment:

```js
      /* ─── Scene: Galaxy spiral (log-spiral arm density) ─── */
      const galN = isMobile ? 220 : 450;
      const galStars = new Float32Array(galN * 3); // r, theta0, age
      function initGalaxy() {
        for (let i = 0; i < galN; i++) {
          galStars[i * 3 + 0] = Math.random() * 1.2 + 0.05;
          // Bias arm placement: 4 arms via theta0 quantization with noise
          const armId = (i * 4) % 4;
          galStars[i * 3 + 1] = (armId / 4) * Math.PI * 2 + (Math.random() - 0.5) * 0.4;
          galStars[i * 3 + 2] = Math.random();
        }
      }
      initGalaxy();
      function drawGalaxy(now) {
        clearBuf();
        const t = now * 0.0006;
        const cx = COLS / 2, cy = ROWS / 2;
        const sx = COLS * 0.42;
        const sy = ROWS * 0.42;
        // Bright center
        if (cx >= 0 && cy >= 0 && cx < COLS && cy < ROWS) {
          buf[((cy | 0) * COLS) + (cx | 0)] = 64 /*'@'*/;
        }
        for (let i = 0; i < galN; i++) {
          const b = i * 3;
          const r = galStars[b + 0];
          // Logarithmic spiral: theta = theta0 + k * log(r)
          const theta = galStars[b + 1] + Math.log(r + 0.1) * 3 - t;
          const px = Math.round(cx + Math.cos(theta) * r * sx);
          const py = Math.round(cy + Math.sin(theta) * r * sy);
          if (px < 0 || px >= COLS || py < 0 || py >= ROWS) continue;
          const bright = Math.max(0.15, 1 - r * 0.7);
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
          if (buf[py * COLS + px] === SPACE) buf[py * COLS + px] = RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Glitch text ("KD HOMEBREW" with data-mosh corruption) ─── */
      const GLITCH_TEXT = "KD  HOMEBREW";
      const GLITCH_POOL = "█▓▒░@#%&*+=<>/\\|01";
      function drawGlitchText(now) {
        clearBuf();
        const t = now * 0.001;
        // Cycle: 0..2 stable, 2..3 glitch, 3..5 recover
        const cyc = (t % 5);
        const corrupting = cyc > 1.8;
        const recoveryT = corrupting ? Math.max(0, (cyc - 1.8) / 1.2) : 0;
        const textRow = (ROWS / 2) | 0;
        const startX = Math.max(0, ((COLS - GLITCH_TEXT.length) / 2) | 0);
        for (let i = 0; i < GLITCH_TEXT.length; i++) {
          const x = startX + i;
          if (x < 0 || x >= COLS) continue;
          let ch = GLITCH_TEXT.charCodeAt(i);
          if (corrupting && Math.random() < recoveryT * 0.7) {
            ch = GLITCH_POOL.charCodeAt((Math.random() * GLITCH_POOL.length) | 0);
          }
          buf[textRow * COLS + x] = ch;
        }
        // Background data-mosh blocks
        const blocks = corrupting ? ((recoveryT * 12) | 0) : 0;
        for (let k = 0; k < blocks; k++) {
          const bx = (Math.random() * COLS) | 0;
          const by = (Math.random() * ROWS) | 0;
          const bw = 2 + ((Math.random() * 6) | 0);
          const bh = 1 + ((Math.random() * 2) | 0);
          for (let dy = 0; dy < bh; dy++) {
            for (let dx = 0; dx < bw; dx++) {
              const x = bx + dx, y = by + dy;
              if (x >= COLS || y >= ROWS) continue;
              if (y === textRow) continue; // don't trash the title row
              buf[y * COLS + x] = GLITCH_POOL.charCodeAt((Math.random() * GLITCH_POOL.length) | 0);
            }
          }
        }
        // Subtle ramp dots elsewhere
        if (!corrupting) {
          const dots = (COLS * ROWS * 0.08) | 0;
          for (let k = 0; k < dots; k++) {
            const x = (Math.random() * COLS) | 0;
            const y = (Math.random() * ROWS) | 0;
            if (y === textRow) continue;
            if (buf[y * COLS + x] === SPACE) buf[y * COLS + x] = 46 /*'.'*/;
          }
        }
      }
```

- [ ] **Step 3: Add SCENES entries**

Anchor: search for `{ name: 'DNA',        fn: drawDnaHelix,   dur: 7000, cls: 'fx-dna' },` (added in Task 20).

Insert *after* that line:

```js
        { name: 'GALAXY',     fn: drawGalaxy,     dur: 7000, cls: 'fx-galaxy' },
        { name: 'GLITCH',     fn: drawGlitchText, dur: 6000, cls: 'fx-glitch' },
```

- [ ] **Step 4: Restart server**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 5: Verify**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  const out = [];
  for (const n of ['GALAXY', 'GLITCH']) {
    out.push(await window.__fxVerify(n, 2500));
  }
  return out;
}
mcp__playwright__browser_evaluate → () => window.__fxJump('GALAXY')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/GALAXY.png
mcp__playwright__browser_evaluate → () => window.__fxJump('GLITCH')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/GLITCH.png
```

Expected: GALAXY `nonSpacePct > 12` (sparse star scatter on spiral), GLITCH `nonSpacePct` between 5 and 60 (depends on cycle phase). Both `distinctChars >= 5`.

- [ ] **Step 6: Commit**

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
feat(fx): GALAXY, GLITCH (3D family)

- Galaxy: log-spiral with 4 arms, brightness fades with radius,
  bright center
- Glitch: "KD HOMEBREW" with cyclic data-mosh corruption + recovery

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Phase 7 — Final verification sweep

### Task 22: Smoke-test all 46 scenes + capture screenshots

**Goal:** A single Playwright pass that visits every scene, asserts non-empty rendering, and saves a screenshot per scene to `.cat-audit/demoscene/`.

**Files:**
- No source changes (verification only)

- [ ] **Step 1: Restart server (clean)**

```bash
pkill -f /tmp/kdhome ; sleep 0.3 ; go build -o /tmp/kdhome ./... && PORT=8089 DATA_DIR=/tmp/kdhome-data /tmp/kdhome &
sleep 1
```

- [ ] **Step 2: Run the full verify pass**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => window.__fxList()
```

Capture the list. It should contain exactly 46 names: the 26 originals plus the 20 new ones (SHADEBOBS, BOBS, FOUNTAIN, SNAKE, LIGHTNING, RAIN, CLIFFORD, LORENZ, TREE, MANDELBOX, HILBERT, JULIA, REACTION, VORONOI, HEXGRID, HIGHLIFE, RAYMARCH, DNA, GALAXY, GLITCH).

```
mcp__playwright__browser_evaluate → async () => {
  const all = window.__fxList();
  const results = [];
  for (const n of all) {
    const r = await window.__fxVerify(n, 1800);
    results.push(r);
  }
  // Also check for any console errors collected since page load — Playwright
  // exposes them via browser_console_messages, sampled separately.
  return results;
}
```

Capture the array of `{name, ok, nonSpacePct, distinctChars, cols, rows}`.

- [ ] **Step 3: Assert minimums**

Programmatically check:
- All `ok === true`.
- All `distinctChars >= 2` (Cellular and HighLife may be exactly 2; everything else higher).
- All `nonSpacePct > 0`. Sparse effects (BOBS, VECTORS, STARFIELD, WARP, SPIRAL, FOUNTAIN, LIGHTNING) may be as low as 4 — anything ≥ 4 acceptable.

If any scene fails, jump to it and inspect visually:

```
mcp__playwright__browser_evaluate → () => window.__fxJump('FAILING_NAME')
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: /tmp/failed.png
```

Identify and fix root cause; re-run this task.

- [ ] **Step 4: Capture per-scene screenshots**

```
mcp__playwright__browser_evaluate → async () => {
  for (const n of window.__fxList()) {
    window.__fxJump(n);
    await new Promise(r => setTimeout(r, 1500));
  }
  return true;
}
```

(That just exercises each scene once for timing.) Then re-iterate with screenshots:

```
mcp__playwright__browser_evaluate → () => window.__fxFreeze() // freeze cycler
```

Then for each scene name `N`:

```
mcp__playwright__browser_evaluate → () => window.__fxJump('<N>')
mcp__playwright__browser_evaluate → async () => { await new Promise(r => setTimeout(r, 1800)); return true; }
mcp__playwright__browser_take_screenshot → element: .fx-canvas, filename: .cat-audit/demoscene/<N>.png
```

Run this sequence for all 46 names. (Subagent or simple shell loop driving the Playwright MCP works well — bag-shuffle is frozen so jumps land deterministically.)

- [ ] **Step 5: Console-error check**

```
mcp__playwright__browser_console_messages
```

Expected: no entries with type `error` or `warning` related to `fx-canvas` or `createFxInstance`. Pre-existing music/visit-counter messages are unrelated and OK.

- [ ] **Step 6: Verify the Demo window still works end-to-end**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → () => { const btn = document.querySelector('.entry-gate-btn'); if (btn) btn.click(); }
// wait ~600ms for gate dismiss
mcp__playwright__browser_evaluate → async () => { await new Promise(r => setTimeout(r, 600)); window.openDemoWindow(); return true; }
mcp__playwright__browser_evaluate → async () => { await new Promise(r => setTimeout(r, 1500)); return document.getElementById('demo-fx-canvas').textContent.length; }
```

Expected: returns a positive integer (>0), confirming the Demo window's separate FX instance also renders.

- [ ] **Step 7: Frame-rate spot check**

```
mcp__playwright__browser_navigate → http://localhost:8089/
mcp__playwright__browser_evaluate → async () => {
  let frames = 0;
  let lastT = performance.now();
  const deltas = [];
  return new Promise(r => {
    function tick(t) {
      const dt = t - lastT;
      deltas.push(dt);
      lastT = t;
      frames++;
      if (frames < 120) requestAnimationFrame(tick);
      else r({
        avgFps: 1000 / (deltas.reduce((a,b) => a+b, 0) / deltas.length),
        worstFrameMs: Math.max(...deltas)
      });
    }
    requestAnimationFrame(tick);
  });
}
```

Expected: `avgFps > 50`, `worstFrameMs < 50`. If avgFps is below 40, identify the heaviest scene (Mandelbrot, Mandelbox, Raymarch, Sphere3D, Voxel are candidates) and reduce its inner-loop iteration count for mobile.

- [ ] **Step 8: Final commit (only if no source changes were needed in steps 3 / 7)**

If steps 3 and 7 passed without code changes, this task has no commit. Otherwise, fold any fixes into a single follow-up commit:

```bash
git add static/index.html
git commit -m "$(cat <<'EOF'
fix(fx): minor cleanup found during verification sweep

[describe what was tightened]

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Done

At this point:
- All 12 bug fixes are committed
- 20 new effects are committed
- Verification harness (`__fxVerify`) is in place
- 46 screenshots in `.cat-audit/demoscene/` for human eyeball
- Demo window + entry gate both render correctly
- Frame rate hits 60 fps target on desktop

