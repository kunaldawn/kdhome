# Demoscene Signin Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the basic `/login` page with a full demoscene signin screen (live KD.FX intro + terms + archive inventory + Google button) and delete the post-login entry-gate overlay from the archive page.

**Architecture:** Extract the ~2685-line FX engine out of `static/index.html` into a shared, publicPath `static/fx.js` loaded by both the archive page and the pre-auth login page. Build the login page as a `go:embed`-ed `html/template` (`login.html`) rendered by `authConfig.loginPage`, injecting the validated Google start URL. Remove all entry-gate markup, CSS, and scripts from `index.html`.

**Tech Stack:** Go (`embed`, `html/template`, net/http), vanilla JS/CSS. No new dependencies.

## Global Constraints

- No new Go module dependencies — `embed`, `html/template`, `bytes` are stdlib.
- The FX engine MUST be moved **verbatim** — byte-for-byte, no edits. It is shared by the archive's Demo window (`index.html` `#demo-fx-canvas`), which must keep working.
- Open-redirect defense is unchanged: the redirect is re-validated by `safeRedirect` at `/auth/google/start`. Never weaken this.
- `publicPaths` (`auth.go`) is an exact-match map; static files served from `./static` at their root path.
- Theme: monospace, dark, neon green `#00ff99` / cyan `#7fd1b3` / magenta `#ff48a0` on near-black `#05110d`.
- The page is pre-auth: it may only load assets that are in `publicPaths` (so `/fx.js` must be public).

---

## File Structure

- **Create** `static/fx.js` — the demoscene FX engine (`window.createFxInstance`), moved verbatim from `index.html`.
- **Create** `login.html` (repo root) — `html/template` source for the demoscene signin page.
- **Modify** `auth.go` — `loginPage` renders the embedded template; add `/fx.js` to `publicPaths`; swap `html` import for `html/template` (+ `embed`, `bytes`).
- **Modify** `static/index.html` — replace the inline FX `<script>` block with `<script src="/fx.js"></script>`; delete the entry-gate (markup, CSS, body class, early localStorage script, accept IIFE, FX boot).
- **Modify** `auth_test.go` — extend `/login` and publicPath tests.

---

### Task 1: Extract FX engine to shared `static/fx.js`

**Files:**
- Create: `static/fx.js`
- Modify: `static/index.html:4247-6947` (inline FX `<script>` block), `static/index.html:10636` (demo boot — unchanged, just verify)
- Modify: `auth.go` (publicPaths map, ~`auth.go:173-190`)
- Test: `auth_test.go:105` (`TestMiddlewarePublicBypass`)

**Interfaces:**
- Produces: `static/fx.js` exposing global `window.createFxInstance(cfg)` where `cfg = { canvas, ticker, sceneLabel, timerLabel, gate?, installDebug? }`. Returns `{ stop: fn }`.
- Consumes: nothing (first task).

- [ ] **Step 1: Create `static/fx.js` from the engine block**

Move `static/index.html` lines **4249–6933** (the statement `window.createFxInstance = function(cfg) { … };`) **verbatim** into a new file `static/fx.js`. Prepend the comment currently at line 4248:

```js
/* ═══ Demoscene ASCII intro (bb/aalib-inspired) ═══ */
window.createFxInstance = function(cfg) {
  // ... lines 4250–6932 copied byte-for-byte ...
};
```

Do NOT copy the entry-gate boot IIFE (lines 6935–6946) — it is entry-gate-specific and gets dropped here. Do NOT include `<script>` tags — `fx.js` is a plain JS file.

- [ ] **Step 2: Replace the inline block in `index.html` with an external reference**

Delete `static/index.html` lines **4247–6947** (the entire `<script> … </script>` that held the comment, the engine, and the entry-gate boot) and replace with a single line at the same position:

```html
  <script src="/fx.js"></script>
```

This is a synchronous, parser-blocking script placed before the Demo window script (~line 10636), so `window.createFxInstance` is defined in order. (The entry-gate `#fx-canvas` markup still present at this point will simply not be booted; Task 2 removes it.)

