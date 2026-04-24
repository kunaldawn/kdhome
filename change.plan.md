# Archive Collections Change Plan

Updating the site from **3 sub-archives** to **7 sub-archives**. The old
`archive.kunaldawn.com` ("Data Archive") is broken and must be retired; its
contents are split across four dedicated sub-archives, and two new sub-archives
are added. The PDF archive hostname is also shortened.

---

## 1. Archive inventory (old → new)

| Status  | Old                                              | New                                              | Notes                                                       |
| ------- | ------------------------------------------------ | ------------------------------------------------ | ----------------------------------------------------------- |
| keep    | `pdfarchive.kunaldawn.com` — PDF Archive         | `pdf.kunaldawn.com` — PDF Archive                | Hostname change. Content unchanged.                         |
| keep    | `wiki.kunaldawn.com` — Wiki Archive              | `wiki.kunaldawn.com` — Wiki Archive              | Unchanged. Content unchanged.                               |
| remove  | `archive.kunaldawn.com` — Data Archive (BROKEN)  | — (split into 4 below)                           | Drop all references, links, and cards.                      |
| new     | —                                                | `iso.kunaldawn.com` — CD/DVD Archive             | Vintage CD/DVD, warez, abandonware (was part of Data).      |
| new     | —                                                | `os.kunaldawn.com` — OS Archive                  | OS images & driver collections (was part of Data).          |
| new     | —                                                | `chiptune.kunaldawn.com` — Chiptune Archive      | Chiptune & keygen music collection (was part of Data).      |
| new     | —                                                | `tube.kunaldawn.com` — Tube Archive              | YouTube content archive (was "curated video" in Data).      |
| new     | —                                                | `audio.kunaldawn.com` — Audiobook Archive        | Audiobook collection (new; not present before).             |

Final list in display order (to use consistently everywhere):

1. **Wiki Archive** — `wiki.kunaldawn.com`
2. **PDF Archive** — `pdf.kunaldawn.com`
3. **OS Archive** — `os.kunaldawn.com`
4. **CD/DVD Archive** — `iso.kunaldawn.com`
5. **Chiptune Archive** — `chiptune.kunaldawn.com`
6. **Tube Archive** — `tube.kunaldawn.com`
7. **Audiobook Archive** — `audio.kunaldawn.com`

---

## 2. Files that need updating

Summary (details in §3):

| File                                  | Kind of change                                                                                     |
| ------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `static/index.html`                   | 8 distinct regions: meta tags, JSON-LD, sr-only inventory, terminal COLLECTIONS, Start Menu, Vault panel cards, console/cat tips, browser `homeDoc()` start page. |
| `static/llms.txt`                     | "three sub-archives" count + the entire `## Sub-Archives` section.                                 |
| `static/site.webmanifest`             | `description` field wording.                                                                       |
| `generate_og.py`                      | Archives list, stats labels, hostnames. OG image must be regenerated afterwards.                   |
| `static/og-image.png`                 | Regenerated product of `generate_og.py`.                                                           |
| `.claude/settings.local.json`         | Two stale curl allowlist entries (`archive.kunaldawn.com`, `pdfarchive.kunaldawn.com`).            |

Files confirmed **not affected**:

- `main.go`, `Dockerfile`, `docker-compose.yml`, `entrypoint.sh` — no archive references.
- `static/robots.txt`, `static/sitemap.xml` — only point at the root origin.
- `static/favicon*.png`, `static/apple-touch-icon.png`, `static/android-chrome-*.png`, `static/infra.webp` — no archive text.

---

## 3. File-by-file change list

### 3.1 `static/index.html` (8 regions)

**a. Meta tags (head)**

- **L58** — `<meta name="description">`: rewrite the tail "500+ GB of OS images, vintage software, and chiptunes" to reflect 7 archives. Suggested replacement phrase: *"OS images, vintage CD/DVD archives, chiptunes, YouTube mirrors, and audiobooks"*.
- **L59** — `<meta name="keywords">`: add `audiobook archive`, `chiptune archive`, `OS archive`, `YouTube archive` if we want them indexed. Optional.
- **L67** — `og:description`: same phrasing refresh as L58.
- **L72** — `og:image:alt`: "500+ GB archives, 12 TB total" — update to "7 sub-archives, 12 TB total" (or whatever new headline figure we settle on).
- **L79** — `twitter:description`: same refresh as L58/L67.
- **L81** — `twitter:image:alt`: refresh same as L72.
- **L96** — JSON-LD `description`: refresh same as L58.
- **L106** — JSON-LD `CollectionPage` `description`: refresh same as L58.
- **L135** — `<title>`: currently "12 TB Preserved on a Home-grown Homelab" — no change required unless we want to mention "7 archives".

