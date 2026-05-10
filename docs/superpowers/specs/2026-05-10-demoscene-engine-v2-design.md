# Demoscene Engine v2 — Bug Fixes + 20 New Effects

**Date:** 2026-05-10
**Status:** Approved (brainstorming complete)
**Surface:** `static/index.html` lines 3890–5305 (`createFxInstance`)

## Context

A single `createFxInstance(cfg)` factory drives 26 ASCII demoscene effects on
two surfaces: the entry-gate intro pre-paint, and the "Demo" desktop window
opened from the start menu. The factory holds all per-effect state in closure
scope; each call mounts a fresh instance with its own buffers.

The engine has accumulated rendering bugs (some longstanding, some
intermittent), and the effect catalogue feels incomplete vs. what the format
can produce. This spec covers the bug audit, fixes, and a +20-effect expansion
to 46 total.

## Goals

1. Fix every confirmed rendering bug in the existing 26 effects.
2. Add 20 new effects spanning particle, math/fractal, pattern/grid, and 3D
   families.
3. Maintain 60 fps desktop / 30 fps mobile target.
4. Verify every effect (existing and new) with Playwright screenshot capture.

## Non-goals

- WebGL / canvas-2d rewrite. Stay in ASCII / `<pre>` rendering.
- New scene-director or transition styles (dissolve stays).
- New surfaces beyond entry gate + Demo window.
- Audio / music sync.

## Bug audit (12)

| # | Effect | Bug | Fix |
|---|--------|-----|-----|
| 1 | WAVE3D | Painter's order reversed: `gz=1` (near) drawn first, `gz=GRID` (far) last → far overwrites near; wave looks inside-out | Iterate `gz` from `GRID` down to `1` so near overwrites far |
| 2 | MATRIX | Single drop per column; tail uses fixed RAMP brightness ladder; no real "trail decay" | Add per-cell brightness buffer that decays each frame; allow 1–2 active drops per column; head bright, tail fades through ramp |
| 3 | LANDSCAPE | Stars don't twinkle (just slide); moon static; flat composition | Per-star phase + on/off probability; drifting moon; occasional shooting-star event; foreground tree silhouettes |
| 4 | TWISTER | Width capped at 18 of 46 cols → ~70% empty | Widen to ~70% of canvas; add side rails + diagonal body fill so it reads as a 3D twisting band |
| 5 | STARFIELD | Near-camera stars vanish off-canvas instead of streaking out | Cap to canvas; draw streak (2–3 cells along motion vector) when bright/near |
| 6 | TUNNEL | Hot pixel at exact center when COLS/ROWS even-aligned; lookup tables frozen across resize | Floor of distance via `max(0.6, d)` to soften hot pixel; recompute tables on canvas resize |
| 7 | SPHERE3D / VOXEL / VECTORS | Allocate `Float32Array` / `Int16Array` / `Array` every frame → GC churn | Hoist allocations to closure scope; reuse buffers |
| 8 | CELLULAR | O(ROWS×COLS) history scroll per step + per-step `new Uint8Array(COLS)` | Convert to ring buffer with row-index pointer; reuse `next` row |
| 9 | All effects | Hardcoded `*1.8` aspect compensation; correct ratio is ~2.0 desktop / 1.92 mobile | Replace with computed `CHAR_ASPECT` constant per instance |
| 10 | Engine | No resize handler → orientation flip leaves COLS/ROWS stale and tables wrong | Add `ResizeObserver` on canvas that recomputes COLS/ROWS, reallocates buffers, rebuilds lookup tables |
| 11 | MANDELBROT | Inside-set cells render as space → iconic black bulb is invisible | Render interior with `#`/`@` so the bulb shows; outside cells already use the smooth ramp |
| 12 | Engine | Scene class swaps to next colors instantly at transition start; only chars dissolve | Apply both classes during DISSOLVE_MS with cross-fade via inline `color`/`text-shadow` interpolation |

## New effects (20)

### Particle / motion (6)

- **SHADEBOBS** — 16 sine-orbiting bobs with additive trail buffer that decays
  ~0.85 per frame
- **BOBS** — Amiga-style bob cluster on Lissajous path, depth-sorted, glow
- **FOUNTAIN** — gravity-driven particle fountain spawning from bottom-center;
  rainbow tint per spawn batch
- **SNAKE** — procedural worm chasing its tail; rebuilds when it self-collides
- **LIGHTNING** — Brownian-motion bolts root → fork events with random branches;
  brief screen flash on strike
- **RAIN** — slanted '/' rain at ~70°; ground-row splash chars

### Math / fractal (6)

- **CLIFFORD** — Clifford strange attractor, 4-param `(a,b,c,d)` slowly morphs;
  scatter mode (additive)
- **LORENZ** — Lorenz butterfly orbiting in 3D with auto-rotation; trace as
  fading polyline
- **FRACTAL_TREE** — recursive L-system tree; wind sway from `sin(t)`; slow
  regrow loop