- [ ] **Step 3: Add `/fx.js` to publicPaths**

In `auth.go`, inside the `publicPaths` map, add:

```go
	"/fx.js":                      true,
```

- [ ] **Step 4: Extend the publicPath bypass test (write failing test)**

In `auth_test.go`, in `TestMiddlewarePublicBypass`, add `"/fx.js"` to the path list:

```go
	for _, p := range []string{"/login", "/fx.js", "/robots.txt", "/auth/google/start", "/og-image.png", "/favicon.ico"} {
```

- [ ] **Step 5: Run tests**

Run: `go test ./... -run TestMiddlewarePublicBypass -v`
Expected: PASS.

- [ ] **Step 6: Build and manually verify FX engine still serves**

Run: `go build ./... && go vet ./...`
Expected: builds clean.
Run server (`go run .` with auth disabled or a valid session) and confirm:
- `GET /fx.js` returns 200 `text/javascript` (or `application/javascript`).
- Archive page Demo window (Start menu → Demo) still animates — the extraction regression check.

- [ ] **Step 7: Commit**

```bash
git add static/fx.js static/index.html auth.go auth_test.go
git commit -m "refactor: extract demoscene FX engine to shared static/fx.js"
```

---

### Task 2: Remove the entry-gate overlay from `index.html`

**Files:**
- Modify: `static/index.html` — body class (4118), early localStorage IIFE (4132–4142), entry-gate markup + accept IIFE (4167–4246), entry-gate CSS blocks.