**b. JSON-LD `hasPart` (L112–L131)**

Replace the 3-item `hasPart` array with a 7-item array in the order fixed in §1. Each entry is `{"@type":"WebSite","name":<Name>,"url":<URL>,"description":<one-liner>}`. PDF URL changes from `https://pdfarchive.kunaldawn.com/` → `https://pdf.kunaldawn.com/`. Drop the `archive.kunaldawn.com` entry entirely.

**c. sr-only inventory paragraph (L3254)**

Rewrite the sentence *"500 GB of operating system images, vintage software CD/DVD archives, chiptune collections, and curated video archives."* to enumerate the 7 archives (OS, CD/DVD, chiptune, YouTube, audiobook) with whatever total-size figure we now use. Retain the homelab/solar boilerplate.

**d. Terminal "COLLECTIONS" section (L3502–L3515)**

Currently 4 blocks (`wikis`, `pdfs`, `data`, `retro`). Restructure to one block per sub-archive (keep `retro` as the closing flourish or drop it — recommend keeping it as a thematic tagline line rather than a separate archive).

- **L3504–3505** `wikis` — keep, unchanged.
- **L3507–3508** `pdfs` — keep, unchanged.
- **L3510–3511** `data` — REPLACE with 5 new blocks:
  - `os -- <size>` / `operating system images . drivers . bootable media`
  - `iso -- <size>` / `vintage cd/dvd . warez . abandonware`
  - `chiptune -- <size>` / `tracker modules . keygen music . scene audio`
  - `tube -- <size>` / `youtube mirrors . channels . playlists`
  - `audio -- <size>` / `audiobook collection`
