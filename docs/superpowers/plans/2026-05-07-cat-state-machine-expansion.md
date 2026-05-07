# Cat State Machine Expansion + Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 12 homepage-themed cat states, 4 classes of surprises (Konami / typed words / visit milestones / click streaks), color-palette + frame variants, and fix 4 audit bugs in the existing cat state machine.

**Architecture:** All work is in `static/index.html` inside the `DesktopCat` class (around line 5935 onward). The Go server caches static files at startup, so each round of changes requires `go build -o /tmp/kdhome ./...` followed by restarting the server on port 8089 to verify in Playwright.

**Tech Stack:** Vanilla JS class, CSS, Playwright MCP for verification (no test framework — verification is by driving `window.__cat` from Playwright `evaluate` and screenshotting the result).

**Spec:** `docs/superpowers/specs/2026-05-07-cat-state-machine-expansion-design.md`

**Conventions used in this plan:**
- Source line numbers reference the file *before* this plan starts. They drift as edits land. Use the surrounding code in `old_string` snippets to locate the right place — that's the authoritative anchor.
- Each "Restart server" step is `pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &` with a 1 s wait.
- "Verify in Playwright" steps assume the server is up on `http://localhost:8089/`.

---

### Task 1: Bug fix B3 — `pickWeighted` boundary bias

**Files:**
- Modify: `static/index.html` (function `pickWeighted` inside `DesktopCat`)

- [ ] **Step 1: Apply fix**

Replace the function body:

```js
          pickWeighted(options) {
            const total = options.reduce((a, o) => a + o.w, 0);
            let r = Math.random() * total;
            for (const o of options) { r -= o.w; if (r <= 0) return o.s; }
            return options[0].s;
          }
```

with:

```js
          pickWeighted(options) {
            // r is initially in [0, total). After subtracting each weight,
            // we pick the first bucket where r became negative — using `< 0`
            // (not `<= 0`) keeps the boundary case in the right bucket. The
            // fallback returns the LAST option, not the first, in case
            // floating-point noise leaves r exactly 0 after the final subtract.
            const total = options.reduce((a, o) => a + o.w, 0);
            let r = Math.random() * total;
            for (const o of options) { r -= o.w; if (r < 0) return o.s; }
            return options[options.length - 1].s;
          }
```

- [ ] **Step 2: Restart server and verify in Playwright**

```bash
pkill -f /tmp/kdhome ; go build -o /tmp/kdhome ./... && PORT=8089 /tmp/kdhome &
sleep 1
```

In Playwright, navigate to `http://localhost:8089/` and run:

```js
() => {
  // Distribution test — 1k samples on a small set should be close to weights.
  const opts = [{s:'a',w:50},{s:'b',w:30},{s:'c',w:20}];
  const tally = { a: 0, b: 0, c: 0 };
  for (let i = 0; i < 1000; i++) tally[window.__cat.pickWeighted(opts)]++;
  return tally;
}
```

Expected: `a` ≈ 500 (±50), `b` ≈ 300 (±50), `c` ≈ 200 (±50). Confirm none is zero (no degenerate skew).

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "fix(cat): pickWeighted boundary bias and fallback

r=0 after the final subtraction now stays in the current bucket instead
of falling through to options[0]; fallback also returns last option for
FP edge cases.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Bug fix B4 — rate-limit document-click curiosity bump

**Files:**
- Modify: `static/index.html` (inside `DesktopCat.init`, the `document.addEventListener('click', ...)` block)

- [ ] **Step 1: Apply fix**

Replace:

```js
            document.addEventListener('click', () => {
              this.lastUserActivity = Date.now();
              this.mood.curiosity = Math.min(100, this.mood.curiosity + 3);
            });
```

with:

```js
            document.addEventListener('click', () => {
              const now = Date.now();
              this.lastUserActivity = now;
              // Rate-limit so scroll-bar drags / synthetic taps / bubbling
              // multi-clicks can't ramp curiosity faster than once per 250 ms.
              if (now - (this._lastCuriosityBump || 0) < 250) return;
              this._lastCuriosityBump = now;
              this.mood.curiosity = Math.min(100, this.mood.curiosity + 3);
            });
```

- [ ] **Step 2: Restart server and verify**

Restart as above. In Playwright:

```js
() => {
  const c = window.__cat;
  c.mood.curiosity = 0;
  c._lastCuriosityBump = 0;
  // Simulate 10 rapid clicks within ~50ms total
  for (let i = 0; i < 10; i++) document.dispatchEvent(new Event('click'));
  return { curiosity: c.mood.curiosity };
}
```

Expected: `curiosity` is exactly 3 (one bump only, not 30). Without the fix it'd be 30.

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "fix(cat): rate-limit document-click curiosity bump

Prevent synthetic scroll-bar drags / inertial taps from ramping
curiosity 10x in a frame. 250 ms gate.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Bug fix B2 — paw prints only during locomotion

**Files:**
- Modify: `static/index.html` (function `leavePawPrints`)

- [ ] **Step 1: Apply fix**

Replace:

```js
          leavePawPrints() {
            if (Math.random() > 0.3) return;
            const catRect = this.cat.getBoundingClientRect();
            const paw = document.createElement('div');
            paw.className = 'paw-print';
            paw.style.left = (catRect.left + catRect.width / 2) + 'px';
            // Drop the print just under the cat's feet, wherever it currently
            // is — the old hardcoded bottom:45px stranded prints near the
            // taskbar even when the cat had wandered upward.
            paw.style.bottom = (window.innerHeight - catRect.bottom + 2) + 'px';
            document.body.appendChild(paw);
            setTimeout(() => paw.remove(), 2000);
          }
```

with:

```js
          leavePawPrints() {
            // transitionstart fires on every style mutation, including the
            // empty-transform cleanup in setState. Only emit while the cat
            // is actually moving so prints aren't sprayed at every state flip.
            const moving = this.currentState === 'walking' || this.currentState === 'running';
            if (!moving) return;
            if (Math.random() > 0.3) return;
            const catRect = this.cat.getBoundingClientRect();
            const paw = document.createElement('div');
            paw.className = 'paw-print';
            paw.style.left = (catRect.left + catRect.width / 2) + 'px';
            // Drop the print just under the cat's feet, wherever it currently
            // is — the old hardcoded bottom:45px stranded prints near the
            // taskbar even when the cat had wandered upward.
            paw.style.bottom = (window.innerHeight - catRect.bottom + 2) + 'px';
            document.body.appendChild(paw);
            setTimeout(() => paw.remove(), 2000);
          }
```

- [ ] **Step 2: Restart server and verify**

In Playwright:

```js
() => {
  const c = window.__cat;
  c.cancelBehaviorLoop && c.cancelBehaviorLoop();
  c.isMoving = false;
  c.cat.style.transition = 'none';
  c.cat.style.right = '30px';
  c.cat.style.bottom = '50px';
  // Force 20 setStates (which trigger transitionstart) — should produce 0 paw prints
  // because none of these states are walking/running.
  for (const s of ['idle','sitting','thinking','coffee','playing','dancing','sitting','idle','coding','idle']) {
    c.setState(s);
  }
  return { paws: document.querySelectorAll('.paw-print').length };
}
```

Expected: `paws: 0`. Pre-fix you'd see ~3 (30% × 10).

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "fix(cat): only spawn paw prints during locomotion

leavePawPrints was firing on every transitionstart, including empty
transform cleanups in setState. Gate on currentState in {walking, running}.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Bug fix B1 — `followPoint` token + `cancelFollow` helper

**Files:**
- Modify: `static/index.html` (`followPoint` and the inline `if (this.followTimeout) ... clearTimeout` blocks)

- [ ] **Step 1: Add `cancelFollow` helper next to `cancelBehaviorLoop`**

Find:

```js
          cancelBehaviorLoop() {
            if (this.behaviorTimer) { clearTimeout(this.behaviorTimer); this.behaviorTimer = null; }
          }
```

