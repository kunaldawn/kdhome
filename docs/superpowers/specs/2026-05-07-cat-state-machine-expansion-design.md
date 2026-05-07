# Cat State Machine — Expansion + Audit (2026-05-07)

## Goal

Audit the existing desktop-cat state machine for residual bugs, expand it with
12 new homepage-themed states, add four classes of surprises (key combos,
time-of-day, visit milestones, click streaks), and introduce visual variants
(color palettes + occasional alt ASCII frames). Verify each addition end-to-end
with Playwright.

## In scope

- `static/index.html` only. The cat code lives entirely inside that file
  (`DesktopCat` class around line 5920).

## Out of scope

- Music-window avoid logic (already shipped earlier this session).
- Z-order / mobile-positioning fixes (already shipped earlier this session).
- Server-side changes — none needed.

## 1. Bug audit fixes

| # | Bug | Fix |
|---|---|---|
| B1 | `followPoint` inner `setTimeout` resets state to `sitting` even when interrupted. | Track a `followToken`; the inner timeout bails out if the token has been invalidated (interrupted or follow restarted). |
| B2 | `leavePawPrints` listens to `transitionstart` for *every* style change, including the empty-`transform` cleanup in `setState`. | Gate emission on `currentState ∈ {walking, running, wander}` so prints only happen during locomotion. |
| B3 | `pickWeighted` floating-point boundary bias (uses `r <= 0` after subtraction; the equal case picks the next bucket). | Change to `r < 0` after subtraction so the boundary case stays in the current bucket. |
| B4 | Document-wide `click` listener bumps `mood.curiosity` even from inertial scroll-bar drags / synthetic taps. | Add a 250 ms rate-limit gate around the curiosity bump. |

## 2. New states (12)

Each state adds entries to: `frames`, `msgPools`, `transitions` (incl. inbound
edges from existing states), `stateDurations`, and an `executeBehavior` case.

| Name | Theme | Frames (final spacing matched to existing 8-cell grid) | Sample messages | Duration | Inbound from |
|---|---|---|---|---|---|
| `reading` | Wiki / PDF | `( o.o )=` ↔ `( -.- )=` | "Page 4,821 of 23,000…", "[citation needed]", "*flips page*", "Cite: Knuth, 1968" | 5000 | `surfing`, `idle`, `archiving` |
| `researching` | Wiki | `( ?.? )` ↔ `( o.o )?` | "TIL the obscure", "depth=8 levels", "*scans index*" | 4000 | `thinking`, `surfing` |
| `listening` | Audiobook | `(-_-)Π` ↔ `(o_o)Π` | "Chapter 12: A Tale of Two Servers", "*listening*", "narrator @ 1.25x" | 4500 | `idle`, `coffee` |
| `vibing` | Chiptune | `( ^.^ )♪` ↔ `( ^.^ )♫` | "Untz untz", ".XM module loaded", "*nods to beat*", "Tracker: 4ch" | 4000 | `idle`, `dancing`, `playing` |
| `watching` | Tube | `( O.O )*` ↔ `( o.o )*` | "*munch munch*", "Skipping ads…", "Buffer: 99%", "1080p preserved" | 4000 | `idle`, `surfing` |
| `soldering` | OS Archive (hardware) | `( o.o )+` ↔ `( -.o )+` | "Pin 3 reflowed", "Smoke = working", "Why warm?", "+5V rail OK" | 4500 | `coding`, `debugging` |
| `disc-spin` | CD/DVD | `( o.o )(O)` ↔ `( o.o )(\|)` | "*chk-chk-chk*", "Insert disc 2", "Reading TOC…", "ISO9660" | 4000 | `archiving`, `downloading` |
| `low-power` | Off-grid solar | `( -.- )░` ↔ `( -.- )▒` | "30 W is plenty", "*solar charging*", "throttled", "kWh efficient" | 5000 | `idle`, `sleeping` (low energy) |
| `demoscene` | Demo window | `(♥.♥)` ↔ `(★.★)` | "Greetz to the scene", "/\* 64 bytes left \*/", "4 KB demo", "scrolltext goes here →" | 4000 | `coding`, `hacking`, `matrix` |
| `stargaze` | Idle alt | `( ^.^ )` (looks up) ↔ `( o.o )` | "*contemplates 12 TB*", "cosmic rays…", "12 TB is just numbers", "Carl Sagan would approve" | 4000 | `idle`, `sitting` |
| `lunch` | Time of day 12–13h | `(>.<)><((°>` ↔ `( ^.^ )><` | "*nom nom*", "1 PM tuna time", "404: Salmon found", "*purr while eating*" | 3500 | weighted from `idle` only between 12:00–13:00 |
| `celebrate` | Visit milestone | `( ^o^ )b` ↔ `( ^O^ )b` | "100 visits! Mrow!", "🎉 1k visits!", "Achievement unlocked", "*party*" | 4000 | injected on milestone crossing |

Themed weight biases injected in `getNextState`:

- If music-window visible & not minimized → `vibing` weight ↑15 (chiptune
  fits the music context). `listening` is *not* tied to the music window
  because the audiobook archive is a browser-window experience, not the
  in-page chiptune player.
- If browser-window visible (any archive being browsed) → `reading`,
  `watching`, `researching`, `listening` weights ↑8 each.
- If demo-window visible → `demoscene` weight ↑15.
- Hour ∈ [12, 13) local time → `lunch` weight ↑40 (only inbound from `idle`).
- `mood.energy < 30` → `low-power` weight ↑20.