**Interfaces:**
- Consumes: Task 1 (FX engine now in `fx.js`; the entry-gate's FX boot already gone).
- Produces: an archive page with no terms gate — authenticated users land directly in the desktop.

- [ ] **Step 1: Drop the locked body class**

`static/index.html:4118` — change:

```html
<body class="entry-gate-locked">
```
to:
```html
<body>
```

- [ ] **Step 2: Remove the terms-accepted early script**

Delete `static/index.html` lines **4132–4142** (the second IIFE, the one reading `kd:prefs:terms-accepted`). KEEP the transparency IIFE immediately above it (lines 4125–4131) and the surrounding `<script>`/`</script>` (4119, 4143).

- [ ] **Step 3: Remove the entry-gate markup and accept IIFE**

Delete `static/index.html` lines **4167–4246** inclusive — from the `<!-- Entry gate: ... -->` comment through the entire entry-gate `<div>` markup and its accept-checkbox `<script> … </script>`. The SVG filter block above (4144–4166) and the content below (4248+, now the `<!-- Advanced Desktop Cat -->` region) stay.

- [ ] **Step 4: Remove entry-gate CSS rule blocks**

In the `<style>` of `static/index.html`, delete these rule blocks verbatim (selector → approx line range; delete each full `{ … }` block):

- `.entry-gate { … }` and `.entry-gate.dismissing { … }` (3438–3457)
- `.entry-gate-glass { … }`, `.entry-gate-glass::before { … }`, `.entry-gate.dismissing .entry-gate-glass { … }` (3458–3490)
- `.entry-gate-title { … }` (3491–3509)
- `.entry-terms { … }`, `.entry-terms:hover { … }`, `.entry-terms-head { … }` (3795–3829)
- `.entry-check { … }` through `.entry-check-label b { … }` (3830–3877)
- `.entry-rules { … }`, `.entry-rules li { … }`, `.entry-rules .bul { … }` (3878–3899)
- `.entry-gate-btn { … }` through `.entry-cursor { … }` and the entry-gate `@keyframes`/cursor rules (3900–3938)
- `body.entry-gate-locked { … }` and `body.entry-gate-accepted .entry-gate { … }` (3948–3949)
- The two mobile media-query lines for the entry-gate inside the `@media` block at 3939–3945 (`.entry-gate`, `.entry-gate-glass`, `.entry-gate-title`, `.entry-terms`, `.entry-terms-head`, `.entry-rules`, `.entry-gate-btn`).

In the `body.no-transparency` rules: remove `.entry-gate` (3965) and `.entry-gate-glass` (3966) from the shared `backdrop-filter` selector list, and delete the two standalone blocks `body.no-transparency .entry-gate { … }` (3987–3989) and `body.no-transparency .entry-gate-glass { … }` (3990–3992).

KEEP: all `.fx-*` rules (3510 `.sr-only`, 3519–3600 `.fx-wrap`/`.fx-header`/`.fx-footer`/`.fx-ticker*`, 3556–3792 `.fx-canvas*`) — the Demo window uses them.

- [ ] **Step 5: Verify no entry-gate references remain**

Run: `grep -nE "entry-gate|entry-terms|entry-check|entry-rules|entry-cursor|kd:prefs:terms-accepted|entry-gate-locked|entry-gate-accepted" static/index.html`
Expected: **no output** (zero matches).

- [ ] **Step 6: Build/serve and confirm archive loads with no overlay**

Run server, load `/` with a valid session.
Expected: desktop loads directly, no terms overlay, no console errors. Demo window FX still works.

- [ ] **Step 7: Commit**

```bash
git add static/index.html
git commit -m "feat: remove post-login entry-gate terms overlay"
```

---

### Task 3: Build the demoscene signin page (`login.html` + template render)

**Files:**
- Create: `login.html` (repo root)
- Modify: `auth.go` — imports, `loginPage` (357–408), keep `handleLogin` (214–219)
- Test: `auth_test.go:168` (`TestLoginPageEmbedsRedirect`)

**Interfaces:**
- Consumes: `static/fx.js` global `window.createFxInstance(cfg)` (Task 1); `/fx.js` publicPath (Task 1).
- Produces: `loginPage(redirect string) []byte` rendering the embedded template with field `StartHref string`.

- [ ] **Step 1: Write the failing test**

Replace `TestLoginPageEmbedsRedirect` in `auth_test.go` with the expanded version:

```go
func TestLoginPageEmbedsRedirect(t *testing.T) {
	c := testAuthConfig()
	page := string(c.loginPage("https://wiki.kunaldawn.com/x"))
	want := "/auth/google/start?redirect=" + url.QueryEscape("https://wiki.kunaldawn.com/x")
	if !strings.Contains(page, want) {
		t.Fatalf("login page should link to %q", want)
	}
	for _, frag := range []string{`id="fx-canvas"`, `src="/fx.js"`, "no ads", "Sign in with Google"} {
		if !strings.Contains(page, frag) {
			t.Fatalf("login page missing expected fragment %q", frag)
		}
	}
}

func TestLoginPageEscapesRedirect(t *testing.T) {
	c := testAuthConfig()
	// A hostile redirect is neutralized by safeRedirect at the handler, but the
	// template must never emit a raw double-quote that breaks the href attribute.
	page := string(c.loginPage(`x" onerror="alert(1)`))
	if strings.Contains(page, `onerror="alert(1)"`) {
		t.Fatalf("login page must not emit an unescaped attribute injection")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestLoginPage -v`
Expected: FAIL (fragments `id="fx-canvas"`/`src="/fx.js"`/`no ads` not yet present).

- [ ] **Step 3: Create `login.html`**

Create `login.html` at the repo root. Structure below; copy the bracketed verbatim ranges from `static/index.html` exactly.

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in — kunaldawn.com</title>
<meta property="og:title" content="KD's Homebrew Digital Archive">
<meta property="og:description" content="A home-grown mirror of the public internet. Sign in to continue.">
<meta property="og:image" content="/og-image.png">
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    background: #05110d; color: #cfeee0;
    font-family: 'Share Tech Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
    padding: 24px;
    background-image: radial-gradient(circle at 50% 0%, rgba(127,209,179,0.08), transparent 60%);
  }
  .signin {
    width: 100%; max-width: 520px;
    border: 1px solid rgba(127,209,179,0.35); border-radius: 12px;
    padding: 26px 22px 30px; background: rgba(10,28,22,0.6);
    box-shadow: 0 0 40px rgba(0,0,0,0.5);
    display: flex; flex-direction: column; gap: 18px;
  }
  .signin-title {
    text-align: center; font-size: 18px; margin: 4px 0 0;
    color: #7fd1b3; letter-spacing: 2px;
  }
  .signin-inventory {
    font-size: 11px; line-height: 1.5; opacity: 0.6;
    max-height: 92px; overflow: auto; text-align: left;
  }
  .signin-btn {
    align-self: center; display: inline-flex; align-items: center; gap: 10px;
    padding: 13px 26px; border-radius: 6px; text-decoration: none;
    background: #7fd1b3; color: #05110d; font-weight: bold; letter-spacing: 1px;
  }
  .signin-btn:hover { background: #a8d8c4; }
  .signin-foot { text-align: center; font-size: 11px; opacity: 0.5; }

  /* ── COPY VERBATIM from static/index.html ─────────────────────────────
     Paste these rule blocks here, byte-for-byte, in this order:
       • .fx-wrap, .fx-wrap::after                         (lines 3519–3537)
       • .fx-header, .fx-footer + descendant rules         (lines 3538–3555)
       • .fx-canvas base + every .fx-canvas.fx-* + the
         two @media blocks that tune .fx-canvas/.fx-ticker (lines 3556–3792)
       • .fx-ticker-wrap, .fx-ticker + .tk-* rules         (lines 3572–3600)
       • .entry-terms, .entry-terms:hover, .entry-terms-head (lines 3795–3829)
       • .entry-rules, .entry-rules li, .entry-rules .bul  (lines 3878–3899)
     ──────────────────────────────────────────────────────────────────── */
</style>
</head>
<body>
  <main class="signin">
    <div class="fx-wrap" aria-hidden="true">
      <div class="fx-header"><span class="fx-tag">KD/HOMEBREW</span><span class="fx-mid">PRV-INTRO/0x01</span><span class="fx-fx">FX:<span class="fx-scene">KD.FX</span></span></div>
      <pre class="fx-canvas fx-kdfx" id="fx-canvas"></pre>
      <div class="fx-ticker-wrap">
        <pre class="fx-ticker" id="fx-ticker"></pre>
      </div>
      <div class="fx-footer"><span class="fx-chan">&gt; bbs/kd ·  node 01</span><span class="fx-timer" id="fx-timer">T+0.0s</span></div>
    </div>

    <h1 class="signin-title">KD's Homebrew Digital Archive</h1>

    <div class="entry-terms" role="group" aria-label="Terms">
      <pre class="entry-terms-head" aria-label="Terms">
████████╗███████╗██████╗ ███╗   ███╗███████╗
╚══██╔══╝██╔════╝██╔══██╗████╗ ████║██╔════╝
   ██║   █████╗  ██████╔╝██╔████╔██║███████╗
   ██║   ██╔══╝  ██╔══██╗██║╚██╔╝██║╚════██║
   ██║   ███████╗██║  ██║██║ ╚═╝ ██║███████║
   ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚══════╝
</pre>
      <ul class="entry-rules">
        <li><span class="bul">&raquo;</span> no ads</li>
        <li><span class="bul">&raquo;</span> basic analytics</li>
        <li><span class="bul">&raquo;</span> google login</li>
        <li><span class="bul">&raquo;</span> no uptime guarantee</li>
        <li><span class="bul">&raquo;</span> no monetization</li>
      </ul>
    </div>

    <p class="signin-inventory">[COPY VERBATIM the inventory text from static/index.html:4178 — the full paragraph beginning "KD's Homebrew Digital Archive — a home-grown mirror of the public internet…" through "…Developed by Kunal Dawn with AI assistance."]</p>

    <a class="signin-btn" href="{{.StartHref}}">Sign in with Google</a>
    <div class="signin-foot">free for all, free forever</div>
  </main>

  <script src="/fx.js"></script>
  <script>
    (function () {
      if (typeof window.createFxInstance !== 'function') return;
      if (!document.getElementById('fx-canvas')) return;
      window.createFxInstance({
        canvas: document.getElementById('fx-canvas'),
        ticker: document.getElementById('fx-ticker'),
        sceneLabel: document.querySelector('.fx-scene'),
        timerLabel: document.getElementById('fx-timer')
      });
    })();
  </script>
</body>
</html>
```

- [ ] **Step 4: Wire the embed + template in `auth.go`**

Edit the import block (`auth.go:3-18`): remove `"html"`, add `"bytes"`, `"embed"`, and `"html/template"`. Result:

```go
import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)
```

(If `embed` triggers "imported and not used" because only the directive needs it, keep the blank-free form — the `//go:embed` directive counts as use. If the linter still complains, use `_ "embed"`.)