Insert this method directly after it:

```js
          // Invalidates any in-flight followPoint chain. Bumping the token
          // tells the queued inner setTimeouts to bail when they wake up —
          // they'd otherwise overwrite state that's been set by an
          // interrupting action (pet, spook, escape).
          cancelFollow() {
            this._followToken = (this._followToken || 0) + 1;
            if (this.followTimeout) { clearTimeout(this.followTimeout); this.followTimeout = null; }
          }
```

- [ ] **Step 2: Replace `followPoint` body**

Find:

```js
          followPoint(x, y) {
            if (this.isMoving) return;
            this.isMoving = true;
            this.setState('alert');
            this.animateFrames(500, 500);
            if (this.followTimeout) clearTimeout(this.followTimeout);
            this.followTimeout = setTimeout(() => {
              if (!this.isMoving) return; // interrupted
              this.setState('walking');
              const catRect = this.cat.getBoundingClientRect();
              this.direction = (x - catRect.left) > 0 ? -1 : 1;
              this.updateDirection();
              const targetX = window.innerWidth - x - 30;
              const targetY = window.innerHeight - y - 30;
              const clampedR = Math.max(30, Math.min(window.innerWidth - 100, targetX));
              const clampedB = Math.max(50, Math.min(300, targetY));
              const safe = this.avoidMusicWindow(clampedR, clampedB);
              const dur = 2000;
              this.cat.style.transition = `all ${dur}ms ease-out`;
              this.cat.style.right = safe.right + 'px';
              this.cat.style.bottom = safe.bottom + 'px';
              this.animateFrames(200, dur);
              setTimeout(() => {
                this.isMoving = false;
                this.cat.style.transition = '';
                this.setState('sitting');
                this.showMessage('Caught it!');
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => { this.hideMessage(); this.restartBehaviorLoop(); }, 2000);
              }, dur);
            }, 500);
          }
```

Replace with:

```js
          followPoint(x, y) {
            if (this.isMoving) return;
            this.isMoving = true;
            // Token guards both inner setTimeouts: if cancelFollow runs (or
            // followPoint is restarted), bumping the token makes any pending
            // inner timeout return without overwriting state.
            this._followToken = (this._followToken || 0) + 1;
            const myToken = this._followToken;
            this.setState('alert');
            this.animateFrames(500, 500);
            if (this.followTimeout) clearTimeout(this.followTimeout);
            this.followTimeout = setTimeout(() => {
              if (myToken !== this._followToken) return; // cancelled
              if (!this.isMoving) return; // interrupted
              this.setState('walking');
              const catRect = this.cat.getBoundingClientRect();
              this.direction = (x - catRect.left) > 0 ? -1 : 1;
              this.updateDirection();
              const targetX = window.innerWidth - x - 30;
              const targetY = window.innerHeight - y - 30;
              const clampedR = Math.max(30, Math.min(window.innerWidth - 100, targetX));
              const clampedB = Math.max(50, Math.min(300, targetY));
              const safe = this.avoidMusicWindow(clampedR, clampedB);
              const dur = 2000;
              this.cat.style.transition = `all ${dur}ms ease-out`;
              this.cat.style.right = safe.right + 'px';
              this.cat.style.bottom = safe.bottom + 'px';
              this.animateFrames(200, dur);
              setTimeout(() => {
                if (myToken !== this._followToken) return; // cancelled
                this.isMoving = false;
                this.cat.style.transition = '';
                this.setState('sitting');
                this.showMessage('Caught it!');
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => { this.hideMessage(); this.restartBehaviorLoop(); }, 2000);
              }, dur);
            }, 500);
          }
```

- [ ] **Step 3: Replace ALL five inline `followTimeout` clears with `cancelFollow()` calls**

There are exactly 5 occurrences of the line

```
if (this.followTimeout) { clearTimeout(this.followTimeout); this.followTimeout = null; }
```

in the file, located in:
1. Music-watcher `recheck` — max-window snap-down branch (~line 6677)
2. Music-watcher `recheck` — music-overlap escape branch (~line 6687)
3. `pet()` (~line 6890)
4. `interact()` (~line 6907)
5. `spook()` (~line 6947)

For each occurrence, replace the line with `this.cancelFollow();` (preserving the same indentation as the surrounding code).

Confirm with:

```bash
grep -c "if (this.followTimeout) { clearTimeout(this.followTimeout); this.followTimeout = null; }" static/index.html
```

Expected: `0`. (After this step, the only remaining `followTimeout` clear is the one *inside* `followPoint` itself, which uses a different shape — `if (this.followTimeout) clearTimeout(this.followTimeout);` without the assignment to null — and is intentionally left as a dedupe within `followPoint` for fresh-start cases.)

Verify the replacements landed:

```bash
grep -c "this.cancelFollow();" static/index.html
```

Expected: `5` (or more, if Tasks 11-13 have already added trigger methods that call it; in that case run this check before this task and after).

- [ ] **Step 4: Restart server and verify**

In Playwright:

```js
() => {
  const c = window.__cat;
  c.cancelBehaviorLoop();
  c.isMoving = false;
  c.cat.style.transition = 'none';
  c.cat.style.right = '30px';
  c.cat.style.bottom = '50px';
  // Start a follow toward (200, 200)
  c.followPoint(200, 200);
  const tokenAtStart = c._followToken;
  // Immediately interrupt with a pet (which calls cancelFollow internally)
  c.pet();
  // Wait long enough for both inner timeouts to have fired (2.5 s + 0.5 s safety)
  return new Promise(res => setTimeout(() => {
    res({
      tokenAtStart,
      tokenNow: c._followToken,
      currentState: c.currentState,
      bubbleText: c.bubble.textContent,
    });
  }, 3000));
}
```

Expected: `tokenNow > tokenAtStart` (cancelled). `currentState` is whatever `pet()`'s timer ended at (likely `idle`), NOT `sitting`. `bubbleText` is NOT "Caught it!". Pre-fix you'd see `currentState === 'sitting'` and `bubbleText === 'Caught it!'`.

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "fix(cat): followPoint token guards inner timeouts

The two inner setTimeouts in followPoint would happily run to completion
even after the chain was cancelled by pet/spook/escape, clobbering state
the interrupting action set. Added a per-call token + cancelFollow helper
that bumps the token; inner timeouts bail if their token is stale.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Add 12 new state frames + message pools

**Files:**
- Modify: `static/index.html` (the `this.frames = { ... }` and `this.msgPools = { ... }` blocks inside the constructor)

- [ ] **Step 1: Append new entries to `this.frames`**

Find the end of the existing `this.frames = { ... };` declaration (the line with `defrag: ...`) and add these entries before the closing `};`:

```js
              reading:     [' /\\_/\\  \n( o.o )=\n > ^ <  ', ' /\\_/\\  \n( -.- )=\n > ^ <  '],
              researching: [' /\\_/\\  \n( ?.? ) \n > ^ <  ', ' /\\_/\\  \n( o.o )?\n > ^ <  '],
              listening:   [' /\\_/\\  \n(-_-) Π\n > ^ <  ', ' /\\_/\\  \n(o_o) Π\n > ^ <  '],
              vibing:      [' /\\_/\\  \n( ^.^ )♪\n > ^ <  ', ' /\\_/\\  \n( ^.^ )♫\n > ^ <  '],
              watching:    [' /\\_/\\  \n( O.O )*\n > ^ <  ', ' /\\_/\\  \n( o.o )*\n > ^ <  '],
              soldering:   [' /\\_/\\  \n( o.o )+\n > ^ <  ', ' /\\_/\\  \n( -.o )+\n > ^ <  '],
              'disc-spin': [' /\\_/\\  \n( o.o )O\n > ^ <  ', ' /\\_/\\  \n( o.o )|\n > ^ <  '],
              'low-power': [' /\\_/\\  \n( -.- )░\n > ^ <  ', ' /\\_/\\  \n( -.- )▒\n > ^ <  '],
              demoscene:   [' /\\_/\\  \n(♥.♥) \n > ^ <  ', ' /\\_/\\  \n(★.★) \n > ^ <  '],
              stargaze:    [' /\\_/\\  \n( ^.^ ).\n > ^ <  ', ' /\\_/\\  \n( o.o )*\n > ^ <  '],
              lunch:       [' /\\_/\\  \n(>.<) ><\n > ^ <  ', ' /\\_/\\  \n( ^.^ )<\n > ^ <  '],
              celebrate:   [' /\\_/\\  \n( ^o^ )b\n > ^ <  ', ' /\\_/\\  \n( ^O^ )b\n > ^ <  '],
```

