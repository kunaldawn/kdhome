# Human / Bot Visit Counter Split

**Date:** 2026-05-26
**Owner:** kunaldawn
**Status:** Draft

## Background

The production visit counter at kunaldawn.com reads far higher than real
human traffic because **every** GET to `/` is counted, including crawlers.

`staticHandler` in `main.go` calls `recordVisit()` on every GET to `/` or
`/index.html` (including `304 Not Modified`, excluding `HEAD`). There is **no**
request inspection — no User-Agent check, no IP, no dedup — so each crawler hit
is `+1`. The code comment there states this is intentional: *"cache, bfcache,
no-JS clients, and bots are all reflected."* The earlier counter spec
(`2026-05-09-server-side-counters-design.md`) listed bot filtering as an
explicit non-goal; **this spec supersedes that decision.**

The inflation source is overwhelmingly **self-declaring crawlers**: AI/research
bots (GPTBot, ClaudeBot, CCBot, PerplexityBot, Google-Extended, Bytespider…),
search bots (Googlebot, bingbot), SEO bots (AhrefsBot, SemrushBot), uptime
monitors, and link-preview fetchers. These set honest User-Agent strings (they
*want* to be identified for robots.txt), so a server-side User-Agent check
catches the large majority of the inflation cheaply.

### Constraints discovered

- Storage is a single-row aggregate `visit_count(id INTEGER PK CHECK(id=1), n)`.
  In-memory it is cached in the `visitCount` atomic and exposed via
  `GET /api/visit` → `{"count": n}`.
- The legacy `visits` table holds 232 timestamped rows but **no User-Agent**, so
  past traffic **cannot** be retroactively reclassified. Any split must begin at
  deploy.
- Frontend: `#visit-count` in the system tray (`static/index.html:7247`) renders
  `"<n> visits"`, fetched once on load. It dispatches a `kd-visit-count`
  CustomEvent whose `detail.count` drives the desktop cat's milestone
  celebration (`onVisitCount`, `static/index.html:9208`, tiers
  100/500/1k/5k/10k/100k).

## Goals

1. Classify each counted request as **human** or **bot** by User-Agent,
   server-side, and accumulate two separate counters.
2. Reset both counters to **0** on deploy (the existing inflated total cannot be
   split honestly, so it is discarded).
3. Surface the split in the system tray as `248 human · 1.2k bot`, with the full
   numbers in the tooltip.
4. Drive the cat's milestone celebration off the **human** count.

## Non-goals

- **Catching UA-spoofing stealth scrapers.** This is a deny-list on User-Agent;
  a bot that forges a real browser UA and runs no JS is counted as human. Rare
  on a personal site, and out of scope (no JS beacon, no behavioural signals).
- **Per-crawler / per-category breakdown.** Two buckets only (human, bot).
- **IP capture, dedup, or unique-visitor counting.** Counts remain monotonic
  totals of GET-to-`/` events, exactly as today.
- **Reversibility of the reset.** The reset is one-time and discards the old
  total by design.

## Design

### Detection — `isBot`

One precompiled, case-insensitive `*regexp.Regexp` in `main.go`, plus an
empty/missing User-Agent short-circuit:

```go
// botUA matches self-declaring crawlers, monitors, HTTP libraries, and
// link-preview fetchers. It is a deny-list: real browser User-Agents carry
// none of these tokens and fall through to "human". Grouped for maintenance;
// order within the alternation is irrelevant.
var botUA = regexp.MustCompile(`(?i)` + strings.Join([]string{
    // generic
    `bot`, `crawl`, `spider`, `slurp`, `scrape`,
    // AI / research
    `gptbot`, `oai-searchbot`, `chatgpt`, `claudebot`, `anthropic-ai`,
    `ccbot`, `perplexitybot`, `google-extended`, `bytespider`, `amazonbot`,
    `applebot`, `diffbot`, `cohere-ai`, `imagesiftbot`, `omgili`,
    // search
    `googlebot`, `bingbot`, `duckduckbot`, `baiduspider`, `yandexbot`,
    // SEO
    `ahrefsbot`, `semrushbot`, `mj12bot`, `dotbot`, `dataforseo`,
    // monitors
    `uptimerobot`, `pingdom`, `statuscake`, `site24x7`,
    // HTTP libs / headless
    `curl`, `wget`, `libwww`, `python-requests`, `go-http-client`, `okhttp`,
    `java/`, `node-fetch`, `axios`, `headlesschrome`, `phantomjs`,
    // link preview
    `facebookexternalhit`, `twitterbot`, `slackbot`, `discordbot`,
    // NOTE: "whatsapp" is deliberately excluded — WhatsApp's in-app browser
    // (a real human) shares the "WhatsApp/" UA token with its link-unfurl
    // bot, so matching it would undercount real visitors.
    `telegrambot`, `linkedinbot`, `embedly`, `redditbot`,
}, `|`))

func isBot(ua string) bool {
    if strings.TrimSpace(ua) == "" {
        return true // no UA → treat as bot/script
    }
    return botUA.MatchString(ua)
}
```

Note the generic tokens (`bot`, `crawl`, `spider`, `slurp`) already catch the
long tail (`bot` alone covers `Googlebot`, `bingbot`, `AhrefsBot`, etc.); the
named entries are kept for documentation and to catch the few that lack a
generic token (e.g. `Bytespider`, `curl`, `python-requests`). The match runs
once per `/` GET — a single regexp match, microseconds, negligible on the Pi.

### Storage & in-memory state

Extend the existing single-row table rather than adding a new one:

```sql
ALTER TABLE visit_count ADD COLUMN human INTEGER NOT NULL DEFAULT 0;
ALTER TABLE visit_count ADD COLUMN bot   INTEGER NOT NULL DEFAULT 0;
```

The old `n` column is retired (left in place, set to 0, no longer read or
written). In-memory, the `visitCount` atomic is replaced by two:

```go
var (
    visitDB    *sql.DB
    humanCount int64 // atomic
    botCount   int64 // atomic
)
```

Both are hydrated from `human`/`bot` at startup in `initVisitDB`.

### Reset migration (one-time, idempotent)

Guarded by `PRAGMA user_version` so it runs exactly once and restarts never
re-zero accumulated counts:

```
on initVisitDB, after the ALTER ... ADD COLUMN statements (which are themselves
idempotent-guarded — see below):

  read PRAGMA user_version
  if user_version < 1:
      UPDATE visit_count SET n = 0, human = 0, bot = 0 WHERE id = 1
      PRAGMA user_version = 1
```

`ADD COLUMN` errors if the column already exists, so each `ALTER` is wrapped to
ignore the "duplicate column name" error (run unconditionally; the error on a
second boot is benign). The `user_version` gate is what makes the **reset**
one-time — the columns may already exist on a fresh `CREATE TABLE` in future
versions, but the zeroing only ever happens at the `0 → 1` transition.

### Recording

`recordVisit` takes the classification; the call site computes it:

```go
// staticHandler, replacing the current recordVisit() call:
if r.Method == http.MethodGet &&
    (r.URL.Path == "/" || r.URL.Path == "/index.html") {
    recordVisit(isBot(r.UserAgent()))
}

func recordVisit(bot bool) {
    if visitDB == nil {
        return
    }
    col, ctr := "human", &humanCount
    if bot {
        col, ctr = "bot", &botCount
    }
    go func() {
        if _, err := visitDB.Exec(
            `UPDATE visit_count SET `+col+` = `+col+` + 1 WHERE id = 1`,
        ); err != nil {
            log.Printf("[VISITS] update error (%s): %v", col, err)
            return
        }
        atomic.AddInt64(ctr, 1)
    }()
}
```

(`col` is from a fixed two-value set, never user input, so string-building the
SQL is safe here.) The trigger is unchanged: GET to `/`, includes `304`,
excludes `HEAD`.

### API

`GET /api/visit` returns both buckets (the `count` field is dropped; the only
consumer is our own frontend):

```json
{"humans": 248, "bots": 1203}
```

### Frontend (`static/index.html`)

- **Tray** (`#visit-count`): render `${abbrev(humans)} human · ${abbrev(bots)} bot`
  where `abbrev(n)` is `n < 1000 ? n.toLocaleString() : <1.2k / 3.4M form>`.
  So `248 human · 1.2k bot`. The `title` attribute carries the full picture:
  `"248 humans · 1,203 bots filtered"`.
- **Event**: the `kd-visit-count` CustomEvent detail becomes `{humans, bots}`.
- **Cat milestones** (`onVisitCount`): key off `humans` (real visitors). Tiers
  unchanged (100/500/1k/5k/10k/100k). Label wording changes "N visits!" →
  "N visitors!" so the celebration stays accurate against the human number.

## Data flow

```
GET / (any client)
  └─ staticHandler
       ├─ isBot(User-Agent)?
       │     ├─ yes → recordVisit(true)  → UPDATE bot,   botCount++
       │     └─ no  → recordVisit(false) → UPDATE human, humanCount++
       └─ serve index (200 / 304 as before)

page JS  ── fetch /api/visit ──►  {"humans":H,"bots":B}
   ├─ tray: "H human · B bot"  (title: full numbers)
   └─ dispatch kd-visit-count {humans:H, bots:B}
                                   └─ cat.onVisitCount(H) → milestone
```

## Error handling

- `visitDB == nil` (DB open failed): `recordVisit` is a no-op, as today.
- DB `UPDATE` error: logged, atomic not bumped (counter is eventually
  consistent with disk; a dropped increment is acceptable for a vanity counter).
- `/api/visit` fetch failure on the client: tray simply shows nothing new
  (`-- visits` placeholder remains), matching current behaviour.
- `ALTER TABLE` on an already-migrated DB: duplicate-column error is caught and
  ignored.

## Testing / verification

1. **Build** to `/tmp/kdhome-verify/kdhome-bin` from the repo root (staticDir is
   `./static`); run with `PORT=8099 DATA_DIR=/tmp/kdhome-verify`.
2. **Classification** — curl `/` with representative User-Agents and assert the
   right bucket moves in `/api/visit`:
   - real Chrome UA → `humans++`
   - `GPTBot/1.1`, `ClaudeBot`, `AhrefsBot`, `UptimeRobot`, `curl/8.x`, and an
     **empty** UA → `bots++`
3. **Reset / idempotency** — start against a DB whose `visit_count.n` is
   non-zero and `user_version` is 0; confirm all three columns become 0 and
   `user_version` becomes 1. Restart; confirm counts are **not** re-zeroed and
   continue to accumulate.
4. **Frontend** — Playwright load: tray renders `N human · N bot`, the `title`
   tooltip shows full comma-formatted numbers, and the `kd-visit-count` event
   carries `{humans, bots}`.

## Files touched

- `main.go` — `isBot`/`botUA`, `human`/`bot` columns + migration, `humanCount`/
  `botCount` atomics, `recordVisit(bool)`, `visitHandler` JSON shape.
- `static/index.html` — `#visit-count` fetch + render + `abbrev`, event detail,
  `onVisitCount` (key off humans, label wording).
