# Demoscene Signin Page — Design

**Date:** 2026-06-28
**Status:** Approved, ready for planning

## Problem

Two separate auth-adjacent surfaces exist today:

1. **`/login`** (`auth.go:loginPage`) — a tiny, plain themed page with a single
   "Sign in with Google" button. Shown to unauthenticated visitors (the whole
   site sits behind the auth middleware at `auth.go:middleware`, which 302s
   unauthenticated requests to `/login`).
2. **Entry-gate overlay** (`static/index.html`) — a rich, post-login overlay on
   the main archive page: a live `KD.FX` demoscene intro, a `TERMS` ASCII block
   with a rules list, an archive inventory blurb, an "I accept" checkbox, and an
   `[ ENTER ]` button. Gated by `localStorage` (`kd:prefs:terms-accepted`) so
   returning visitors skip it.

The entry-gate is effectively a *second* gate that fires after login. We want to
collapse the experience into a single rich signin page that appears **before**
auth, and delete the post-login overlay entirely.

## Goal

Replace the basic `/login` page with a full-screen demoscene signin screen that
carries over the entry-gate's best parts, and remove the entry-gate overlay from
the main archive page.

Carried over to the new signin page:

- The live `KD.FX` demoscene FX intro (header / canvas / ticker / footer).
- The `TERMS` ASCII header + rules list (no ads, basic analytics, google login,
  no uptime guarantee, no monetization).
- The archive inventory blurb (the long descriptive paragraph).

Explicitly **dropped**:

- The "I accept the terms" checkbox gate. Sign-in is the action; the rules are
  shown as informational, not a blocking checkbox.
- The whole post-login entry-gate overlay and its `localStorage` persistence.

## Flow

```
unauthenticated request
   -> middleware: 302 /login?redirect=<safe original URL>
   -> /login renders demoscene signin page (KD.FX + terms + inventory + Google button)
   -> user clicks "Sign in with Google"
   -> /auth/google/start?redirect=... (redirect re-validated server-side)
   -> Google consent -> /auth/google/callback -> session cookie set
   -> redirect back into index.html (archive), NO overlay
```

After auth the archive page loads directly. There is no second gate.

## Architecture

### 1. Extract the FX engine to `static/fx.js`

The demoscene FX engine — `window.createFxInstance = function(cfg){…}`, roughly
`static/index.html:4249–6933`, ~2685 lines, 50+ scenes — is currently inlined in
`index.html`. It is shared by two consumers:

- The entry-gate boot (`index.html:6935–6943`, canvas `#fx-canvas`).
- The standalone Demo window (`index.html:10635–10647`, canvas `#demo-fx-canvas`).

Move the engine verbatim into a new `static/fx.js`:

- `index.html` replaces the inline `createFxInstance` definition with
  `<script src="/fx.js"></script>`.
- The new pre-auth login page also loads `/fx.js`.
- Add `"/fx.js": true` to `publicPaths` (`auth.go`) so the unauthenticated login
  page can fetch it without a 302. `publicPaths` is an exact-match map and the
  static file server (`mux.Handle("/", staticHandler(...))`) serves files from
  `./static`, so a `static/fx.js` file at path `/fx.js` is served directly.

Single source of truth: future FX edits touch only `fx.js`.

**Risk:** the extraction must be verbatim. The demo window (`#demo-fx-canvas`)
must keep working unchanged. Verify by opening the Demo window after the change.

### 2. New `login.html` (go:embed + html/template)

- New file `login.html` at the repo root.
- `auth.go` embeds it: `//go:embed login.html` into a `string`/`[]byte` var,
  parsed once at init with `html/template`.
- `loginPage(redirect)` executes the template with a single field,
  `StartHref` = `/auth/google/start?redirect=<url.QueryEscape(redirect)>`.
  `redirect` is already validated by the caller (`safeRedirect`); `html/template`
  auto-escaping handles the attribute context, replacing today's manual
  `html.EscapeString`.

Page content (self-contained; pre-auth, so no auth-gated assets except the
publicPath `/fx.js`):

- `fx-wrap`: `fx-header` (KD/HOMEBREW tag, scene label), `<pre class="fx-canvas
  fx-kdfx" id="fx-canvas">`, `fx-ticker`, `fx-footer` (channel + timer) — same
  structure the entry-gate used.
- `<h2>` title: "KD's Homebrew Digital Archive".
- Terms block: `TERMS` ASCII header + the rules `<ul>` (informational, no
  checkbox).
- Archive inventory blurb (the long descriptive paragraph; may keep it
  `sr-only` for SEO/screen-readers or render it visibly — render visibly here
  since it's now part of the signin content).
- `Sign in with Google` anchor → `{{.StartHref}}`.
- `<script src="/fx.js"></script>` + an inline boot call to
  `window.createFxInstance({ canvas: document.getElementById('fx-canvas'), … })`
  driving the `KD.FX` scene.
- Inlined CSS: the `.fx-*` subset the page needs plus the signin layout styling.
  The login page carries its own copy of this CSS (it does not share
  `index.html`'s stylesheet).

### 3. Remove the entry-gate from `index.html`

Delete (entry-gate-specific only):

- CSS: `.entry-gate*`, `.entry-gate-glass*`, `.entry-gate-title`,
  `.entry-terms*`, `.entry-check*`, `.entry-rules`, `.entry-gate-btn`,
  `.entry-inventory`, `.entry-cursor`, `body.entry-gate-locked`,
  `body.entry-gate-accepted`, and the `body.no-transparency .entry-gate*` rules.
- The `entry-gate-locked` class on `<body>` (`index.html:4118`).
- The early `kd:prefs:terms-accepted` localStorage script (~`4132–4139`).
- The entry-gate HTML markup (`4168–4205`).
- The accept-checkbox IIFE (`4207`+).
- The entry-gate FX boot (`6935–6943`).

Keep (shared / unrelated):

- `createFxInstance` (now in `fx.js`).
- `.fx-canvas` CSS (`3556–3792`) — the Demo window's `#demo-fx-canvas` uses it.
- The standalone Demo window markup + boot (`7180`, `10635–10647`) and the
  demoscene cat-state content — unrelated to the entry-gate.

### 4. Server (`auth.go`)

- `loginPage` switches from string concatenation to executing the embedded
  template.
- `handleLogin` is otherwise unchanged: still derives the validated redirect and
  writes the page.
- The open-redirect defense is unchanged: `/auth/google/start` re-validates the
  redirect via `safeRedirect`, so even a forwarded raw value is neutralized
  server-side.

## Testing

`auth_test.go` (extend existing):

- `GET /login` returns 200 and the body contains the FX markup
  (`id="fx-canvas"`), the Google sign-in link
  (`/auth/google/start?redirect=`), and the terms rules text.
- The `redirect` query value is properly escaped in the rendered link
  (existing open-redirect / escaping assertions carry over to the template
  output).
- `GET /fx.js` is reachable **without** a session cookie (publicPath), returns
  200 and JavaScript content.
- A gated path (e.g. `/`) still 302s to `/login` when unauthenticated.

Manual:

- Load `/login` unauthenticated → demoscene animation runs, terms + inventory
  visible, Google button present.
- Sign in with Google → land in the archive (`index.html`) with **no** overlay.
- Open the Demo window in the archive → FX still animates (extraction regression
  check).

## Out of Scope

- No change to the OAuth handshake, session/cookie logic, or `safeRedirect`.
- No change to the Demo window behavior beyond sourcing FX from `fx.js`.
- No redesign of the archive page itself.