- [ ] **Step 2: Append new entries to `this.msgPools`**

Find the end of `this.msgPools = { ... };` (the `tips: [...],` array) and add these entries directly after `tips: [...]`, before the closing `};`:

```js
              reading:     ['Page 4,821 of 23,000…', '[citation needed]', '*flips page*', 'Cite: Knuth, 1968', 'TIL…'],
              researching: ['*scans index*', 'depth=8 levels', 'cross-referencing…', 'fact-checking', 'three primary sources'],
              listening:   ['Chapter 12: A Tale of Two Servers', '*listening*', 'narrator @ 1.25x', 'audiobook.mp3', 'side note: footnotes!'],
              vibing:      ['Untz untz', '.XM module loaded', '*nods to beat*', 'Tracker: 4ch', 'sid chip dreams', 'four on the floor'],
              watching:    ['*munch munch*', 'Skipping ads…', 'Buffer: 99%', '1080p preserved', '*staring at pixels*'],
              soldering:   ['Pin 3 reflowed', 'Smoke = working', 'Why warm?', '+5V rail OK', '*solder hiss*', 'flux is friend'],
              'disc-spin': ['*chk-chk-chk*', 'Insert disc 2', 'Reading TOC…', 'ISO9660', 'sectors: 333,000', '*disc whirr*'],
              'low-power': ['30 W is plenty', '*solar charging*', 'throttled', 'kWh efficient', '*low-power mode*', 'eco purr'],
              demoscene:   ['Greetz to the scene', '/* 64 bytes left */', '4 KB demo', 'scrolltext goes here →', 'sync.ms = 0', '*plasma effect*'],
              stargaze:    ['*contemplates 12 TB*', 'cosmic rays…', 'Carl Sagan would approve', '12 TB is just numbers', '✨'],
              lunch:       ['*nom nom*', '1 PM tuna time', '404: Salmon found', '*purr while eating*', '*munch*'],
              celebrate:   ['Mrow!', '*party*', 'Achievement unlocked', 'wow such count', '*confetti*'],
```

- [ ] **Step 3: Restart server and verify**

In Playwright:

```js
() => {
  const c = window.__cat;
  const want = ['reading','researching','listening','vibing','watching','soldering','disc-spin','low-power','demoscene','stargaze','lunch','celebrate'];
  const missingFrames = want.filter(s => !c.frames[s]);
  const missingMsgs = want.filter(s => !c.msgPools[s]);
  return { missingFrames, missingMsgs };
}
```

Expected: both arrays empty.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): add frames + message pools for 12 new themed states

reading, researching, listening, vibing, watching, soldering, disc-spin,
low-power, demoscene, stargaze, lunch, celebrate. ASCII-art only; no
state-machine wiring yet (next commit).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Add transitions + state durations for new states

**Files:**
- Modify: `static/index.html` (the `this.transitions = { ... }` and `this.stateDurations = { ... }` blocks)

- [ ] **Step 1: Add inbound edges to existing states' transitions**

Replace the existing `this.transitions = { ... };` block. Find:

```js
            this.transitions = {
              idle:        [{s:'coding',w:15},{s:'walking',w:12},{s:'wander',w:10},{s:'sitting',w:10},{s:'thinking',w:10},{s:'archiving',w:12},{s:'surfing',w:8},{s:'idle_tip',w:8}],
              sitting:     [{s:'idle',w:20},{s:'sleeping',w:15},{s:'thinking',w:15},{s:'coding',w:10},{s:'cleaning',w:10}],
              thinking:    [{s:'coding',w:25},{s:'archiving',w:20},{s:'idle',w:15},{s:'coffee',w:10}],
              coding:      [{s:'debugging',w:30},{s:'idle',w:20},{s:'coffee',w:15},{s:'coding',w:10}],
              debugging:   [{s:'annoyed',w:30},{s:'coding',w:25},{s:'idle',w:15},{s:'coffee',w:10}],
              annoyed:     [{s:'coffee',w:40},{s:'idle',w:20},{s:'glitch',w:10}],
              coffee:      [{s:'coding',w:35},{s:'idle',w:25},{s:'archiving',w:15}],
              archiving:   [{s:'downloading',w:35},{s:'idle',w:20},{s:'defrag',w:15},{s:'surfing',w:10}],
              downloading: [{s:'idle',w:30},{s:'archiving',w:20},{s:'annoyed',w:15}],
              defrag:      [{s:'idle',w:40},{s:'archiving',w:20}],
              surfing:     [{s:'idle',w:30},{s:'archiving',w:25},{s:'hacking',w:10}],
              hacking:     [{s:'idle',w:30},{s:'matrix',w:20},{s:'glitch',w:10}],
              matrix:      [{s:'idle',w:50},{s:'hacking',w:15}],
              sleeping:    [{s:'waking',w:100}],
              waking:      [{s:'idle',w:50},{s:'coffee',w:30}],
              walking:     [{s:'idle',w:40},{s:'sitting',w:20},{s:'wander',w:10}],
              wander:      [{s:'idle',w:40},{s:'sitting',w:20},{s:'walking',w:10}],
              cleaning:    [{s:'idle',w:40},{s:'sitting',w:20}],
              playing:     [{s:'idle',w:40},{s:'dancing',w:15}],
              dancing:     [{s:'idle',w:50},{s:'playing',w:15}],
              gamer:       [{s:'idle',w:30},{s:'annoyed',w:20}],
              glitch:      [{s:'bsod',w:20},{s:'idle',w:50}],
              bsod:        [{s:'idle',w:100}],
              idle_tip:    [{s:'idle',w:30},{s:'coding',w:15},{s:'archiving',w:15},{s:'walking',w:10}],
            };
```

with:

```js
            this.transitions = {
              idle:        [{s:'coding',w:15},{s:'walking',w:12},{s:'wander',w:10},{s:'sitting',w:10},{s:'thinking',w:10},{s:'archiving',w:12},{s:'surfing',w:8},{s:'idle_tip',w:8},{s:'reading',w:10},{s:'vibing',w:8},{s:'watching',w:8},{s:'listening',w:8},{s:'stargaze',w:8},{s:'low-power',w:6},{s:'lunch',w:1}],
              sitting:     [{s:'idle',w:20},{s:'sleeping',w:15},{s:'thinking',w:15},{s:'coding',w:10},{s:'cleaning',w:10},{s:'stargaze',w:10}],
              thinking:    [{s:'coding',w:25},{s:'archiving',w:20},{s:'idle',w:15},{s:'coffee',w:10},{s:'researching',w:15}],
              coding:      [{s:'debugging',w:30},{s:'idle',w:20},{s:'coffee',w:15},{s:'coding',w:10},{s:'soldering',w:8}],
              debugging:   [{s:'annoyed',w:30},{s:'coding',w:25},{s:'idle',w:15},{s:'coffee',w:10},{s:'soldering',w:10}],
              annoyed:     [{s:'coffee',w:40},{s:'idle',w:20},{s:'glitch',w:10}],
              coffee:      [{s:'coding',w:35},{s:'idle',w:25},{s:'archiving',w:15}],
              archiving:   [{s:'downloading',w:35},{s:'idle',w:20},{s:'defrag',w:15},{s:'surfing',w:10},{s:'reading',w:10},{s:'disc-spin',w:10}],
              downloading: [{s:'idle',w:30},{s:'archiving',w:20},{s:'annoyed',w:15},{s:'disc-spin',w:8}],
              defrag:      [{s:'idle',w:40},{s:'archiving',w:20}],
              surfing:     [{s:'idle',w:30},{s:'archiving',w:25},{s:'hacking',w:10},{s:'reading',w:10},{s:'researching',w:8},{s:'watching',w:6}],
              hacking:     [{s:'idle',w:30},{s:'matrix',w:20},{s:'glitch',w:10},{s:'demoscene',w:10}],
              matrix:      [{s:'idle',w:50},{s:'hacking',w:15},{s:'demoscene',w:10}],
              sleeping:    [{s:'waking',w:100},{s:'low-power',w:5}],
              waking:      [{s:'idle',w:50},{s:'coffee',w:30}],
              walking:     [{s:'idle',w:40},{s:'sitting',w:20},{s:'wander',w:10}],
              wander:      [{s:'idle',w:40},{s:'sitting',w:20},{s:'walking',w:10}],
              cleaning:    [{s:'idle',w:40},{s:'sitting',w:20}],
              playing:     [{s:'idle',w:40},{s:'dancing',w:15},{s:'vibing',w:8}],
              dancing:     [{s:'idle',w:50},{s:'playing',w:15},{s:'vibing',w:12}],
              gamer:       [{s:'idle',w:30},{s:'annoyed',w:20}],
              glitch:      [{s:'bsod',w:20},{s:'idle',w:50}],
              bsod:        [{s:'idle',w:100}],
              idle_tip:    [{s:'idle',w:30},{s:'coding',w:15},{s:'archiving',w:15},{s:'walking',w:10}],
              // ─── New themed states (outbound transitions) ───
              reading:     [{s:'idle',w:30},{s:'thinking',w:15},{s:'researching',w:10},{s:'archiving',w:8}],
              researching: [{s:'idle',w:30},{s:'thinking',w:20},{s:'reading',w:10}],
              listening:   [{s:'idle',w:30},{s:'sleeping',w:10},{s:'vibing',w:10}],
              vibing:      [{s:'idle',w:30},{s:'dancing',w:25},{s:'playing',w:15}],
              watching:    [{s:'idle',w:30},{s:'thinking',w:10},{s:'surfing',w:15}],
              soldering:   [{s:'idle',w:30},{s:'coding',w:20},{s:'coffee',w:10},{s:'debugging',w:15}],
              'disc-spin': [{s:'idle',w:30},{s:'archiving',w:25},{s:'downloading',w:15}],
              'low-power': [{s:'sleeping',w:30},{s:'idle',w:25}],
              demoscene:   [{s:'idle',w:30},{s:'hacking',w:15},{s:'matrix',w:15},{s:'coding',w:10}],
              stargaze:    [{s:'idle',w:40},{s:'sitting',w:20},{s:'thinking',w:10}],
              lunch:       [{s:'idle',w:40},{s:'sitting',w:20},{s:'sleeping',w:10}],
              celebrate:   [{s:'idle',w:50},{s:'dancing',w:20}],
            };
```

- [ ] **Step 2: Add durations**

Find the existing `this.stateDurations = { ... };` block. Find this line:

```js
              glitch: 2000, bsod: 3000, idle_tip: 5000, alert: 1500,
```

Replace it with:

```js
              glitch: 2000, bsod: 3000, idle_tip: 5000, alert: 1500,
              reading: 5000, researching: 4000, listening: 4500, vibing: 4000,
              watching: 4000, soldering: 4500, 'disc-spin': 4000, 'low-power': 5000,
              demoscene: 4000, stargaze: 4000, lunch: 3500, celebrate: 5000,
```

- [ ] **Step 3: Restart server and verify (no syntax errors, transitions reachable)**

```js
() => {
  const c = window.__cat;
  // All new states must be both keys in transitions AND keys in stateDurations.
  const newStates = ['reading','researching','listening','vibing','watching','soldering','disc-spin','low-power','demoscene','stargaze','lunch','celebrate'];
  const missingTr = newStates.filter(s => !c.transitions[s]);
  const missingDur = newStates.filter(s => !c.stateDurations[s]);
  // And every transition target must be a known state.
  const allStates = new Set([...Object.keys(c.transitions), 'walking', 'wander', 'idle_tip']);
  const dangling = [];
  for (const [from, edges] of Object.entries(c.transitions)) {
    for (const e of edges) if (!allStates.has(e.s)) dangling.push(`${from} -> ${e.s}`);
  }
  return { missingTr, missingDur, dangling };
}
```

Expected: all three arrays empty.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): wire 12 new states into transitions + durations

Inbound edges from contextually-related parents (e.g. archiving →
disc-spin, surfing → reading); outbound edges back to idle/family
states. Durations match the spec.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Add `executeBehavior` cases for new states

**Files:**
- Modify: `static/index.html` (`executeBehavior(state)` switch statement)

- [ ] **Step 1: Add the 12 new cases before the `default:` case**

Find the existing `executeBehavior(state)` switch. Locate:

```js
              case 'bsod':
                this.setState('bsod');
                this.showMessage(this.pickMsg('bsod'));
                this.animateFrames(3000, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => {
                  this.hideMessage();
                  this.showMessage('Rebooting...');
                  setTimeout(() => this.hideMessage(), 1500);
                }, dur - 1500);
                this.mood.energy = Math.min(100, this.mood.energy + 10);
                break;

              default:
```

Insert these cases between the `bsod` `break;` and `default:` (i.e., right after `this.mood.energy = Math.min(100, this.mood.energy + 10); break;` and before `default:`):

```js
              case 'reading':
                this.setState('reading');
                this.showMessage(this.pickMsg('reading'));
                this.animateFrames(900, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.curiosity += 5;
                break;

              case 'researching':
                this.setState('researching');
                this.showMessage(this.pickMsg('researching'));
                this.animateFrames(700, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.curiosity += 8;
                break;

              case 'listening':
                this.setState('listening');
                this.showMessage(this.pickMsg('listening'));
                this.animateFrames(1200, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                break;

              case 'vibing':
                this.setState('vibing');
                this.showMessage(this.pickMsg('vibing'));
                this.animateFrames(300, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.boredom = Math.max(0, this.mood.boredom - 8);
                break;

              case 'watching':
                this.setState('watching');
                this.showMessage(this.pickMsg('watching'));
                this.animateFrames(800, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.boredom = Math.max(0, this.mood.boredom - 5);
                break;

              case 'soldering':
                this.setState('soldering');
                this.showMessage(this.pickMsg('soldering'));
                this.animateFrames(400, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                break;

              case 'disc-spin':
                this.setState('disc-spin');
                this.showMessage(this.pickMsg('disc-spin'));
                this.animateFrames(150, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                break;

              case 'low-power':
                this.setState('low-power');
                this.showMessage(this.pickMsg('low-power'));
                this.animateFrames(2500, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.energy = Math.min(100, this.mood.energy + 5);
                break;

              case 'demoscene':
                this.setState('demoscene');
                this.showMessage(this.pickMsg('demoscene'));
                this.animateFrames(200, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.curiosity += 5;
                break;

              case 'stargaze':
                this.setState('stargaze');
                this.showMessage(this.pickMsg('stargaze'));
                this.animateFrames(2000, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                break;

              case 'lunch':
                this.setState('lunch');
                this.showMessage(this.pickMsg('lunch'));
                this.animateFrames(400, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                this.mood.energy = Math.min(100, this.mood.energy + 10);
                break;

              case 'celebrate':
                this.setState('celebrate');
                this.showMessage(this.pickMsg('celebrate'));
                this.animateFrames(300, dur);
                this.clearStateTimer();
                this.stateTimer = setTimeout(() => this.hideMessage(), dur - 500);
                for (let i = 0; i < 8; i++) {
                  setTimeout(() => this.spawnEmote(['★','✨','♦','♥'][Math.floor(Math.random()*4)], '#ffd166', 1200), i * 100);
                }
                this.mood.boredom = 0;
                break;

```