Add the embed + parsed template as package vars (near the top of `auth.go`, after the imports):

```go
//go:embed login.html
var loginHTML string

var loginTmpl = template.Must(template.New("login").Parse(loginHTML))
```

Replace the whole `loginPage` function (`auth.go:357-408`) with:

```go
// loginPage renders the self-contained themed demoscene sign-in page. redirect
// is already validated by the caller; it is URL-encoded into the Google start
// link, and html/template auto-escapes it for the attribute context.
func (c authConfig) loginPage(redirect string) []byte {
	startHref := "/auth/google/start?redirect=" + url.QueryEscape(redirect)
	var buf bytes.Buffer
	if err := loginTmpl.Execute(&buf, struct{ StartHref string }{startHref}); err != nil {
		// Template is compiled at init; an execution error is a programmer bug.
		return []byte("sign-in temporarily unavailable")
	}
	return buf.Bytes()
}
```

`handleLogin` (`auth.go:214-219`) is unchanged.

- [ ] **Step 5: Run the tests**

Run: `go test ./... -run TestLoginPage -v`
Expected: PASS for both `TestLoginPageEmbedsRedirect` and `TestLoginPageEscapesRedirect`.

- [ ] **Step 6: Full build, vet, and test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass; `html` import gone, no unused-import errors.