- **L3513–3514** `retro` — keep (it's an ethos line, not an archive entry).

Sizes are TBD — see §4.

**e. Start Menu Quick Access (L4762–L4776)**

Replace the 3 `<div class="menu-item">` entries with 7, one per sub-archive, each wired to `openBrowserWindow(<url>, <label>)`. PDF URL changes to `pdf.kunaldawn.com`. SVG icons can be reused/mixed — suggest distinct glyphs per archive (globe for wiki, doc for pdf, disc for OS & iso, music-note for chiptune, play-triangle for tube, headphones for audio). The Start Menu column height will grow from 3 items to 7; visually verify the menu still fits within viewport at small sizes (see §5).

**f. Vault panel "Featured Archives" cards (L4945–L4970)**

- **L4949** heading — keep "Featured Archives"; optionally update the tagline.
- **L4954–L4969** — replace 3 `<a class="card">` with 7, one per sub-archive. Each card has `<h4>` (archive name) and `<div class="muted">` (short description). Update PDF href from `pdfarchive.kunaldawn.com` → `pdf.kunaldawn.com`. Drop the Data Archive card.
- CSS (`.grid-cards` at L441) uses `repeat(auto-fit, minmax(180px, 1fr))`, so 7 cards will reflow automatically — **no CSS change required**, though we should visually confirm on both wide and narrow viewports (mobile breakpoints at L1210 and L1390).

**g. `homeDoc()` browser start-page sites list (L7030–L7035)**

Replace the 3-entry `sites` array with 7 entries matching the card set in 3.1.f. This is the Cloudflare-style "about:home" for the internal browser window. Update PDF URL. Drop Data Archive.

**h. Cat tips & console logs (L5102–L5121, L5439–L5443)**

Minor, optional:
- **L5432** cat "surfing" tips — fine as-is.
- **L5439–L5443** cat "tips" list — references *"Wiki Archive"* and *"PDF archive has rare zines!"* — still valid; optional to add tips that mention chiptune/audio.
- **L5098** console banner `HOMEBREW DIGITAL ARCHIVE · v5.0.0` — bump version (e.g. `v6.0.0`) to match the collection reshuffle (optional).
- **L5160** `vaultStats` `total_size: '12TB (hot) + 12TB cold'` — no change unless hardware totals changed.

### 3.2 `static/llms.txt`

- **L20** "The project spans **three** sub-archives totalling over 12 TB" → **"seven sub-archives"**.
- **L24–L52** the entire `## Sub-Archives` section:
  - Keep the **Wiki Archive** block (L26–L35) as-is.
  - Replace the **Data Archive** block (L37–L42) with five new blocks: OS Archive, CD/DVD Archive, Chiptune Archive, Tube Archive, Audiobook Archive (each with hostname, one-paragraph description, bullet highlights).
  - Update **PDF Archive** heading on L44: `pdfarchive.kunaldawn.com` → `pdf.kunaldawn.com`.
- Overall tone/formatting must stay consistent with the existing entries.

### 3.3 `static/site.webmanifest`

- **L4** `description`: current text mentions *"500+ GB of OS images and chiptunes"*. Rewrite to reflect 7 archives (same phrasing as the HTML meta description). Keep under ~300 chars for PWA tooling.

### 3.4 `generate_og.py`

The OG image has a fixed 1200×630 canvas with a middle-column archive list (left) and a 4-box stats column (right). Current layout comfortably fits 3 archive rows; 7 rows will not fit at current spacing.

- **L180–L187** — the `archives` list (3 entries at 46px per row). Options:
  1. **Condense**: reduce to the 3–4 headline archives on the OG card (e.g. Wiki, PDF, OS, Chiptune) and mention "+3 more" on a final row. Keeps the design intact.
  2. **Shrink + expand**: reduce font (`url_font=bold(13)`, `desc_font=mono(12)`) and row pitch to fit all 7 lines. Visually denser.
  - Recommend **option 1** for design clarity. Decision needed.
- **L183–L184** — must drop `archive.kunaldawn.com`.
- **L185** — `pdfarchive.kunaldawn.com` → `pdf.kunaldawn.com`.
- **L194–L200** — stats column: 4 values (`36`, `23,000+`, `500+ GB`, `12 TB`). The `500+ GB — Data Archives` tile no longer maps to anything. Replace with either `7 — Sub-Archives` or a new headline figure (total size of the 5 non-wiki-non-pdf archives).
- **L97, L325** — `vault@kd:~/archive` titlebar text — unchanged.

After editing the script, **regenerate** `static/og-image.png`:
```
python3 generate_og.py
```

### 3.5 `.claude/settings.local.json`

- **L6** `archive.kunaldawn.com` curl allow — delete (host gone).
- **L7** `pdfarchive.kunaldawn.com` curl allow — update to `pdf.kunaldawn.com`, or delete if no longer exercised.
- Optional: add allow entries for the 5 new hostnames (`os.`, `iso.`, `chiptune.`, `tube.`, `audio.kunaldawn.com`) so future verification curls don't prompt.

---

## 4. Open questions / content inputs needed from user

Before making edits, we need authoritative copy for:

1. **Per-archive descriptions** — one-liner for each of the 7 (used in JSON-LD, Vault cards, `homeDoc()`, llms.txt, Start Menu). The existing Wiki and PDF copy can stay.
2. **Per-archive sizes** (GB) — for the terminal COLLECTIONS section, llms.txt bullet points, and any "X+ GB" totals.
3. **New aggregate figures** — the site currently advertises "12 TB" preserved, "500+ GB" of digital artifacts, "23,000+ PDFs", "36 wikis". Which of these remain accurate after the split, and what replaces "500+ GB of digital artifacts"?
4. **OG image layout preference** — option 1 (condense to headline archives) vs option 2 (shrink font to fit all 7). See §3.4.
5. **Whether to bump visible version strings** — `v5.0.0` in the console banner (L5098), any version string elsewhere.

---

## 5. Post-change verification

- `go build ./...` succeeds (no code changes expected, but sanity check).
- `python3 generate_og.py` succeeds and emits new `og-image.png` (~80–120 KB, 1200×630).
- Run server: `PORT=8765 go run .` — then load `http://localhost:8765/`:
  - Start Menu Quick Access shows 7 items and fits within viewport at mobile width.
  - Vault panel "Featured Archives" grid reflows cleanly at 320, 768, 1440 px widths.
  - Each card's `openBrowserWindow(...)` opens an iframe aimed at the correct new hostname.
  - Browser window's `about:home` start page shows all 7 cards.
  - View source: confirm JSON-LD `hasPart` lists 7 items, no stale `archive.kunaldawn.com` references.
- `grep -rn "archive.kunaldawn.com\|pdfarchive.kunaldawn.com" static/ *.py *.go *.json` — should return zero results in site content (the `.claude/settings.local.json` entry, if kept for history, is the only allowed residue).
- Validate `/llms.txt` renders correctly in-browser.
- Validate `/site.webmanifest` still parses as JSON (PWA installability check).
- Post-deploy: verify each of the 7 hostnames resolves and serves 200 OK (the site itself doesn't depend on this, but the links do).

---

## 6. Suggested commit structure

Two commits keep review easy:

1. **content**: update index.html, llms.txt, site.webmanifest, JSON-LD — all 7-archive copy.
2. **og**: update `generate_og.py` + regenerated `og-image.png`.

Plus an optional third for `.claude/settings.local.json` if we want to clean up stale permissions.