- [ ] **Step 2: Restart server and verify each state runs**

```js
async () => {
  const c = window.__cat;
  c.cancelBehaviorLoop();
  c.cancelFollow();
  const want = ['reading','researching','listening','vibing','watching','soldering','disc-spin','low-power','demoscene','stargaze','lunch','celebrate'];
  const results = {};
  for (const s of want) {
    c.executeBehavior(s);
    await new Promise(r => setTimeout(r, 80));
    results[s] = {
      state: c.currentState,
      bodyText: c.body.textContent,
      bubbleShown: c.bubble.classList.contains('show'),
    };
  }
  return results;
}
```

Expected: every entry has `state` matching the requested name, `bodyText` non-empty, `bubbleShown: true`.

- [ ] **Step 3: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): executeBehavior cases for 12 new themed states

reading, researching, listening, vibing, watching, soldering, disc-spin,
low-power, demoscene, stargaze, lunch, celebrate. celebrate spawns 8
emotes; mood adjustments mirror the existing patterns (e.g. low-power
restores a little energy, vibing burns boredom, lunch refills energy).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Themed weight biases in `getNextState`

**Files:**
- Modify: `static/index.html` (`getNextState()` and add a `windowVisible(id)` helper)

- [ ] **Step 1: Add a `windowVisible` helper**

Add this method to the class right above `getAllAvoidRects()` (it's a thin generic helper used by `getAvoidRect`-family + the new bias logic):

```js
          // Cheap visibility check — true if the element is in the DOM and
          // not currently hidden / minimized. Used by the themed weight
          // biases in getNextState; getBoundingClientRect would be overkill.
          windowVisible(id) {
            const el = document.getElementById(id);
            if (!el) return false;
            if (el.classList.contains('hidden')) return false;
            if (el.style.display === 'none') return false;
            return true;
          }
```

- [ ] **Step 2: Extend `getNextState`**

Replace the existing `getNextState` body. Find:

```js
          getNextState() {
            if (this.chainQueue.length > 0) return this.chainQueue.shift();

            const trans = this.transitions[this.currentState];
            if (!trans) return 'idle';

            const modified = trans.map(t => {
              let w = t.w;
              if (t.s === 'sleeping' || t.s === 'coffee') w += this.mood.energy < 25 ? 30 : 0;
              if (t.s === 'coding' || t.s === 'archiving' || t.s === 'hacking') w += this.mood.curiosity > 40 ? 10 : 0;
              if (t.s === 'walking' || t.s === 'playing' || t.s === 'dancing') w += this.mood.boredom > 40 ? 15 : 0;
              const h = new Date().getHours();
              if (h >= 22 || h < 6) { if (t.s === 'sleeping') w += 25; }
              if (h >= 6 && h < 10) { if (t.s === 'coffee') w += 15; }
              if (this.mood.energy < 20 && ['dancing','playing','gamer','running'].includes(t.s)) w = Math.max(1, w - 15);
              return { s: t.s, w: Math.max(1, w) };
            });

            return this.pickWeighted(modified);
          }
```

with:

```js
          getNextState() {
            if (this.chainQueue.length > 0) return this.chainQueue.shift();

            const trans = this.transitions[this.currentState];
            if (!trans) return 'idle';

            const now = new Date();
            const h = now.getHours();
            const day = now.getDay();
            const browserVisible = this.windowVisible('browser-window');
            const demoVisible = this.windowVisible('demo-window');
            const musicVisible = this.windowVisible('music-window');
            const lunchHour = h >= 12 && h < 13;
            const fridayEvening = day === 5 && h >= 17;

            const modified = trans.map(t => {
              let w = t.w;
              if (t.s === 'sleeping' || t.s === 'coffee') w += this.mood.energy < 25 ? 30 : 0;
              if (t.s === 'coding' || t.s === 'archiving' || t.s === 'hacking') w += this.mood.curiosity > 40 ? 10 : 0;
              if (t.s === 'walking' || t.s === 'playing' || t.s === 'dancing') w += this.mood.boredom > 40 ? 15 : 0;
              if (h >= 22 || h < 6) { if (t.s === 'sleeping') w += 25; }
              if (h >= 6 && h < 10) { if (t.s === 'coffee') w += 15; }
              if (this.mood.energy < 20 && ['dancing','playing','gamer','running'].includes(t.s)) w = Math.max(1, w - 15);

              // ─── Themed biases (homepage context) ───
              if (musicVisible && t.s === 'vibing') w += 15;
              if (browserVisible && (t.s === 'reading' || t.s === 'watching' || t.s === 'researching' || t.s === 'listening')) w += 8;
              if (demoVisible && t.s === 'demoscene') w += 15;
              if (lunchHour && t.s === 'lunch') w += 40;
              if (fridayEvening && (t.s === 'vibing' || t.s === 'dancing')) w += 12;
              if (this.mood.energy < 30 && t.s === 'low-power') w += 20;

              return { s: t.s, w: Math.max(1, w) };
            });

            return this.pickWeighted(modified);
          }
```

- [ ] **Step 3: Restart server and verify**

```js
() => {
  const c = window.__cat;
  // Force currentState='idle' for the test, then sample 1k transitions with browser-window forced visible.
  c.currentState = 'idle';
  // Stub windowVisible so we can assert on context bumps regardless of UI state.
  const origVisible = c.windowVisible.bind(c);
  c.windowVisible = (id) => id === 'browser-window';
  const tally = {};
  for (let i = 0; i < 2000; i++) {
    const s = c.getNextState();
    tally[s] = (tally[s] || 0) + 1;
  }
  c.windowVisible = origVisible;
  // With browser-window visible, reading/watching/researching/listening should be over-represented vs base weight 10/8/8/8.
  return { tally };
}
```

Expected: `reading` and `watching` and `researching` and `listening` each in the range 100-300+ (markedly elevated). Without bias they'd cluster much lower because their base weights are 8-10.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): themed weight biases in getNextState

Music window open → vibing weighted up. Browser window (any archive)
→ reading/watching/researching/listening up. Demo window →
demoscene up. Lunch hour → lunch heavily up. Friday evening →
vibing/dancing up. Low energy → low-power state available.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Color palette variants

**Files:**
- Modify: `static/index.html` (constructor, `setState`)

- [ ] **Step 1: Define palettes + pick one in the constructor**

In the constructor, find this block (just after `this.stateStyles = { ... };`):

```js
            // ─── State Durations ───
            this.stateDurations = {
```

Immediately *before* `// ─── State Durations ───`, add:

```js
            // ─── Color palettes (variant) ───
            // Picked once per page load. Default neon (empty color string) is
            // the most common; the others are little surprises that show up
            // for the rest of a session. Per-state stateStyles still override.
            this.palettes = [
              { name: 'neon',      weight: 80, color: '',          glow: '0 0 10px rgba(0, 255, 150, 0.6)' },
              { name: 'orange',    weight:  7, color: '#ffaa44',   glow: '0 0 10px rgba(255, 170, 68, 0.7)' },
              { name: 'tuxedo',    weight:  5, color: '#e8eef0',   glow: '0 0 10px rgba(180, 200, 220, 0.6)' },
              { name: 'pink',      weight:  4, color: '#ff66cc',   glow: '0 0 10px rgba(255, 102, 204, 0.7)' },
              { name: 'solar',     weight:  2, color: '#f4d35e',   glow: '0 0 10px rgba(244, 211, 94, 0.7)' },
              { name: 'darkgreen', weight:  2, color: '#33ff77',   glow: '0 0 8px rgba(51, 255, 119, 0.6), 0 0 16px rgba(0, 80, 0, 0.5)' },
            ];
            this.palette = this.pickPalette();

```

- [ ] **Step 2: Add `pickPalette` and `applyPalette` methods**

Add these two methods to the class (good location: directly after `pickWeighted`):

```js
          pickPalette() {
            const total = this.palettes.reduce((a, p) => a + p.weight, 0);
            let r = Math.random() * total;
            for (const p of this.palettes) { r -= p.weight; if (r < 0) return p; }
            return this.palettes[0];
          }

          applyPalette() {
            // Default 'neon' palette has color === '' which means "use whatever
            // CSS specified". Non-default palettes set color/textShadow on the
            // body and tweak the drop-shadow filter on the wrapper.
            if (!this.palette || !this.palette.color) {
              this.cat.style.filter = '';
              return;
            }
            this.cat.style.filter = 'drop-shadow(' + this.palette.glow + ')';
          }
```

- [ ] **Step 3: Apply the palette at end of `init()`**

In `init()`, find:

```js
            this.cat.addEventListener('transitionstart', () => this.leavePawPrints());
          }
```

Replace with:

```js
            this.cat.addEventListener('transitionstart', () => this.leavePawPrints());
            this.applyPalette();
          }
```

- [ ] **Step 4: Make `setState` honor palette as default color**

In `setState`, find:

```js
            const style = this.stateStyles[state];
            this.body.style.color = style ? style.color : '';
            this.body.style.textShadow = style ? style.shadow : '';
```

Replace with:

```js
            const style = this.stateStyles[state];
            const paletteColor = (this.palette && this.palette.color) || '';
            const paletteShadow = paletteColor ? '0 0 10px ' + paletteColor : '';
            this.body.style.color = style ? style.color : paletteColor;
            this.body.style.textShadow = style ? style.shadow : paletteShadow;
```

- [ ] **Step 5: Restart server and verify distribution**

```js
() => {
  // Sample 2000 palette picks and tally — should land within 25% of weights.
  const c = window.__cat;
  const tally = {};
  for (let i = 0; i < 2000; i++) {
    const p = c.pickPalette();
    tally[p.name] = (tally[p.name] || 0) + 1;
  }
  return { tally, currentPalette: c.palette.name, filter: c.cat.style.filter };
}
```

Expected: `tally.neon` ≈ 1600 (±200), `tally.orange` ≈ 140, others nonzero. `currentPalette` is whichever was picked at construction; `filter` reflects it (or empty for neon).

- [ ] **Step 6: Force a non-default palette and screenshot for visual confirmation**

```js
() => {
  const c = window.__cat;
  c.palette = c.palettes.find(p => p.name === 'pink');
  c.applyPalette();
  // Re-trigger setState so colors flow.
  c.setState('idle');
  return { palette: c.palette.name };
}
```

Then take a screenshot and confirm the cat glows pink.

- [ ] **Step 7: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): random color palette per session