## 3. Surprises

### 3.1 Key combos

- **Konami sequence** `↑↑↓↓←→←→BA` (or `KeyB` `KeyA`) tracked across `keydown`.
  Match → cancel behavior loop, force a chain `matrix → hacking → idle`,
  bubble "Cheat enabled.", spawn 8 ✦ emotes.
- **Type "cat"** anywhere (case-insensitive, in any non-input element): cat
  does `playing` for 2.5 s with "*ears perk*" bubble.
- **Type "meow"**: cat does `surprised` with "Mrrow?" bubble.
- **Ctrl+Shift+M**: jump to `matrix` for 4 s.

Implementation: a single rolling buffer of last 12 keydowns scoped to
`document` (excluding `INPUT`, `TEXTAREA`, `[contenteditable]`).

### 3.2 Time of day

Existing weighting (sleeping at night, coffee in morning) preserved. Adding:

- 12:00–13:00 local time: `lunch` injected with weight 40 from `idle`.
- Friday 17:00–23:59 local time (`getDay()===5 && hour>=17`): `vibing` and
  `dancing` weight ↑12 each. (Friday only, not the whole weekend.)

### 3.3 Visit milestones

Hook the existing visit-counter fetch (`fetch('/visits')` round-trip in the
home script). After the count is fetched once at load, compare against
thresholds `[100, 500, 1000, 5000, 10000, 100000]` and crossover from a stored
`localStorage` value `kd:cat:last-visit-tier`. If a new tier was crossed
**since the user's last visit**, fire `celebrate` once with a count-aware
message ("100 visits!", "🎉 1k!", etc.) plus 8 emote particles.

Storage key: `kd:cat:last-visit-tier`. Stores the integer of the highest tier
already celebrated so we don't celebrate the same milestone twice on reload.

### 3.4 Click-streak unlocks

Extend `handleInteraction()`:

- Existing `clickCount >= 3` → `pet()` (kept as-is).
- Add a *separate* `streakCount` that increments on every click and resets
  after 1500 ms idle. Thresholds:
  - `streakCount === 5`: `boop` (surprised + "B O O P")
  - `streakCount === 10`: `annoyed` + "Hey, leash up."
  - `streakCount === 20`: `dancing` + "OK fine, dance party." (5 s)

`streakCount` resets after each unlock to avoid retriggering at every
subsequent click.

## 4. Variants

### 4.1 Color palettes

Picked once on construction via weighted random and stored in
`this.palette`. Applied via inline style on `.cat-body` (color, textShadow)
and on `.desktop-cat` (drop-shadow filter color), overriding only the
defaults — `stateStyles` (matrix, glitch, bsod, love, coffee, annoyed) still
take precedence per-state.

| Palette | Color | Glow | Weight |
|---|---|---|---|
| Neon (default) | `#7fd1b3` | green | 80 |
| Orange tabby | `#ffaa44` | warm | 7 |
| Tuxedo | `#e8eef0` | cool slate | 5 |
| Cyberpunk pink | `#ff66cc` | magenta | 4 |
| Solar yellow | `#f4d35e` | amber | 2 |
| Matrix dark | `#33ff77` | dark green | 2 |

### 4.2 Frame variants

A small parallel `frameVariants` object keyed by a *base* state name. When
`animate()` selects a frame, with 15% probability it picks from the variant
pool for that state (if any) instead of the default. Variants by state:

- `idle`: hat-cat (`_/\_/\_` with `<-_->` brim line) — 1 frame
- `sitting`: long-whiskers `>>=^.^=<<` — 1 frame
- `coding` / `hacking`: shades-cat `(=.=)` instead of normal eyes — 1 frame
- `reading`: bookmark `( o.o )=▤` — 1 frame
- `vibing`: extra-notes `( ^.^ )♪♫♬` — 1 frame

Variant flicker is independent per `animate()` call so the effect is
"occasional, charming" rather than persistent.

## 5. Verification plan

For each addition, drive it deterministically through `window.__cat` and
verify in Playwright:

1. **Bug fixes** (B1–B4): trigger the race condition / paw-print spam path,
   check counts in console / DOM.
2. **Each new state**: `__cat.setState('<name>')` + `executeBehavior('<name>')`
   in turn, screenshot, confirm `frames[name]` resolves and the bubble shows
   one of the pool's messages.
3. **Themed weight biases**: open browser/demo/music windows, run `getNextState`
   1000× and confirm distribution shifts in the expected direction.
4. **Konami code**: synthesize the keydown sequence; confirm a `matrix → hacking`
   chain and the cheat bubble.
5. **Type triggers**: synthesize "cat" / "meow" keystrokes; confirm bubble.
6. **Click streak**: rapid `cat.click()` calls; assert state at 5, 10, 20.
7. **Visit milestone**: stub the `/visits` response with `{count: 105}`,
   reload, confirm `celebrate` fires; reload again with same count, confirm
   it does NOT fire (storage tier guard).
8. **Variants**: instantiate cat 200× (in evaluate), tally palette
   distribution; assert weights within tolerance.

## 6. Risks / open questions

- **Performance**: 12 new states + variant lookup + emote spawns are all
  cheap, but the full-document keystroke buffer is new. Buffer size 12 chars,
  scoped to keydown, ignored on inputs — no real risk.
- **Visual noise**: variants firing at 15% during idle could feel busy. If
  feedback says so, drop to 8% — change-one-line.
- **`localStorage` privacy**: only stores the integer milestone tier; no PII.