- **MANDELBOX** — animated 2D Mandelbox slice with `scale` parameter sweep
- **HILBERT** — Hilbert curve drawn progressively on a `2^n` grid, then unwinds
- **JULIA** — animated Julia set with morphing complex parameter `c` along a
  closed loop

### Pattern / grid (4)

- **REACTION_DIFFUSION** — Gray-Scott model on COLS×ROWS grid; visualize `v`
  through ramp; periodic reseed
- **VORONOI** — animated Voronoi cells with N drifting seed points; cell ID
  → ramp index
- **HEX_GRID** — hexagonal pulse waves outward from center; `dist`-based ramp
  with phase offset
- **CONWAY_HIGHLIFE** — B36/S23 variant of Life; produces replicator patterns

### 3D / scene (4)

- **RAYMARCH_SDF** — sphere + torus distance field; normal estimated via
  central differences; lambertian shading via ramp
- **DNA_HELIX** — rotating double helix; rung connections every N units
- **GALAXY_SPIRAL** — log-spiral galaxy with arm density falloff; slow rotation
- **GLITCH_TEXT** — "KD HOMEBREW" rendered then corrupted with data-mosh
  blocks; recovery cycles

## Engine architecture changes

1. **Allocation hoisting**: every per-frame `new` becomes closure-scope; each
   effect's helper buffers persist for the instance's lifetime.
2. **`CHAR_ASPECT` constant**: derived once from font metrics
   (`getComputedStyle(canvas)` line-height ÷ char-width). Replace 32 callsites.
3. **`ResizeObserver`**: subscribe on canvas mount; on real change (>2 px),
   recompute COLS/ROWS, reallocate `buf`/`prevBuf`, rebuild any lookup tables
   (TUNNEL `tunU/tunV/tunDist`, etc.), reseed dynamic effects.
4. **Trail helper**: shared `decayTrail(trailBuf, factor)` for shadebobs,
   fountain, lightning, rain.
5. **Color cross-fade during dissolve**: read `getComputedStyle` for both
   classes' `color` and `text-shadow` at transition start, interpolate via
   `requestAnimationFrame` setting inline styles for `DISSOLVE_MS` then strip.
6. **Scene catalogue**: extend `SCENES[]` to 46 entries. Bag picker is already
   generic — no change. Mobile `dur` stays per-scene.

## Verification plan

A Playwright harness (`scripts/verify-fx.js` or inline test) that:

1. Spins up the Go server (`go run main.go` on a free port).
2. Opens the page; auto-dismisses entry gate; opens Demo window.
3. For each scene name in `__fxList()`:
   - Calls `__fxFreeze(name)` then waits 2 s for settle.
   - Captures canvas `textContent` and asserts:
     - frame is not all-space (>10% non-space cells)
     - char variety: ≥4 distinct chars present
     - no `console.error` since last scene
   - Saves screenshot to `.cat-audit/demoscene/<name>.png`.
4. Reports pass/fail per scene; exits non-zero on any failure.

## Risks & mitigations

| Risk | Mitigation |
|------|------------|
| Engine grows past one big script tag becomes hard to navigate | Keep effects as small numbered functions; consider section-header comments at every group |
| New effects disagree with `CHAR_ASPECT` migration | All new effects use `CHAR_ASPECT` from day one |
| Resize observer fires constantly during entry-gate dismiss animation | Debounce 100 ms; ignore deltas <2 px |
| 46-effect bag picker may feel repetitive in short sessions | Already shuffled bag-with-exclude; extend to "no scene from same family back-to-back" if it feels off |
| Mandelbox / raymarch SDF may be CPU-heavy | Cap inner-loop iters on mobile; shrink steps |

## Rollout

Single PR; commits grouped by phase:

1. `refactor(fx): hoist allocations + CHAR_ASPECT constant`
2. `feat(fx): ResizeObserver for live canvas dimensions`
3. `fix(fx): wave3d/matrix/landscape/twister/starfield/tunnel/cellular/mandelbrot rendering`
4. `fix(fx): cross-fade colors during scene dissolve`
5. `feat(fx): particle family — shadebobs, bobs, fountain, snake, lightning, rain`
6. `feat(fx): math family — clifford, lorenz, fractal-tree, mandelbox, hilbert, julia`
7. `feat(fx): pattern family — reaction-diffusion, voronoi, hex-grid, highlife`
8. `feat(fx): 3D family — raymarch-sdf, dna-helix, galaxy-spiral, glitch-text`
9. `test(fx): playwright verification + screenshot capture for all 46 scenes`

## Acceptance criteria

- All 12 bugs fixed and visually verified
- 20 new effects added; each renders distinct, non-empty content
- 60 fps desktop / 30 fps mobile target met (verified via `performance.now()` deltas in dev)
- Playwright harness passes for all 46 scenes
- Screenshots saved to `.cat-audit/demoscene/` (gitignored — local verification artifact, not committed)