Six palettes weighted neon=80 / orange=7 / tuxedo=5 / pink=4 / solar=2 /
darkgreen=2. Palette is picked once at construction and applied through
setState's color/shadow defaults; per-state stateStyles still override
(matrix stays green, glitch stays magenta).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Frame variants (occasional alt designs)

**Files:**
- Modify: `static/index.html` (constructor, `animate`)

- [ ] **Step 1: Define frame variants in the constructor**

Right after the new palette block from Task 9 (still before `// ─── State Durations ───`), add:

```js
            // ─── Frame variants (visual surprise) ───
            // 15% of the time, animate() picks from this pool instead of the
            // base frame. Pure cosmetic; doesn't affect transitions or moods.
            this.frameVariants = {
              idle:    [' _,/\\_/\\,_\n( o.o ) \n > ^ <  '],
              sitting: [' /\\_/\\  \n>>=^.^=<<\n(> ^ <)  '],
              coding:  [' /\\_/\\  \n( =.= )_\n > ^ <[]'],
              hacking: [' /\\_/\\  \n( =.= ) \n > ^ <  '],
              reading: [' /\\_/\\  \n( o.o )▤\n > ^ <  '],
              vibing:  [' /\\_/\\  \n( ^.^ )♫\n > ^ <♪ '],
            };

```

- [ ] **Step 2: Update `animate()` to consult variants**

Find:

```js
          animate() {
            const key = this.currentState === 'idle_tip' ? 'idle' : this.currentState;
            const f = this.frames[key];
            if (!f) return;
            this.body.textContent = f[this.frameIndex % f.length];
          }
```

Replace with:

```js
          animate() {
            const key = this.currentState === 'idle_tip' ? 'idle' : this.currentState;
            const f = this.frames[key];
            if (!f) return;
            let frame = f[this.frameIndex % f.length];
            const variants = this.frameVariants && this.frameVariants[key];
            if (variants && variants.length && Math.random() < 0.15) {
              frame = variants[Math.floor(Math.random() * variants.length)];
            }
            this.body.textContent = frame;
          }
```

- [ ] **Step 3: Restart server and verify**

```js
() => {
  const c = window.__cat;
  c.cancelBehaviorLoop();
  c.isMoving = false;
  c.setState('idle');
  // Sample animate() 200 times; we should see >5% but <40% variants.
  let variants = 0;
  for (let i = 0; i < 200; i++) {
    c.animate();
    if (c.body.textContent.includes('_,/')) variants++;
  }
  return { variantHits: variants };
}
```

Expected: `variantHits` between 10 and 80 (15% × 200 = 30 expected, with binomial noise). If it's 0 or > 100 something's broken.

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): occasional frame variants for visual surprise

15% chance per animate() to swap in an alt ASCII design (hat-cat, shades,
extra whiskers, bookmark, more music notes) for the relevant state.
Cosmetic only; doesn't perturb the state machine.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Surprise triggers — Konami, typed words, Ctrl+Shift+M

**Files:**
- Modify: `static/index.html` (`init()` and four new trigger methods)

- [ ] **Step 1: Add the four trigger methods to the class**

Add these methods to `DesktopCat` (good location: directly after `pet()` and `interact()`, before `checkSpook`):

```js
          // ─── Surprises ───

          triggerKonami() {
            this.cancelBehaviorLoop();
            this.cancelFollow();
            this.isMoving = false;
            this.setState('matrix');
            this.showMessage('Cheat enabled.');
            this.animateFrames(150, 5000);
            for (let i = 0; i < 8; i++) {
              setTimeout(() => this.spawnEmote('✦', '#00ffff', 1500), i * 120);
            }
            this.clearStateTimer();
            this.stateTimer = setTimeout(() => {
              this.setState('hacking');
              this.showMessage('I\'m in.');
              this.animateFrames(500, 2500);
              this.stateTimer = setTimeout(() => {
                this.hideMessage();
                this.setState('idle');
                this.restartBehaviorLoop(1500);
              }, 2500);
            }, 5000);
          }

          triggerMatrixHotkey() {
            this.cancelBehaviorLoop();
            this.cancelFollow();
            this.isMoving = false;
            this.setState('matrix');
            this.showMessage('Wake up, Neo...');
            this.animateFrames(200, 4000);
            this.clearStateTimer();
            this.stateTimer = setTimeout(() => {
              this.hideMessage();
              this.setState('idle');
              this.restartBehaviorLoop(1500);
            }, 4000);
          }

          triggerCatWord() {
            this.cancelBehaviorLoop();
            this.cancelFollow();
            this.isMoving = false;
            this.setState('playing');
            this.showMessage('*ears perk*');
            this.animateFrames(250, 2500);
            this.clearStateTimer();
            this.stateTimer = setTimeout(() => {
              this.hideMessage();
              this.setState('idle');
              this.restartBehaviorLoop(1500);
            }, 2500);
          }

          triggerMeowWord() {
            this.cancelBehaviorLoop();
            this.cancelFollow();
            this.isMoving = false;
            this.setState('surprised');
            this.showMessage('Mrrow?');
            this.animateFrames(300, 2000);
            this.clearStateTimer();
            this.stateTimer = setTimeout(() => {
              this.hideMessage();
              this.setState('idle');
              this.restartBehaviorLoop(1500);
            }, 2000);
          }
```