- [ ] **Step 7: Manual end-to-end check**

Serve with auth enabled. In a fresh browser (no session):
- Hit any path → 302 to `/login` → demoscene signin renders, KD.FX animates, terms + inventory visible, "Sign in with Google" present.
- Click sign-in → Google consent → callback → land back in the archive desktop with **no** overlay.

- [ ] **Step 8: Commit**

```bash
git add login.html auth.go auth_test.go
git commit -m "feat: demoscene sign-in page replacing basic /login"
```

---

## Self-Review

**Spec coverage:**
- New rich `/login` with KD.FX + terms + inventory + Google button → Task 3. ✓
- No accept checkbox (informational terms only) → Task 3 markup omits `.entry-check`/`[ ENTER ]`. ✓
- FX engine extracted to shared publicPath `static/fx.js` → Task 1. ✓
- `go:embed` + `html/template` render with templated redirect → Task 3 Step 4. ✓
- Entry-gate overlay (markup/CSS/body class/localStorage/boot) removed from `index.html` → Tasks 1 (boot) + 2 (rest). ✓
- Demo window keeps working (shared `.fx-canvas` CSS + engine) → Task 1 Step 6, Task 2 Step 4 KEEP list. ✓
- Open-redirect re-validated at `/auth/google/start` (unchanged) → Global Constraints; `TestLoginPageEscapesRedirect` guards attribute escaping. ✓
- Tests: `/login` 200 with FX/terms/Google link, `/fx.js` public, gated path still 302s → Tasks 1 & 3 tests (`TestMiddlewareRedirectsWhenNoCookie` already covers the 302). ✓

**Placeholder scan:** The only "copy verbatim" directives (FX engine, FX/terms CSS, inventory paragraph) carry exact file + line ranges — concrete, not vague. No TBD/TODO.

**Type consistency:** `loginPage(redirect string) []byte` signature unchanged (callers `handleLogin` + tests untouched). Template field `StartHref` matches `{{.StartHref}}`. `createFxInstance(cfg)` cfg keys match between `fx.js`, the Demo window boot, and the login boot.