- [ ] **Step 2: Add the keystroke listener to `init()`**

In `init()`, find:

```js
            this.cat.addEventListener('transitionstart', () => this.leavePawPrints());
            this.applyPalette();
          }
```

Replace with:

```js
            this.cat.addEventListener('transitionstart', () => this.leavePawPrints());
            this.applyPalette();
            this.installKeystrokeTriggers();
          }
```

- [ ] **Step 3: Add `installKeystrokeTriggers` method**

Add this method to the class (location: immediately after the four trigger methods from Step 1):

```js
          installKeystrokeTriggers() {
            this._konamiSeq = ['ArrowUp','ArrowUp','ArrowDown','ArrowDown','ArrowLeft','ArrowRight','ArrowLeft','ArrowRight','KeyB','KeyA'];
            this._codeBuffer = [];
            this._wordBuffer = '';
            document.addEventListener('keydown', (e) => {
              if (document.hidden) return;
              const tgt = e.target;
              if (tgt && (tgt.tagName === 'INPUT' || tgt.tagName === 'TEXTAREA' || tgt.isContentEditable)) return;
              // Ctrl+Shift+M jump-to-matrix
              if (e.ctrlKey && e.shiftKey && e.code === 'KeyM') {
                e.preventDefault();
                this.triggerMatrixHotkey();
                return;
              }
              // Konami: rolling code buffer
              this._codeBuffer.push(e.code);
              if (this._codeBuffer.length > this._konamiSeq.length) this._codeBuffer.shift();
              if (this._codeBuffer.length === this._konamiSeq.length &&
                  this._codeBuffer.every((k, i) => k === this._konamiSeq[i])) {
                this._codeBuffer = [];
                this.triggerKonami();
                return;
              }
              // Word triggers: rolling letter buffer (case-insensitive)
              if (e.key && e.key.length === 1) {
                this._wordBuffer = (this._wordBuffer + e.key.toLowerCase()).slice(-16);
                if (this._wordBuffer.endsWith('meow')) {
                  this._wordBuffer = '';
                  this.triggerMeowWord();
                } else if (this._wordBuffer.endsWith('cat')) {
                  this._wordBuffer = '';
                  this.triggerCatWord();
                }
              }
            });
          }
```

- [ ] **Step 4: Restart server and verify each trigger**

```js
async () => {
  const c = window.__cat;
  const fire = (init) => new KeyboardEvent('keydown', init);
  // 1. Konami
  c._codeBuffer = []; c.cancelBehaviorLoop(); c.cancelFollow();
  for (const k of ['ArrowUp','ArrowUp','ArrowDown','ArrowDown','ArrowLeft','ArrowRight','ArrowLeft','ArrowRight','KeyB','KeyA']) {
    document.dispatchEvent(fire({ code: k, key: k.startsWith('Arrow') ? k.replace('Arrow','') : (k === 'KeyB' ? 'b' : 'a') }));
  }
  await new Promise(r => setTimeout(r, 50));
  const konami = { state: c.currentState, msg: c.bubble.textContent };

  // 2. Ctrl+Shift+M
  c.cancelBehaviorLoop(); c.cancelFollow();
  document.dispatchEvent(fire({ code: 'KeyM', key: 'M', ctrlKey: true, shiftKey: true }));
  await new Promise(r => setTimeout(r, 50));
  const matrix = { state: c.currentState, msg: c.bubble.textContent };

  // 3. Type "cat"
  c.cancelBehaviorLoop(); c.cancelFollow();
  c._wordBuffer = '';
  for (const ch of 'cat') document.dispatchEvent(fire({ key: ch, code: 'Key' + ch.toUpperCase() }));
  await new Promise(r => setTimeout(r, 50));
  const catWord = { state: c.currentState, msg: c.bubble.textContent };

  // 4. Type "meow"
  c.cancelBehaviorLoop(); c.cancelFollow();
  c._wordBuffer = '';
  for (const ch of 'meow') document.dispatchEvent(fire({ key: ch, code: 'Key' + ch.toUpperCase() }));
  await new Promise(r => setTimeout(r, 50));
  const meowWord = { state: c.currentState, msg: c.bubble.textContent };

  return { konami, matrix, catWord, meowWord };
}
```

Expected:
- `konami.state === 'matrix'`, `konami.msg === 'Cheat enabled.'`
- `matrix.state === 'matrix'`, `matrix.msg === 'Wake up, Neo...'`
- `catWord.state === 'playing'`, `catWord.msg === '*ears perk*'`
- `meowWord.state === 'surprised'`, `meowWord.msg === 'Mrrow?'`

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): keyboard surprise triggers

Konami code (↑↑↓↓←→←→BA) → matrix → hacking chain with
✦ emote storm. Ctrl+Shift+M → quick matrix jump. Typing 'cat' →
playing+'*ears perk*'. Typing 'meow' → surprised+'Mrrow?'. All
ignore inputs/textareas/contenteditable.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Click-streak unlocks (5/10/20)

**Files:**
- Modify: `static/index.html` (`handleInteraction`, add `triggerStreak`)

- [ ] **Step 1: Add `triggerStreak` method**

Add this method to the class (good location: directly after `pet()` and `interact()`, before `checkSpook`):

```js
          triggerStreak(state, msg, dur) {
            this.cancelBehaviorLoop();
            this.cancelFollow();
            this.isMoving = false;
            this.setState(state);
            this.showMessage(msg);
            this.animateFrames(state === 'dancing' ? 300 : 400, dur);
            this.clearStateTimer();
            this.stateTimer = setTimeout(() => {
              this.hideMessage();
              this.setState('idle');
              this.restartBehaviorLoop(1200);
            }, dur);
          }
```

- [ ] **Step 2: Update `handleInteraction`**

Find:

```js
          handleInteraction() {
            this.mood.curiosity = Math.min(100, this.mood.curiosity + 10);
            this.lastUserActivity = Date.now();
            this.clickCount++;
            if (this.clickTimer) clearTimeout(this.clickTimer);
            this.clickTimer = setTimeout(() => { this.clickCount = 0; }, 800);

            if (this.clickCount >= 3) {
              this.pet();
              this.clickCount = 0;
            } else {
              this.interact();
            }
          }
```

Replace with:

```js
          handleInteraction() {
            this.mood.curiosity = Math.min(100, this.mood.curiosity + 10);
            this.lastUserActivity = Date.now();
            this.clickCount++;
            this.streakCount = (this.streakCount || 0) + 1;
            if (this.clickTimer) clearTimeout(this.clickTimer);
            this.clickTimer = setTimeout(() => { this.clickCount = 0; }, 800);
            if (this.streakTimer) clearTimeout(this.streakTimer);
            this.streakTimer = setTimeout(() => { this.streakCount = 0; }, 1500);

            // Streak unlocks (highest first; reset streak on trigger)
            if (this.streakCount === 20) {
              this.streakCount = 0;
              this.triggerStreak('dancing', 'OK fine, dance party.', 5000);
              return;
            }
            if (this.streakCount === 10) {
              this.streakCount = 0;
              this.triggerStreak('annoyed', 'Hey, leash up.', 2500);
              return;
            }
            if (this.streakCount === 5) {
              this.streakCount = 0;
              this.triggerStreak('surprised', 'B O O P', 1500);
              return;
            }

            if (this.clickCount >= 3) {
              this.pet();
              this.clickCount = 0;
            } else {
              this.interact();
            }
          }
```

- [ ] **Step 3: Restart server and verify each tier**

```js
async () => {
  const c = window.__cat;
  const fireClicks = async (n) => {
    c.cancelBehaviorLoop();
    c.cancelFollow();
    c.streakCount = 0;
    c.clickCount = 0;
    if (c.streakTimer) { clearTimeout(c.streakTimer); c.streakTimer = null; }
    if (c.clickTimer) { clearTimeout(c.clickTimer); c.clickTimer = null; }
    for (let i = 0; i < n; i++) c.cat.click();
    await new Promise(r => setTimeout(r, 100));
    return { state: c.currentState, msg: c.bubble.textContent };
  };
  return {
    five: await fireClicks(5),
    ten: await fireClicks(10),
    twenty: await fireClicks(20),
  };
}
```

Expected:
- `five.state === 'surprised'`, msg `'B O O P'`
- `ten.state === 'annoyed'`, msg `'Hey, leash up.'`
- `twenty.state === 'dancing'`, msg `'OK fine, dance party.'`

- [ ] **Step 4: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): click-streak unlocks at 5/10/20

Independent of the existing 3-click-pet (which uses its own clickCount).
streakCount resets after 1500 ms idle or after a tier triggers, so users
can re-arm. 5→boop, 10→annoyed, 20→dance party.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Visit milestone hook

**Files:**
- Modify: `static/index.html` (visit-counter IIFE around line 5919, plus add `onVisitCount` method to `DesktopCat` and a listener registration in `init`)

- [ ] **Step 1: Make the visit-counter IIFE dispatch a custom event**

Find:

```js
        (function () {
          const el = document.getElementById('visit-count');
          fetch('/api/visit', { method: 'POST' })
            .then(r => r.ok ? r.json() : null)
            .then(data => {
              if (el && data && data.count != null) {
                el.textContent = data.count.toLocaleString() + ' visits';
              }
            })
            .catch(() => { /* silent: no counter is fine */ });
        })();
```

Replace with:

```js
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

- [ ] **Step 2: Add `onVisitCount` method to `DesktopCat`**

Add this method to the class (good location: directly after `triggerStreak`):

```js
          onVisitCount(count) {
            const tiers = [100, 500, 1000, 5000, 10000, 100000];
            let crossed = 0;
            for (const t of tiers) if (count >= t) crossed = t;
            if (!crossed) return;
            let prev = 0;
            try { prev = parseInt(localStorage.getItem('kd:cat:last-visit-tier') || '0', 10) || 0; } catch (e) {}
            if (crossed <= prev) return;
            try { localStorage.setItem('kd:cat:last-visit-tier', String(crossed)); } catch (e) {}
            this.cancelBehaviorLoop();
            this.cancelFollow();
            this.isMoving = false;
            this.setState('celebrate');
            const labels = {
              100:    '100 visits! Mrow!',
              500:    '500 visits!',
              1000:   '1k visits! \u{1F389}',
              5000:   '5k visits!',
              10000:  '10k visits!',
              100000: '100k visits! \u{1F389}',
            };
            this.showMessage(labels[crossed]);
            this.animateFrames(300, 5000);
            for (let i = 0; i < 8; i++) {
              setTimeout(() => this.spawnEmote(['★','✨','♦','♥'][Math.floor(Math.random()*4)], '#ffd166', 1500), i * 100);
            }
            this.clearStateTimer();
            this.stateTimer = setTimeout(() => {
              this.hideMessage();
              this.setState('idle');
              this.restartBehaviorLoop(1500);
            }, 5500);
          }
```

- [ ] **Step 3: Register the listener in `init()`**

In `init()`, find:

```js
            this.cat.addEventListener('transitionstart', () => this.leavePawPrints());
            this.applyPalette();
            this.installKeystrokeTriggers();
          }
```

Replace with:

```js
            this.cat.addEventListener('transitionstart', () => this.leavePawPrints());
            this.applyPalette();
            this.installKeystrokeTriggers();
            window.addEventListener('kd-visit-count', (e) => this.onVisitCount(e.detail.count));
          }
```

- [ ] **Step 4: Restart server and verify**

```js
async () => {
  const c = window.__cat;
  // Fresh slate: clear any prior tier the test left in storage.
  localStorage.removeItem('kd:cat:last-visit-tier');
  c.cancelBehaviorLoop();
  c.cancelFollow();
  // Fire a synthetic visit-count event (1500 -> tier 1000)
  window.dispatchEvent(new CustomEvent('kd-visit-count', { detail: { count: 1500 } }));
  await new Promise(r => setTimeout(r, 200));
  const first = { state: c.currentState, msg: c.bubble.textContent, stored: localStorage.getItem('kd:cat:last-visit-tier') };
  // Fire again with the same count — should NOT re-trigger
  c.setState('idle');
  c.bubble.classList.remove('show');
  window.dispatchEvent(new CustomEvent('kd-visit-count', { detail: { count: 1500 } }));
  await new Promise(r => setTimeout(r, 200));
  const second = { state: c.currentState, msg: c.bubble.textContent };
  return { first, second };
}
```

Expected:
- `first.state === 'celebrate'`, `first.msg` includes `'1k visits!'`, `first.stored === '1000'`
- `second.state === 'idle'`, `second.msg` empty (does NOT re-trigger)

- [ ] **Step 5: Commit**

```bash
git add static/index.html
git commit -m "feat(cat): visit-milestone celebrate state

Visit IIFE dispatches kd-visit-count after the POST /api/visit response.
Cat listens for it; on a newly-crossed tier (100/500/1k/5k/10k/100k) it
fires the celebrate state with a milestone-aware bubble + 8 emote storm.
localStorage 'kd:cat:last-visit-tier' suppresses re-trigger across
reloads at the same tier.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: Final E2E verification + screenshot gallery

**Files:**
- (No code changes; verification only.)

- [ ] **Step 1: Restart server one last time and walk through every new state**

```js
async () => {
  const c = window.__cat;
  c.cancelBehaviorLoop();
  c.cancelFollow();
  const want = ['reading','researching','listening','vibing','watching','soldering','disc-spin','low-power','demoscene','stargaze','lunch','celebrate'];
  const out = [];
  for (const s of want) {
    c.executeBehavior(s);
    await new Promise(r => setTimeout(r, 80));
    out.push({ state: s, body: c.body.textContent.replace(/\n/g,' | '), msg: c.bubble.textContent });
  }
  return out;
}
```

Expected: 12 entries, each with non-empty body and msg.

- [ ] **Step 2: Take a screenshot of each new state**

For each state in `want`, in Playwright:
- `c.cancelBehaviorLoop(); c.cancelFollow(); c.executeBehavior('<state>'); c.cat.style.right='30px'; c.cat.style.bottom='50px'; c.cat.style.transition='none';`
- screenshot to `state-<name>.png`

Skim the gallery — each state should be visually distinct and the bubble readable.

- [ ] **Step 3: Smoke-test the existing states still work**

```js
async () => {
  const c = window.__cat;
  c.cancelBehaviorLoop();
  c.cancelFollow();
  const existing = ['idle','sitting','thinking','coding','debugging','annoyed','coffee','archiving','downloading','defrag','surfing','hacking','matrix','sleeping','waking','playing','dancing','gamer','glitch','bsod'];
  const broken = [];
  for (const s of existing) {
    c.executeBehavior(s);
    await new Promise(r => setTimeout(r, 60));
    if (!c.body.textContent || c.currentState !== s) broken.push(s);
  }
  return { broken };
}
```

Expected: `broken: []`.

- [ ] **Step 4: Commit screenshots and final test confirmation**

```bash
# (Screenshots are throwaway — don't commit them. Just confirm no regressions.)
git status
```

Expected: clean working tree (since prior tasks already committed all source changes).

- [ ] **Step 5: Cleanup**

```bash
pkill -f /tmp/kdhome
rm -f /tmp/kdhome
```
