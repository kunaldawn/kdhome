    /* ═══ Demoscene ASCII intro (bb/aalib-inspired) ═══ */
    window.createFxInstance = function(cfg) {
      cfg = cfg || {};
      const canvas = cfg.canvas || document.getElementById('fx-canvas');
      if (!canvas) return { stop: function(){} };
      const sceneLabel = cfg.sceneLabel || document.querySelector('.fx-scene');
      const timerLabel = cfg.timerLabel || document.getElementById('fx-timer');
      const gate = cfg.gate || null;
      // Vertical position of the KD.FX bumper title, as a fraction of ROWS.
      // Default 0.5 (center). Consumers that overlay a centered panel (e.g. the
      // login card) can bias it toward the top so the title isn't hidden.
      const bannerRowFrac = (typeof cfg.bannerRowFrac === 'number') ? cfg.bannerRowFrac : 0.5;

      const isMobile = window.matchMedia('(max-width: 520px)').matches;
      const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

      let COLS = isMobile ? 34 : 46;
      let ROWS = isMobile ? 17 : 22;

      // Cell aspect ratio (height / width). Monospace glyphs are taller than
      // wide; multiply y-deltas by this so circular math reads circular.
      // Computed from canvas font metrics on first paint, with a static fallback.
      let CHAR_ASPECT = isMobile ? 1.92 : 2.0;

      const RAMP = " .'`,:;!i~-+=<>*?/\\|()][}{1trlJCYXZO0Qkbdpwm#W%@&$";
      const RAMP_LEN = RAMP.length;
      const SPACE = 32;

      let buf = new Uint8Array(COLS * ROWS);
      let prevBuf = new Uint8Array(COLS * ROWS);
      let hasPrev = false;
      let rowStrs = new Array(ROWS);
      let geometryRev = 0;
      // Per-effect geometry-rev cache. Effects with COLS/ROWS-sized state
      // increment this when they reallocate their helpers.
      const geomRev = {
        fire: 0, life: 0, cell: 0, sphere: 0, voxel: 0, torus: 0
      };

      function clearBuf() { buf.fill(SPACE); }

      /* ─── Cracktro ticker content (rolls from bottom to top) ───
         Width-aware: all structural glyphs are ASCII so every line has
         identical monospace advance and borders span the full pane.
         Desktop W=56 cols (~428px of 436px content box); mobile W=42
         cols (~270px of 280px). Every line is padded to W so the right
         edge of every row lines up with the right edge of every box.
         Style is intentionally late-90s warez-scene NFO: chunky banner,
         dotted-leader info block, framed section breaks, sites list,
         greetz, signature footer. */
      const TICKER_LINES = (function() {
        const W = isMobile ? 42 : 56;
        const R = (ch, n) => ch.repeat(Math.max(0, n | 0));
        const clip = s => (s.length > W ? s.slice(0, W) : s);
        const padR = s => (s.length >= W ? s.slice(0, W) : s + R(' ', W - s.length));
        const center = s => {
          if (s.length >= W) return s.slice(0, W);
          const gap = W - s.length, l = gap >> 1;
          return R(' ', l) + s + R(' ', gap - l);
        };

        // Full-width double-rule banner top/bottom:  +======...======+
        const hr2 = () => '+' + R('=', W - 2) + '+';
        // Mid rule inside a box:  +------...------+
        const hr1 = () => '+' + R('-', W - 2) + '+';
        // Dashed-with-space full-width line:   - - - - - - - - - - -
        const divLine = () => clip(padR(R('- ', W >> 1)));
        // Two-tone NFO break:  -=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-
        const divZag = () => {
          const half = W >> 1;
          return clip(padR(R('-=', half)));
        };

        // "| <centered inner> |" padded to exactly W.
        const rowC = inner => {
          const inside = W - 4;
          const t = inner.length > inside ? inner.slice(0, inside) : inner;
          const gap = inside - t.length, l = gap >> 1;
          return '| ' + R(' ', l) + t + R(' ', gap - l) + ' |';
        };
        // "| <left inner>         |" padded to exactly W.
        const rowL = inner => {
          const inside = W - 4;
          const t = inner.length > inside ? inner.slice(0, inside) : inner;
          return '| ' + t + R(' ', inside - t.length) + ' |';
        };

        // NFO-style dotted-leader inside a box:
        //   "|  LABEL ........... : VALUE                        |"
        const infoRow = (label, value) => {
          const inside = W - 4;
          const lead = ' ' + label + ' ';
          const tail = ' : ' + value;
          const dots = inside - lead.length - tail.length;
          if (dots < 1) return rowL(label + tail);
          return '| ' + lead + R('.', dots) + tail + ' |';
        };
        // Continuation row: same box, but the lead is spaces (no dotted
        // leader) so the eye reads "still under the previous label".
        //   "|                            : VALUE                |"
        const infoRowCont = (value) => {
          const inside = W - 4;
          const tail = ' : ' + value;
          const fill = inside - tail.length;
          return '| ' + R(' ', fill) + tail + ' |';
        };

        // Word-wrap prose. Body is indented 2 chars left.
        const wrap = (text, maxLen) => {
          const words = text.split(/\s+/).filter(Boolean);
          const out = [];
          let cur = '';
          for (const w of words) {
            const next = cur ? cur + ' ' + w : w;
            if (next.length > maxLen) {
              if (cur) out.push(cur);
              cur = w;
            } else {
              cur = next;
            }
          }
          if (cur) out.push(cur);
          return out;
        };
        const prose = (cls, text) => {
          wrap(text, W - 4).forEach(l => L.push([cls, padR('  ' + l)]));
        };

        const L = [];
        const add = (cls, txt) => L.push([cls, padR(txt)]);
        const addRaw = (cls, txt) => L.push([cls, txt]);
        const blank = () => L.push(['', '']);

        // NFO-style section break:
        //
        //   - - - - - - - - - - - - - - - - - - - - - - - - - -
        //
        //                  ..::  T I T L E  ::..
        //
        //   - - - - - - - - - - - - - - - - - - - - - - - - - -
        //
        const section = (title) => {
          blank();
          L.push(['tk-dim', divLine()]);
          blank();
          L.push(['tk-hot', center('..::  ' + title + '  ::..')]);
          blank();
          L.push(['tk-dim', divLine()]);
          blank();
        };

        // ─── TITLE BANNER ───────────────────────────────
        add('tk-ok',  hr2());
        add('tk-ok',  rowC('KD\'s HOMEBREW DIGITAL ARCHIVE'));
        add('tk-dim', rowC('-=[  proudly presents  ]=-'));
        add('tk-dim', rowC('rel.0x01  ::  online  ::  AD 2026'));
        add('tk-ok',  hr2());

        // ─── RELEASE INFO TABLE ─────────────────────────
        blank();
        add('tk-ok', hr2());
        addRaw('', infoRow('ARCHiViST',  'KUNAL DAWN'));
        addRaw('', infoRow('PROTECTiON', 'NONE / OPEN'));
        addRaw('', infoRow('FORMAT',     'WEB / HTTPS'));
        addRaw('', infoRow('SiZE',       '~12.5 TB'));
        addRaw('', infoRow('DATE',       '2026-05-12'));
        addRaw('', infoRow('RATiNG',     '[X] PRESERVE  [ ] PROFiT'));
        add('tk-ok', hr2());

        // ─── ABOUT ──────────────────────────────────────
        section('A B O U T');
        prose('', 'A home-grown mirror of the public internet.');
        prose('', 'A shelf in a house, not a rack in a data centre.');
        blank();
        prose('', 'This is where abandonware lives -- retro code, dead magazines, dusty journals, and manuals for chips that nobody makes any more.');
        blank();
        prose('', 'Useless to most. Gold to the few who go looking.');

        // ─── HARDWARE ───────────────────────────────────
        section('H A R D W A R E');
        add('tk-ok', hr2());
        addRaw('',       infoRow('compute',     '2x RPi4 8G'));
        addRaw('',       infoRowCont(           '1x N150 12G'));
        addRaw('',       infoRow('network',     '5-port switch'));
        addRaw('',       infoRow('power',       '65W USB-C PSU'));
        addRaw('',       infoRow('hot storage', '4x 2TB HDD'));
        addRaw('',       infoRowCont(           '2x 2TB SSD'));
        addRaw('tk-warn',infoRow('cold backup', '1x 12TB HDD'));
        add('tk-ok', hr1());
        addRaw('tk-warn',infoRow('total draw',  '~30 W'));
        addRaw('tk-warn',infoRow('design goal', 'off-grid solar'));
        add('tk-ok', hr2());

        // ─── KIT (cat + rack) ───────────────────────────
        section('K D   K I T');
        if (!isMobile) {
          // Desktop: cat left (col 0-13), rack right (col 14-55).
          const CAT_COL = 14;
          const CAT = [
            '   /\\_/\\    ',
            '  ( o.o )    ',
            '   > ^ <     ',
          ];
          const rackW = W - CAT_COL;
          const rackTB = '+' + R('=', rackW - 2) + '+';
          const rackMD = '+' + R('-', rackW - 2) + '+';
          const rackRow = s => {
            const inside = rackW - 4;
            const t = s.length > inside ? s.slice(0, inside) : s;
            const gap = inside - t.length, l = gap >> 1;
            return '| ' + R(' ', l) + t + R(' ', gap - l) + ' |';
          };
          const catCell = i => {
            const c = CAT[i] || '';
            return c.length >= CAT_COL ? c.slice(0, CAT_COL) : c + R(' ', CAT_COL - c.length);
          };
          addRaw('tk-ok', catCell(0) + rackTB);
          addRaw('tk-ok', catCell(1) + rackRow('pi4 . pi4 . n150'));
          addRaw('tk-ok', catCell(2) + rackMD);
          addRaw('tk-ok', catCell(3) + rackRow('hdd 2TB . hdd 2TB . hdd 2TB . hdd 2TB'));
          addRaw('tk-ok', catCell(4) + rackRow('ssd 2TB . ssd 2TB . ext 12TB'));
          addRaw('tk-ok', catCell(5) + rackTB);
        } else {
          // Mobile: stacked — small cat centered, then rack box.
          add('tk-ok', center('/\\_/\\'));
          add('tk-ok', center('( o.o )'));
          add('tk-ok', center('> ^ <'));
          blank();
          add('tk-ok', hr2());
          add('tk-ok', rowC('pi4 . pi4 . n150'));
          add('tk-ok', rowC('5-port switch . 65W'));
          add('tk-ok', hr1());
          add('tk-ok', rowC('4x 2TB hdd'));
          add('tk-ok', rowC('2x 2TB ssd'));
          add('tk-ok', rowC('1x 12TB ext backup'));
          add('tk-ok', hr2());
        }

        // ─── COLLECTIONS (NFO sites-list style table) ───
        section('S U B - A R C H i V E S');
        if (!isMobile) {
          // Box-table: 56 cols total.
          //   bars(4) + name(10) + size(10) + desc(32) = 56
          //   (size col widened from 9 to 10 so "119 ZIMs" fits.)
          const aw = 10, bw = 10, cw = 32;
          const cell = (s, w) => {
            const t = (s.length > w - 2 ? s.slice(0, w - 2) : s);
            return ' ' + t + R(' ', w - 1 - t.length);
          };
          const tHead = () => '+' + R('=', aw) + '+' + R('=', bw) + '+' + R('=', cw) + '+';
          const tSep  = () => '+' + R('-', aw) + '+' + R('-', bw) + '+' + R('-', cw) + '+';
          const tRow  = (a, b, c) => '|' + cell(a, aw) + '|' + cell(b, bw) + '|' + cell(c, cw) + '|';
          addRaw('tk-ok',  tHead());
          addRaw('tk-ok',  tRow('ARCHiVE', 'SiZE', 'KiND'));
          addRaw('tk-ok',  tSep());
          addRaw('',       tRow('wiki',     '119 ZIMs', 'wikipedia, stackex + 117 more'));
          addRaw('',       tRow('pdf',      '800 GB',   '23K+ docs, vintage mags + more'));
          addRaw('',       tRow('os',       '244 GB',   'images, drivers, install media'));
          addRaw('',       tRow('iso',      '27 GB',    'vintage cd/dvd, abandonware'));
          addRaw('',       tRow('chiptune', '22 GB',    'mod/xm/s3m/it, keygen music'));
          addRaw('',       tRow('tube',     '228 GB',   'cs/eng/math/physics deep-dives'));
          addRaw('',       tRow('audio',    'audiobk',  'fiction + lectures + classics'));
          addRaw('',       tRow('retro',    'orphans',  'software saved from 404 errors'));
          addRaw('tk-ok',  tHead());
        } else {
          // Mobile: per-archive blocks of label+meta in prose.
          const block = (name, size, kind) => {
            addRaw('', padR('  ' + name + R(' ', Math.max(1, 10 - name.length)) + size));
            prose('tk-dim', kind);
            blank();
          };
          block('wiki',     '119 ZIMs', 'wikipedia, stackex, devdocs + more');
          block('pdf',      '800 GB',   '23,000+ documents');
          block('os',       '244 GB',   'images, drivers, install media');
          block('iso',      '27 GB',    'vintage cd/dvd, abandonware');
          block('chiptune', '22 GB',    'mod/xm/s3m/it, keygen music');
          block('tube',     '228 GB',   'cs/eng/math/physics deep-dives');
          block('audio',    'audiobk',  'fiction + lectures + classics');
          block('retro',    'orphans',  'software/manuals saved from 404');
        }

        // ─── ETHOS ──────────────────────────────────────
        section('E T H O S');
        add('tk-hot', center('no  ads'));
        add('tk-hot', center('basic  analytics'));
        add('tk-hot', center('no  user  accounts'));
        add('tk-hot', center('no  uptime  guarantee'));
        add('tk-hot', center('no  monetization'));

        // ─── GREETZ ─────────────────────────────────────
        section('G R E E T Z');
        prose('', 'regards fly out to the old-school scene who kept it alive:');
        blank();
        add('tk-ok', center('. MYTH . CORE . RAZOR 1911 . FAiRLiGHT .'));
        add('tk-ok', center('. DEViANCE . CLASS . PARADOX . TRSi .'));
        add('tk-ok', center('. DRiNK OR DiE . LAXiTY . oDDiTy . SAC .'));
        add('tk-ok', center('. RELOADED . SKiDROW . CODEX . HOODLUM .'));
        blank();
        prose('', 'and to the librarians still mirroring in 2026:');
        blank();
        add('tk-ok', center('. ARCHiVE.ORG . TOSEC . NO-iNTRO .'));
        add('tk-ok', center('. ABANDONiA . HOME OF THE UNDERDOGS .'));
        add('tk-ok', center('. SCENE.ORG . MOD ARCHIVE . DEMOZOO .'));
        add('tk-ok', center('. ZENIUS-I-VANISHER . ANNA\'S ARCHiVE .'));
        blank();
        add('',      center('archive everything'));
        add('',      center('mirror everything'));
        add('',      center('seed everything'));
        blank();
        prose('',    '"information wants to be free -- but it also wants to be preserved"');
        blank();
        add('',      center('fighting link rot, one wget at a time'));

        // ─── CONTACT ────────────────────────────────────
        section('C O N T A C T');
        add('tk-ok', hr2());
        addRaw('', infoRow('WWW',    'kunaldawn.com'));
        addRaw('', infoRow('GiTHUB', 'github.com/kunaldawn'));
        addRaw('', infoRow('EMAiL',  'kunal.dawn@gmail.com'));
        addRaw('', infoRow('iRC',    'we don\'t go there any more'));
        add('tk-ok', hr2());
        blank();
        prose('tk-dim', 'NOTE: this archive does not password files, ever.');
        prose('tk-dim', 'NOTE: nothing here is for sale, ever.');
        prose('tk-dim', 'NOTE: no tech support, no missing-disk emails.');

        // ─── DiSTRO / SiTES (the gag list) ──────────────
        section('D i S T R O   S i T E S');
        if (!isMobile) {
          //   bars(4) + name(19) + status(10) + loc(23) = 56
          const aw = 19, bw = 10, cw = 23;
          const cell = (s, w) => {
            const t = (s.length > w - 2 ? s.slice(0, w - 2) : s);
            return ' ' + t + R(' ', w - 1 - t.length);
          };
          const tHead = () => '+' + R('=', aw) + '+' + R('=', bw) + '+' + R('=', cw) + '+';
          const tSep  = () => '+' + R('-', aw) + '+' + R('-', bw) + '+' + R('-', cw) + '+';
          const tRow  = (a, b, c) => '|' + cell(a, aw) + '|' + cell(b, bw) + '|' + cell(c, cw) + '|';
          addRaw('tk-ok',  tHead());
          addRaw('tk-ok',  tRow('SiTE NAME', 'STATUS', 'LOCATiON'));
          addRaw('tk-ok',  tSep());
          addRaw('',       tRow('the.kitchen.shelf', 'WHQ',     'a flat, somewhere'));
          addRaw('',       tRow('the.solar.panel',   'POWER',   '~30W, off-grid'));
          addRaw('',       tRow('the.warm.rpi',      'COMPUTE', 'always on'));
          addRaw('',       tRow('the.warm.rpi.2',    'COMPUTE', 'also always on'));
          addRaw('',       tRow('the.cold.disk',     'BACKUP',  'spins on schedule'));
          addRaw('',       tRow('the.cat',           'QA',      'on top of the rack'));
          addRaw('tk-ok',  tHead());
        } else {
          add('', center('the.kitchen.shelf  -  WHQ'));
          add('', center('the.solar.panel    -  POWER'));
          add('', center('the.warm.rpi       -  COMPUTE'));
          add('', center('the.cold.disk      -  BACKUP'));
          add('', center('the.cat            -  QA'));
        }

        // ─── CLOSING BANNER ─────────────────────────────
        blank();
        add('tk-ok', hr2());
        add('tk-ok', rowC(''));
        add('tk-ok', rowC('DEV  ::  KUNAL DAWN  ::  MADE WITH <3'));
        add('tk-ok', rowC('LOGO + LAYOUT  ::  KD  &  AI'));
        add('tk-ok', rowC('LAST UPDATED   ::  2026-05-12'));
        add('tk-ok', rowC(''));
        add('tk-ok', hr2());
        blank();

        // ─── LOOP MARKER ────────────────────────────────
        add('tk-dim', divZag());
        blank();

        return L;
      })();
      function renderTicker(el) {
        const esc = s => s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
        const block = TICKER_LINES.map(([cls, txt]) =>
          cls ? `<span class="${cls}">${esc(txt)}</span>` : esc(txt)
        ).join('\n');
        // Double the content so the rAF-driven scroll can loop seamlessly
        el.innerHTML = block + '\n' + block;
      }

      /* ─── Scene: Plasma ─── */
      function drawPlasma(now) {
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS / 2;
        for (let y = 0; y < ROWS; y++) {
          const fy = y * 0.25 + t * 0.9;
          const sy = Math.sin(fy);
          for (let x = 0; x < COLS; x++) {
            const dx = x - cx, dy = (y - cy) * CHAR_ASPECT;
            const v = Math.sin(x * 0.18 + t * 1.3) + sy +
                      Math.sin((x + y) * 0.14 + t * 0.7) +
                      Math.sin(Math.sqrt(dx * dx + dy * dy) * 0.3 + t * 1.1);
            const norm = (v + 4) / 8;
            const idx = norm < 0 ? 0 : norm > 1 ? RAMP_LEN - 1 : (norm * RAMP_LEN) | 0;
            buf[y * COLS + x] = RAMP.charCodeAt(idx >= RAMP_LEN ? RAMP_LEN - 1 : idx);
          }
        }
      }

      /* ─── Scene: Fire (classic DOOM / PSX fire: propagate-up with drift + cool) ─── */
      let fireH = ROWS + 1;
      let fireBuf = new Float32Array(COLS * fireH);
      function drawFire(now) {
        if (geomRev.fire !== geometryRev) {
          fireH = ROWS + 1;
          fireBuf = new Float32Array(COLS * fireH);
          geomRev.fire = geometryRev;
        }
        const t = now * 0.001;
        // Seed bottom row: always hot but with per-cell jitter so flame bases flicker.
        for (let x = 0; x < COLS; x++) {
          fireBuf[(fireH - 1) * COLS + x] = 0.88 + Math.random() * 0.12;
        }
        // Slow wind shifts the whole flame gently.
        const wind = Math.sin(t * 0.7) * 0.6;
        // Propagate from below, with per-cell sideways drift (-1, 0, +1) and cooling.
        for (let y = 0; y < fireH - 1; y++) {
          for (let x = 0; x < COLS; x++) {
            const drift = ((Math.random() * 3) | 0) - 1 + (Math.random() < Math.abs(wind) ? (wind > 0 ? 1 : -1) : 0);
            const srcX = Math.max(0, Math.min(COLS - 1, x + drift));
            const below = fireBuf[(y + 1) * COLS + srcX];
            // Cooling decreases with altitude so flames thin out as they rise.
            const cooling = Math.random() * 0.055 + 0.002;
            fireBuf[y * COLS + x] = Math.max(0, below - cooling);
          }
        }
        // Render with a squared ramp so high-heat cells pop and low-heat fade quickly.
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const v = fireBuf[y * COLS + x];
            let idx = (v * v * RAMP_LEN) | 0;
            if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            if (idx < 0) idx = 0;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Starfield (3D parallax) ─── */
      const starCount = isMobile ? 80 : 160;
      const stars = new Float32Array(starCount * 3);
      function initStars() {
        for (let i = 0; i < starCount; i++) {
          stars[i * 3 + 0] = (Math.random() - 0.5) * 2;
          stars[i * 3 + 1] = (Math.random() - 0.5) * 2;
          stars[i * 3 + 2] = Math.random() * 1.5 + 0.1;
        }
      }
      initStars();
      function drawStars(now, dt) {
        clearBuf();
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
      }

      /* ─── Scene: Tunnel ─── */
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
        const cx = COLS / 2, cy = ROWS / 2;
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const dx = x - cx;
            const dy = (y - cy) * CHAR_ASPECT;
            const d = Math.max(0.6, Math.sqrt(dx * dx + dy * dy));
            tunU[y * COLS + x] = 14 / d;
            tunV[y * COLS + x] = Math.atan2(dy, dx) / Math.PI * 6;
            tunDist[y * COLS + x] = d;
          }
        }
      }
      buildTunnelTables();
      function drawTunnel(now) {
        const t = now * 0.001;
        const maxD = Math.sqrt((COLS/2)**2 + (ROWS*0.9)**2);
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const i = y * COLS + x;
            const u = tunU[i] + t * 2;
            const v = tunV[i] + t * 1;
            const check = (((u | 0) + (v | 0)) & 1);
            const shade = 1 - tunDist[i] / maxD;
            let idx = check ? (shade * (RAMP_LEN - 1)) | 0 : (shade * (RAMP_LEN - 1) * 0.35) | 0;
            if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            if (idx < 0) idx = 0;
            buf[i] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Torus (donut.c style, ASCII shaded) ─── */
      let zBuf = new Float32Array(COLS * ROWS);
      function drawTorus(now) {
        if (geomRev.torus !== geometryRev) {
          zBuf = new Float32Array(COLS * ROWS);
          geomRev.torus = geometryRev;
        }
        clearBuf();
        zBuf.fill(0);
        const A = now * 0.0008;
        const B = now * 0.0013;
        const cosA = Math.cos(A), sinA = Math.sin(A);
        const cosB = Math.cos(B), sinB = Math.sin(B);
        const R1 = 1, R2 = 2, K2 = 5;
        // Fit donut to whichever axis is more constrained (COLS in cells vs
        // ROWS * CHAR_ASPECT in equivalent x-cells), so wide canvases don't
        // overflow vertically.
        // K1 sized so projected silhouette comfortably fills the canvas.
        // Some rotations cause minor edge overflow — that's preferable to a
        // donut that floats in a sea of empty cells.
        const fit = Math.min(COLS, ROWS * CHAR_ASPECT);
        const K1 = fit * 0.95;
        const K1y = K1 / CHAR_ASPECT;
        for (let th = 0; th < 6.283; th += 0.17) {
          const cosT = Math.cos(th), sinT = Math.sin(th);
          const circleX = R2 + R1 * cosT;
          const circleY = R1 * sinT;
          for (let ph = 0; ph < 6.283; ph += 0.055) {
            const cosP = Math.cos(ph), sinP = Math.sin(ph);
            const x = circleX * (cosB * cosP + sinA * sinB * sinP) - circleY * cosA * sinB;
            const y = circleX * (sinB * cosP - sinA * cosB * sinP) + circleY * cosA * cosB;
            const z = K2 + cosA * circleX * sinP + circleY * sinA;
            const ooz = 1 / z;
            const xp = (COLS / 2 + K1 * ooz * x) | 0;
            const yp = (ROWS / 2 - K1y * ooz * y) | 0;
            if (xp < 0 || xp >= COLS || yp < 0 || yp >= ROWS) continue;
            const L = cosP * cosT * sinB - cosA * cosT * sinP - sinA * sinT +
                      cosB * (cosA * sinT - cosT * sinA * sinP);
            // Render BOTH faces (z-buffered) so the torus reads as a solid
            // ring from any rotation. Front-lit faces use the upper ramp;
            // back faces use a dim band.
            const bi = yp * COLS + xp;
            if (ooz > zBuf[bi]) {
              zBuf[bi] = ooz;
              if (L > 0) {
                let li = (L * 8) | 0;
                if (li >= RAMP_LEN) li = RAMP_LEN - 1;
                buf[bi] = RAMP.charCodeAt(li);
              } else {
                // Dim shading for back face: ramp index 2-5
                buf[bi] = RAMP.charCodeAt(2 + (((-L) * 3) | 0));
              }
            }
          }
        }
      }

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

      /* ─── Scene: Wave3D (3D sine heightmap, perspective-projected dots) ─── */
      function drawWave3d(now) {
        clearBuf();
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS * 0.78;
        const GRID = isMobile ? 18 : 26;
        for (let gz = GRID; gz >= 1; gz--) {
          const zScale = 1.15 / (gz * 0.17 + 0.55);
          for (let gx = -GRID; gx <= GRID; gx++) {
            const h = Math.sin(gx * 0.42 + t * 1.3) + Math.cos(gz * 0.42 + t * 0.9) * 0.7 + Math.sin((gx + gz) * 0.19 + t) * 0.5;
            const sx = Math.round(cx + gx * zScale * 1.9);
            const sy = Math.round(cy - (h * zScale * 4.2) - gz * zScale * 1.5);
            if (sx < 0 || sx >= COLS || sy < 0 || sy >= ROWS) continue;
            const bright = Math.min(1, zScale * 1.2);
            const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
            buf[sy * COLS + sx] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Ripple (radial water ripple from centre) ─── */
      function drawRipple(now) {
        const t = now * 0.003;
        const cx = COLS / 2, cy = ROWS / 2;
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const dx = x - cx;
            const dy = (y - cy) * CHAR_ASPECT;
            const d = Math.sqrt(dx * dx + dy * dy);
            const v = Math.sin(d * 0.55 - t * 3) * Math.exp(-d * 0.035) +
                      Math.sin(d * 0.25 - t * 1.5) * 0.4 * Math.exp(-d * 0.02);
            const norm = (v + 1.2) / 2.4;
            let idx = (norm * RAMP_LEN) | 0;
            if (idx < 0) idx = 0;
            if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Life (Conway's Game of Life with periodic reseed) ─── */
      let lifeSz = COLS * ROWS;
      let lifeCurr = new Uint8Array(lifeSz);
      let lifeNext = new Uint8Array(lifeSz);
      let lifeAcc = 0;
      let lifeAge = 0;
      function seedLife() {
        for (let i = 0; i < lifeSz; i++) lifeCurr[i] = Math.random() < 0.24 ? 1 : 0;
        lifeAge = 0;
      }
      seedLife();
      function stepLife() {
        for (let y = 0; y < ROWS; y++) {
          const ym1 = (y - 1 + ROWS) % ROWS;
          const yp1 = (y + 1) % ROWS;
          for (let x = 0; x < COLS; x++) {
            const xm1 = (x - 1 + COLS) % COLS;
            const xp1 = (x + 1) % COLS;
            const n = lifeCurr[ym1 * COLS + xm1] + lifeCurr[ym1 * COLS + x] + lifeCurr[ym1 * COLS + xp1] +
                      lifeCurr[y   * COLS + xm1] +                              lifeCurr[y   * COLS + xp1] +
                      lifeCurr[yp1 * COLS + xm1] + lifeCurr[yp1 * COLS + x] + lifeCurr[yp1 * COLS + xp1];
            const alive = lifeCurr[y * COLS + x];
            lifeNext[y * COLS + x] = alive ? ((n === 2 || n === 3) ? 1 : 0) : (n === 3 ? 1 : 0);
          }
        }
        const tmp = lifeCurr; lifeCurr = lifeNext; lifeNext = tmp;
      }
      function drawLife(now, dt) {
        if (geomRev.life !== geometryRev) {
          lifeSz = COLS * ROWS;
          lifeCurr = new Uint8Array(lifeSz);
          lifeNext = new Uint8Array(lifeSz);
          lifeAcc = 0;
          lifeAge = 0;
          seedLife();
          geomRev.life = geometryRev;
        }
        lifeAcc += dt;
        if (lifeAcc > 130) {
          lifeAcc = 0;
          stepLife();
          lifeAge++;
          if (lifeAge > 40) seedLife();
        }
        for (let i = 0; i < lifeSz; i++) {
          buf[i] = lifeCurr[i] ? 64 /* @ */ : 32;
        }
      }

      /* ─── Scene: Cube (wireframe rotating cube, Bresenham line-drawn) ─── */
      const CUBE_V = [[-1,-1,-1],[1,-1,-1],[1,1,-1],[-1,1,-1],[-1,-1,1],[1,-1,1],[1,1,1],[-1,1,1]];
      const CUBE_E = [[0,1],[1,2],[2,3],[3,0],[4,5],[5,6],[6,7],[7,4],[0,4],[1,5],[2,6],[3,7]];
      function plotLine(x0, y0, x1, y1, ch) {
        let dx = Math.abs(x1 - x0), dy = -Math.abs(y1 - y0);
        const sx = x0 < x1 ? 1 : -1, sy = y0 < y1 ? 1 : -1;
        let err = dx + dy;
        while (true) {
          if (x0 >= 0 && x0 < COLS && y0 >= 0 && y0 < ROWS) buf[y0 * COLS + x0] = ch;
          if (x0 === x1 && y0 === y1) break;
          const e2 = 2 * err;
          if (e2 >= dy) { err += dy; x0 += sx; }
          if (e2 <= dx) { err += dx; y0 += sy; }
        }
      }
      function drawCube(now) {
        clearBuf();
        const A = now * 0.0009, B = now * 0.0013, C = now * 0.0007;
        const ca = Math.cos(A), sa = Math.sin(A);
        const cb = Math.cos(B), sb = Math.sin(B);
        const cc = Math.cos(C), sc = Math.sin(C);
        const scale = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.5;
        const cx = COLS / 2, cy = ROWS / 2;
        const proj = new Array(8);
        for (let i = 0; i < 8; i++) {
          const v = CUBE_V[i];
          let x = v[0], y = v[1], z = v[2];
          // Rx
          let y1 = y * cc - z * sc, z1 = y * sc + z * cc;
          // Ry
          let x1 = x * ca + z1 * sa, z2 = -x * sa + z1 * ca;
          // Rz
          let x2 = x1 * cb - y1 * sb, y2 = x1 * sb + y1 * cb;
          const dist = 4;
          const ooz = 1 / (z2 + dist);
          const sxp = Math.round(cx + x2 * ooz * scale * 2);
          const syp = Math.round(cy - y2 * ooz * scale);
          proj[i] = [sxp, syp];
        }
        for (let i = 0; i < CUBE_E.length; i++) {
          const e = CUBE_E[i];
          plotLine(proj[e[0]][0], proj[e[0]][1], proj[e[1]][0], proj[e[1]][1], 42 /* * */);
        }
        // corner dots
        for (let i = 0; i < 8; i++) {
          const p = proj[i];
          if (p[0] >= 0 && p[0] < COLS && p[1] >= 0 && p[1] < ROWS) buf[p[1] * COLS + p[0]] = 35 /* # */;
        }
      }

      /* ─── Scene: Kaleido (8-way mirrored plasma) ─── */
      function drawKaleido(now) {
        const t = now * 0.0012;
        const cx = COLS / 2, cy = ROWS / 2;
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            let dx = Math.abs(x - cx);
            let dy = Math.abs((y - cy) * CHAR_ASPECT);
            if (dy > dx) { const tmp = dx; dx = dy; dy = tmp; }
            const v = Math.sin(dx * 0.25 + t * 2) + Math.cos(dy * 0.32 + t * 1.3) + Math.sin((dx + dy) * 0.18 + t * 0.8);
            const norm = (v + 3) / 6;
            let idx = (norm * RAMP_LEN) | 0;
            if (idx < 0) idx = 0; if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Metaballs (3 orbiting blobs) ─── */
      function drawMetaballs(now) {
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS / 2;
        const bx = [ cx + Math.cos(t) * COLS * 0.32,
                     cx + Math.cos(t * 0.77 + 2) * COLS * 0.35,
                     cx + Math.cos(t * 1.15 + 4) * COLS * 0.28 ];
        const by = [ cy + Math.sin(t * 0.91) * ROWS * 0.38,
                     cy + Math.sin(t * 1.13 + 1) * ROWS * 0.3,
                     cy + Math.sin(t * 0.83 + 3) * ROWS * 0.42 ];
        const br = [ 40, 32, 50 ];
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            let v = 0;
            for (let i = 0; i < 3; i++) {
              const dx = x - bx[i];
              const dy = (y - by[i]) * CHAR_ASPECT;
              v += br[i] / (dx * dx + dy * dy + 0.5);
            }
            const norm = Math.min(1, v * 0.12);
            let idx = (norm * RAMP_LEN) | 0;
            if (idx < 0) idx = 0; if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Mandelbrot (slowly panning+zooming fractal) ─── */
      function drawMandelbrot(now) {
        const t = now * 0.0004;
        const zoom = 1.6 + Math.sin(t * 0.6) * 0.9;
        const cx = -0.745 + Math.sin(t * 0.3) * 0.12;
        const cy = 0.113 + Math.cos(t * 0.25) * 0.08;
        const maxIter = 42;
        const LOG2 = Math.log(2);
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const a0 = (x - COLS / 2) / COLS * 3 / zoom + cx;
            const b0 = (y - ROWS / 2) / ROWS * 2.2 / zoom + cy;
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
          }
        }
      }

      /* ─── Scene: Copper (horizontal scrolling bar stripes) ─── */
      function drawCopper(now) {
        const t = now * 0.002;
        for (let y = 0; y < ROWS; y++) {
          const bar = Math.sin(y * 0.55 - t * 2) * 0.5 + 0.5;
          const bar2 = Math.sin(y * 0.15 + t * 0.8) * 0.4 + 0.5;
          for (let x = 0; x < COLS; x++) {
            const ripple = Math.sin(x * 0.18 + t * 3) * 0.12;
            const mod = Math.max(0, Math.min(1, bar * 0.7 + bar2 * 0.35 + ripple));
            let idx = (mod * RAMP_LEN) | 0;
            if (idx < 0) idx = 0; if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Cellular (rule 30, rolling history) ─── */
      let caRow = new Uint8Array(COLS);
      let caHistory = new Uint8Array(COLS * ROWS);
      let caNext = new Uint8Array(COLS);
      let caAcc = 0;
      let caAge = 0;
      let caHead = 0;
      function seedCellular() {
        caRow.fill(0);
        caRow[COLS >> 1] = 1;
        caHistory.fill(0);
        caAge = 0;
        caHead = 0;
      }
      seedCellular();
      // Ring buffer: caHead is the row index of the *most recent* generation;
      // older generations live above it modulo ROWS. Render maps logical row
      // (top of canvas) to physical row in caHistory.
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
        if (geomRev.cell !== geometryRev) {
          caRow = new Uint8Array(COLS);
          caHistory = new Uint8Array(COLS * ROWS);
          caNext = new Uint8Array(COLS);
          seedCellular();
          geomRev.cell = geometryRev;
        }
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

      /* ─── Scene: Spiral (particles on an outward spiral with fade) ─── */
      const spiralN = isMobile ? 60 : 90;
      const spiralPart = new Float32Array(spiralN * 3); // angle, radius, speed
      function initSpiral() {
        for (let i = 0; i < spiralN; i++) {
          spiralPart[i * 3 + 0] = Math.random() * Math.PI * 2;
          spiralPart[i * 3 + 1] = Math.random() * 2;
          spiralPart[i * 3 + 2] = 0.5 + Math.random() * 1.5;
        }
      }
      initSpiral();
      function drawSpiral(now, dt) {
        clearBuf();
        const cx = COLS / 2, cy = ROWS / 2;
        const maxR = Math.sqrt(cx * cx + cy * cy);
        const step = (dt || 16) / 1000;
        for (let i = 0; i < spiralN; i++) {
          const b = i * 3;
          spiralPart[b + 1] += step * spiralPart[b + 2] * 3;
          spiralPart[b + 0] += step * 1.4;
          if (spiralPart[b + 1] > maxR) {
            spiralPart[b + 0] = Math.random() * Math.PI * 2;
            spiralPart[b + 1] = 0.1;
            spiralPart[b + 2] = 0.5 + Math.random() * 1.5;
          }
          const r = spiralPart[b + 1];
          const a = spiralPart[b + 0] + r * 0.3;
          const sx = Math.round(cx + Math.cos(a) * r);
          const sy = Math.round(cy + Math.sin(a) * r * 0.55);
          if (sx < 0 || sx >= COLS || sy < 0 || sy >= ROWS) continue;
          const bright = Math.max(0.08, 1 - r / maxR);
          const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
          buf[sy * COLS + sx] = RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Voxel (Comanche-style heightmap voxel terrain) ─── */
      function hash2(x, y) {
        let h = x * 374761393 + y * 668265263;
        h = (h ^ (h >>> 13)) * 1274126177;
        return ((h ^ (h >>> 16)) >>> 0) / 4294967295;
      }
      function voxelHeight(x, y) {
        // layered value noise
        const x0 = Math.floor(x), y0 = Math.floor(y);
        const fx = x - x0, fy = y - y0;
        const a = hash2(x0, y0);
        const b = hash2(x0 + 1, y0);
        const c = hash2(x0, y0 + 1);
        const d = hash2(x0 + 1, y0 + 1);
        const u = fx * fx * (3 - 2 * fx);
        const v = fy * fy * (3 - 2 * fy);
        return a + (b - a) * u + (c - a) * v + (a - b - c + d) * u * v;
      }
      let voxelYBuf = new Int16Array(COLS);
      function drawVoxel(now) {
        if (geomRev.voxel !== geometryRev) {
          voxelYBuf = new Int16Array(COLS);
          geomRev.voxel = geometryRev;
        }
        clearBuf();
        const t = now * 0.00035;
        const horizon = Math.floor(ROWS * 0.5);
        // ── Sky: sparse twinkling stars above the horizon
        const starN = isMobile ? 10 : 18;
        for (let i = 0; i < starN; i++) {
          const sx = (i * 41 + ((t * 120) | 0)) % COLS;
          const sy = ((i * 7) % Math.max(1, horizon));
          if ((((i + (t * 5)) | 0) & 1)) buf[sy * COLS + sx] = 46; // '.'
        }
        const camX = t * 12;
        const camZ = t * 10;
        const CAM_H = 0.45;        // camera altitude (world-units) above "sea level"
        const FOV = 15;            // effective focal length; bigger -> taller peaks
        for (let x = 0; x < COLS; x++) voxelYBuf[x] = ROWS;
        // Comanche Y-buffer: iterate near -> far. Close voxels below the horizon
        // (camera above), distant mountain peaks rise above horizon into the sky.
        for (let z = 1.4; z < 38; z += 0.38) {
          const invZ = 1 / z;
          const fogZ = Math.max(0.1, 1 - z / 32);
          for (let sx = 0; sx < COLS; sx++) {
            const wx = camX + (sx - COLS / 2) * z * 0.09;
            const wz = camZ + z;
            // Multi-octave noise, scaled down so no single octave pins every peak
            // to the canvas ceiling. High x-freq so close slices still vary across columns.
            const h = (voxelHeight(wx * 0.35, wz * 0.11) * 1.2 +
                       voxelHeight(wx * 0.9 + 11, wz * 0.25 + 3) * 0.5 +
                       voxelHeight(wx * 2.1 + 7, wz * 0.55 + 9) * 0.22) * 0.52;
            const screenY = Math.round(horizon - (h - CAM_H) * FOV * invZ);
            if (screenY < voxelYBuf[sx]) {
              const top = Math.max(0, screenY);
              const bot = voxelYBuf[sx];
              // Per-column height influences shade so varying peaks don't all render identically.
              const heightTone = Math.max(0.25, Math.min(1, h * 0.75 + 0.25));
              for (let y = top; y < bot; y++) {
                const depth = (y - top) / Math.max(1, ROWS - horizon + 1);
                const shade = fogZ * heightTone * Math.max(0.1, 1 - depth * 0.5);
                const ri = Math.max(1, Math.min(RAMP_LEN - 1, (shade * RAMP_LEN) | 0));
                buf[y * COLS + sx] = RAMP.charCodeAt(ri);
              }
              voxelYBuf[sx] = screenY;
            }
          }
        }
      }

      /* ─── Scene: Checker (perspective ground plane, Space Harrier-style) ─── */
      function drawChecker(now) {
        clearBuf();
        const t = now * 0.001;
        const horizon = Math.floor(ROWS * 0.35);
        // sky rows stay black (cleared)
        for (let y = horizon + 1; y < ROWS; y++) {
          const perspective = (y - horizon) / (ROWS - horizon);
          // clamp the far plane so the farthest rows aren't infinitesimally small
          const z = 3 / Math.max(0.09, perspective);
          for (let x = 0; x < COLS; x++) {
            const wx = (x - COLS / 2) * z * 0.22 + t * 6;
            const wz = z + t * 5;
            // big cells so the checker reads at all depths (Space Harrier feel)
            const cell = ((Math.floor(wx / 5.5) + Math.floor(wz / 3.8)) & 1);
            const fog = Math.pow(Math.max(0, 1 - z / 40), 1.3);
            const idx = cell ? Math.max(2, Math.min(RAMP_LEN - 1, (fog * RAMP_LEN * 1.4) | 0))
                             : Math.max(0, Math.min(RAMP_LEN - 1, (fog * RAMP_LEN * 0.18) | 0));
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
        // horizon line as a thin bright strip
        for (let x = 0; x < COLS; x++) {
          buf[horizon * COLS + x] = 45; // '-'
        }
      }

      /* ─── Scene: Vectors (bouncing 3D "vector balls", Amiga demo classic) ─── */
      const VEC_COUNT = isMobile ? 28 : 48;
      const vecs = new Float32Array(VEC_COUNT * 6); // x,y,z,vx,vy,vz
      function initVecs() {
        for (let i = 0; i < VEC_COUNT; i++) {
          const b = i * 6;
          vecs[b + 0] = (Math.random() - 0.5) * 2;
          vecs[b + 1] = (Math.random() - 0.5) * 2;
          vecs[b + 2] = (Math.random() - 0.5) * 2;
          vecs[b + 3] = (Math.random() - 0.5) * 1.6;
          vecs[b + 4] = (Math.random() - 0.5) * 1.6;
          vecs[b + 5] = (Math.random() - 0.5) * 1.6;
        }
      }
      initVecs();
      const vecOrder = new Array(VEC_COUNT);
      for (let i = 0; i < VEC_COUNT; i++) vecOrder[i] = i;
      const vecZs = new Float32Array(VEC_COUNT);
      const vecProjX = new Float32Array(VEC_COUNT);
      const vecProjY = new Float32Array(VEC_COUNT);
      function drawVectors(now, dt) {
        clearBuf();
        const t = now * 0.0009;
        const cosA = Math.cos(t), sinA = Math.sin(t);
        const cosB = Math.cos(t * 0.7), sinB = Math.sin(t * 0.7);
        const cx = COLS / 2, cy = ROWS / 2;
        const scale = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.78;
        const step = (dt || 16) / 1000;
        // Depth-sort indices
        for (let i = 0; i < VEC_COUNT; i++) {
          const b = i * 6;
          // bounce physics
          vecs[b + 0] += vecs[b + 3] * step;
          vecs[b + 1] += vecs[b + 4] * step;
          vecs[b + 2] += vecs[b + 5] * step;
          if (vecs[b + 0] > 1 || vecs[b + 0] < -1) vecs[b + 3] *= -1;
          if (vecs[b + 1] > 1 || vecs[b + 1] < -1) vecs[b + 4] *= -1;
          if (vecs[b + 2] > 1 || vecs[b + 2] < -1) vecs[b + 5] *= -1;
          let x = vecs[b + 0], y = vecs[b + 1], z = vecs[b + 2];
          // Ry
          let x1 = x * cosA + z * sinA, z1 = -x * sinA + z * cosA;
          // Rx
          let y2 = y * cosB - z1 * sinB, z2 = y * sinB + z1 * cosB;
          const ooz = 1 / (z2 + 3);
          vecProjX[i] = cx + x1 * ooz * scale * 2;
          vecProjY[i] = cy - y2 * ooz * scale;
          vecZs[i] = z2;
        }
        vecOrder.sort((a, b) => vecZs[b] - vecZs[a]);
        for (let k = 0; k < VEC_COUNT; k++) {
          const i = vecOrder[k];
          const px = Math.round(vecProjX[i]);
          const py = Math.round(vecProjY[i]);
          if (px < 0 || px >= COLS || py < 0 || py >= ROWS) continue;
          const depth = (vecZs[i] + 1) / 2;
          const bright = 1 - depth;
          let idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
          buf[py * COLS + px] = RAMP.charCodeAt(idx);
        }
      }

      /* ─── Scene: Rotozoom (rotating+zooming textured plane) ─── */
      function drawRotozoom(now) {
        const t = now * 0.001;
        const zoom = 0.6 + Math.sin(t * 0.5) * 0.35;
        const angle = t * 0.8;
        const ca = Math.cos(angle) * zoom;
        const sa = Math.sin(angle) * zoom;
        const cx = COLS / 2, cy = ROWS / 2;
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const dx = x - cx, dy = (y - cy) * CHAR_ASPECT;
            const u = dx * ca - dy * sa;
            const v = dx * sa + dy * ca;
            // sample: xor-style pattern
            const ui = Math.floor(u + t * 30);
            const vi = Math.floor(v + t * 20);
            const pat = (ui ^ vi) & 31;
            const shade = (pat / 31 + Math.sin(u * 0.4 + t) * 0.2 + 0.5) * 0.7;
            const norm = Math.max(0, Math.min(1, shade));
            let idx = (norm * RAMP_LEN) | 0;
            if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Sphere3D (wireframe sphere with latitude/longitude grid) ─── */
      let sphereZBuf = new Float32Array(COLS * ROWS);
      function drawSphere3d(now) {
        if (geomRev.sphere !== geometryRev) {
          sphereZBuf = new Float32Array(COLS * ROWS);
          geomRev.sphere = geometryRev;
        }
        clearBuf();
        sphereZBuf.fill(0);
        const A = now * 0.0009, B = now * 0.0013;
        const cosA = Math.cos(A), sinA = Math.sin(A);
        const cosB = Math.cos(B), sinB = Math.sin(B);
        const cx = COLS / 2, cy = ROWS / 2;
        const R = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.52;
        // latitude rings (fewer on mobile)
        const lats = isMobile ? 8 : 12;
        const lngs = isMobile ? 14 : 20;
        for (let la = 1; la < lats; la++) {
          const theta = (la / lats) * Math.PI;
          const rr = Math.sin(theta);
          const zY = Math.cos(theta);
          for (let lo = 0; lo < lngs * 6; lo++) {
            const phi = (lo / (lngs * 6)) * Math.PI * 2;
            let x = rr * Math.cos(phi);
            let y = zY;
            let z = rr * Math.sin(phi);
            // Ry
            let x1 = x * cosA + z * sinA, z1 = -x * sinA + z * cosA;
            // Rx
            let y2 = y * cosB - z1 * sinB, z2 = y * sinB + z1 * cosB;
            const ooz = 1 / (z2 + 3);
            const sxp = Math.round(cx + x1 * ooz * R * 2);
            const syp = Math.round(cy - y2 * ooz * R);
            if (sxp < 0 || sxp >= COLS || syp < 0 || syp >= ROWS) continue;
            if (ooz > sphereZBuf[syp * COLS + sxp]) {
              sphereZBuf[syp * COLS + sxp] = ooz;
              const bright = Math.max(0.1, 1 - z2 * 0.4);
              const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
              buf[syp * COLS + sxp] = RAMP.charCodeAt(idx);
            }
          }
        }
        // longitudes
        for (let lo = 0; lo < lngs; lo++) {
          const phi = (lo / lngs) * Math.PI * 2;
          for (let la = 0; la < lats * 6; la++) {
            const theta = (la / (lats * 6)) * Math.PI;
            const rr = Math.sin(theta);
            const zY = Math.cos(theta);
            let x = rr * Math.cos(phi);
            let y = zY;
            let z = rr * Math.sin(phi);
            let x1 = x * cosA + z * sinA, z1 = -x * sinA + z * cosA;
            let y2 = y * cosB - z1 * sinB, z2 = y * sinB + z1 * cosB;
            const ooz = 1 / (z2 + 3);
            const sxp = Math.round(cx + x1 * ooz * R * 2);
            const syp = Math.round(cy - y2 * ooz * R);
            if (sxp < 0 || sxp >= COLS || syp < 0 || syp >= ROWS) continue;
            if (ooz > sphereZBuf[syp * COLS + sxp]) {
              sphereZBuf[syp * COLS + sxp] = ooz;
              const bright = Math.max(0.1, 1 - z2 * 0.4);
              const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
              buf[syp * COLS + sxp] = RAMP.charCodeAt(idx);
            }
          }
        }
      }

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

      /* ─── Scene: Warp (hyperspace radial star streaks) ─── */
      const warpN = isMobile ? 60 : 110;
      const warpPart = new Float32Array(warpN * 3); // angle, radius, speed
      function initWarp() {
        for (let i = 0; i < warpN; i++) {
          warpPart[i * 3 + 0] = Math.random() * Math.PI * 2;
          warpPart[i * 3 + 1] = Math.random() * 2 + 0.1;
          warpPart[i * 3 + 2] = 0.8 + Math.random() * 1.8;
        }
      }
      initWarp();
      function drawWarp(now, dt) {
        clearBuf();
        const cx = COLS / 2, cy = ROWS / 2;
        const maxR = Math.sqrt(cx * cx + (cy * CHAR_ASPECT) * (cy * CHAR_ASPECT));
        const step = (dt || 16) / 1000;
        for (let i = 0; i < warpN; i++) {
          const b = i * 3;
          warpPart[b + 1] += step * warpPart[b + 2] * 8;
          if (warpPart[b + 1] > maxR) {
            warpPart[b + 0] = Math.random() * Math.PI * 2;
            warpPart[b + 1] = 0.1;
            warpPart[b + 2] = 0.8 + Math.random() * 1.8;
          }
          // Draw a short streak: 3 samples along the ray
          const a = warpPart[b + 0];
          const r = warpPart[b + 1];
          const ca = Math.cos(a), sa = Math.sin(a);
          for (let s = 0; s < 3; s++) {
            const rr = r - s * 0.9;
            if (rr <= 0) continue;
            const sx = Math.round(cx + ca * rr);
            const sy = Math.round(cy + sa * rr * 0.55);
            if (sx < 0 || sx >= COLS || sy < 0 || sy >= ROWS) continue;
            const bright = Math.max(0.15, Math.min(1, rr / maxR + 0.1)) - s * 0.25;
            if (bright <= 0) continue;
            const idx = Math.max(1, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
            buf[sy * COLS + sx] = RAMP.charCodeAt(idx);
          }
        }
      }

      /* ─── Scene: Moire (two rotating radial patterns interfering) ─── */
      function drawMoire(now) {
        const t = now * 0.0009;
        const ca = Math.cos(t), sa = Math.sin(t);
        const cb = Math.cos(t * 0.7), sb = Math.sin(t * 0.7);
        const cx = COLS / 2, cy = ROWS / 2;
        const ox1 = Math.sin(t * 0.8) * 6;
        const oy1 = Math.cos(t * 0.8) * 3;
        for (let y = 0; y < ROWS; y++) {
          for (let x = 0; x < COLS; x++) {
            const dxA = x - cx + ox1, dyA = (y - cy + oy1) * CHAR_ASPECT;
            const dxB = x - cx - ox1, dyB = (y - cy - oy1) * CHAR_ASPECT;
            const dA = Math.sqrt(dxA * dxA + dyA * dyA);
            const dB = Math.sqrt(dxB * dxB + dyB * dyB);
            const v = Math.sin(dA * 0.7 + t * 3) + Math.sin(dB * 0.7 - t * 2);
            const norm = (v + 2) / 4;
            let idx = (norm * RAMP_LEN) | 0;
            if (idx < 0) idx = 0; if (idx >= RAMP_LEN) idx = RAMP_LEN - 1;
            buf[y * COLS + x] = RAMP.charCodeAt(idx);
          }
        }
      }

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

      /* ─── Scene: Ribbon (3D undulating ribbon, two edges) ─── */
      function drawRibbon(now) {
        clearBuf();
        const t = now * 0.0011;
        const cx = COLS / 2, cy = ROWS / 2;
        const segs = isMobile ? 140 : 220;
        // Single fit scale (in cells) shared by x and y so rotation looks square;
        // y divides by CHAR_ASPECT to compensate for tall monospace cells.
        const projScale = Math.min(COLS, ROWS * CHAR_ASPECT) * 0.85;
        for (let i = 0; i < segs; i++) {
          const u = (i / segs) * 4 - 2; // -2..2
          // ribbon parametric: x(u), y(u), z(u). Larger y oscillation so the
          // ribbon's vertical sweep fills more of the canvas at most rotations.
          const x = u * 1.4;
          const y0 = Math.sin(u * 1.8 + t * 2.2) * 1.6;
          const z  = Math.cos(u * 1.5 + t * 1.7) * 1.4;
          const A = t * 0.7, B = t * 0.5;
          const cosA = Math.cos(A), sinA = Math.sin(A);
          const cosB = Math.cos(B), sinB = Math.sin(B);
          for (let side = -1; side <= 1; side += 2) {
            const yy = y0 + side * 0.35; // ribbon thickness
            // Ry
            let x1 = x * cosA + z * sinA, z1 = -x * sinA + z * cosA;
            // Rx
            let y2 = yy * cosB - z1 * sinB, z2 = yy * sinB + z1 * cosB;
            const ooz = 1 / (z2 + 5);
            const sxp = Math.round(cx + x1 * ooz * projScale);
            const syp = Math.round(cy - y2 * ooz * projScale / CHAR_ASPECT);
            if (sxp < 0 || sxp >= COLS || syp < 0 || syp >= ROWS) continue;
            const bright = Math.max(0.15, Math.min(1, ooz * 3));
            const idx = Math.max(2, Math.min(RAMP_LEN - 1, (bright * RAMP_LEN) | 0));
            buf[syp * COLS + sxp] = RAMP.charCodeAt(idx);
          }
        }
      }

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
      const bobXs = new Int16Array(bobN);
      const bobYs = new Int16Array(bobN);
      const bobZs = new Float32Array(bobN);
      const bobOrder = new Array(bobN);
      for (let i = 0; i < bobN; i++) bobOrder[i] = i;
      function drawBobs(now) {
        clearBuf();
        const t = now * 0.001;
        const cx = COLS / 2, cy = ROWS / 2;
        // Lissajous parameters slowly morph
        const a = 3 + Math.sin(t * 0.3) * 1.5;
        const b = 4 + Math.cos(t * 0.27) * 1.5;
        const phaseShift = t * 0.6;
        // Build positions and z-depth, sort back-to-front.
        for (let i = 0; i < bobN; i++) {
          const u = (i / bobN) * Math.PI * 2;
          bobXs[i] = Math.round(cx + Math.sin(a * u + phaseShift) * (COLS * 0.42));
          bobYs[i] = Math.round(cy + Math.sin(b * u) * (ROWS * 0.42));
          bobZs[i] = Math.cos(a * u + phaseShift); // -1..1 fake z
        }
        bobOrder.sort((p, q) => bobZs[p] - bobZs[q]);
        for (let k = 0; k < bobN; k++) {
          const i = bobOrder[k];
          const x = bobXs[i], y = bobYs[i];
          const bright = (bobZs[i] * 0.5 + 0.5);
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

      /* ─── Scene: Mandelbox (animated 2D slice) ─── */
      function drawMandelbox(now) {
        const t = now * 0.0005;
        const scale = -1.5 + Math.sin(t) * 0.7;
        const minRad2 = 0.25, fixedRad2 = 1.0;
        const cx = -0.05 + Math.sin(t * 0.4) * 0.3;
        const cy =  0.05 + Math.cos(t * 0.3) * 0.3;
        const maxIter = 20;
        for (let py = 0; py < ROWS; py++) {
          for (let px = 0; px < COLS; px++) {
            let x = (px - COLS / 2) / COLS * 5;
            let y = (py - ROWS / 2) / ROWS * 5;
            const ox = x, oy = y;
            let i = 0;
            let minR2 = 1e9;
            for (; i < maxIter; i++) {
              // Box fold
              if (x > 1) x = 2 - x; else if (x < -1) x = -2 - x;
              if (y > 1) y = 2 - y; else if (y < -1) y = -2 - y;
              // Sphere fold
              const r2 = x * x + y * y;
              if (r2 < minRad2) { const m = fixedRad2 / minRad2; x *= m; y *= m; }
              else if (r2 < fixedRad2) { const m = fixedRad2 / r2; x *= m; y *= m; }
              x = x * scale + cx;
              y = y * scale + cy;
              const nr2 = x * x + y * y;
              if (nr2 < minR2) minR2 = nr2;
              if (nr2 > 256) break;
            }
            let norm;
            if (i < maxIter) {
              // Escaped — shade by iteration count (exterior, brighter = slower escape)
              norm = i / maxIter;
            } else {
              // Trapped — shade by minimum orbit radius (interior detail)
              norm = Math.min(1, Math.sqrt(minR2) * 0.25);
            }
            const idx = Math.max(0, Math.min(RAMP_LEN - 1, (norm * RAMP_LEN) | 0));
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
        const amp = Math.min(COLS * 0.32, 18);
        // Slower turn so 22 rows show ~1.5 visible twists, not 2.8.
        const turn = 0.42;
        for (let y = 0; y < ROWS; y++) {
          const phase = y * turn + t * 1.4;
          const x1 = cx + Math.cos(phase) * amp;
          const x2 = cx - Math.cos(phase) * amp;
          const z1 = Math.sin(phase); // +1 in front, -1 behind
          const sx1 = Math.round(x1), sx2 = Math.round(x2);
          // Distinct strand chars by depth; strand 2 mirrors via z2 = -z1
          const ch1 = z1 >  0.5 ? 79 /*'O'*/ : z1 > -0.5 ? 111 /*'o'*/ : 46 /*'.'*/;
          const ch2 = z1 < -0.5 ? 79          : z1 <  0.5 ? 111          : 46;
          if (Math.abs(sx1 - sx2) <= 1) {
            // Strands cross — draw a single 'X' at the meeting point
            const xc = Math.round((sx1 + sx2) / 2);
            if (xc >= 0 && xc < COLS) buf[y * COLS + xc] = 88 /*'X'*/;
          } else {
            if (sx1 >= 0 && sx1 < COLS) buf[y * COLS + sx1] = ch1;
            if (sx2 >= 0 && sx2 < COLS) buf[y * COLS + sx2] = ch2;
            // Rung every 2 rows; '-' between strand endpoints
            if (y % 2 === 0) {
              const lo = Math.min(sx1, sx2), hi = Math.max(sx1, sx2);
              for (let x = Math.max(0, lo + 1); x < Math.min(COLS, hi); x++) {
                if (buf[y * COLS + x] === SPACE) buf[y * COLS + x] = 45 /*'-'*/;
              }
            }
          }
        }
      }

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

      /* ─── Scene: KD.FX transition ("KD/HOMEBREW" data-mosh bumper) ───
         Plays between every effect as the signature transition: it glitches IN,
         holds the title, then glitches OUT across its (scene-relative) duration. */
      const KDFX_DUR = 4000;
      const GLITCH_TEXT = "KD/HOMEBREW";
      const GLITCH_POOL = "#@%&*+=<>/\\|01.:;~^?!";
      function drawGlitchText(now) {
        clearBuf();
        // Scene-relative progress: corruption is heavy at the start (glitch-in)
        // and end (glitch-out), and clears in the middle so the title reads.
        const p = Math.min(1, Math.max(0, (now - sceneStart) / KDFX_DUR));
        const edge = Math.max(1 - p / 0.32, (p - 0.68) / 0.32, 0); // 1 at edges → 0 mid
        const corrupting = edge > 0.02;
        const recoveryT = edge * 1.5;
        const textRow = Math.max(0, Math.min(ROWS - 1, (ROWS * bannerRowFrac) | 0));
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

      /* ─── BBS ticker scroller (bottom -> top) ─── */
      const tickerEl = cfg.ticker || document.getElementById('fx-ticker');
      let tickerY = 0;
      let tickerHalfHeight = 0;
      const TICKER_SPEED = isMobile ? 16 : 20; // pixels per second
      function initTicker() {
        if (!tickerEl) return;
        renderTicker(tickerEl);
        // half-height = one copy
        tickerHalfHeight = tickerEl.scrollHeight / 2;
        // start so first line sits at the bottom of the visible band
        const wrap = tickerEl.parentElement;
        const visibleH = wrap ? wrap.clientHeight : 0;
        tickerY = -visibleH;
        tickerEl.style.transform = 'translateY(' + (-tickerY) + 'px)';
      }
      function stepTicker(dt) {
        if (!tickerEl || !tickerHalfHeight) return;
        tickerY += (dt / 1000) * TICKER_SPEED;
        if (tickerY >= tickerHalfHeight) tickerY -= tickerHalfHeight;
        tickerEl.style.transform = 'translateY(' + (-tickerY) + 'px)';
      }
      initTicker();

      /* ─── Commit buf → pre.textContent ─── */
      function commit() {
        for (let y = 0; y < ROWS; y++) {
          const start = y * COLS;
          rowStrs[y] = String.fromCharCode.apply(null, buf.subarray(start, start + COLS));
        }
        canvas.textContent = rowStrs.join('\n');
      }

      /* ─── Scene director ─── */
      const SCENES = [
        { name: 'PLASMA',     fn: drawPlasma,     dur: 7000, cls: 'fx-plasma' },
        { name: 'CUBE',       fn: drawCube,       dur: 6000, cls: 'fx-cube' },
        { name: 'STARFIELD',  fn: drawStars,      dur: 6000, cls: 'fx-starfield' },
        { name: 'WARP',       fn: drawWarp,       dur: 6000, cls: 'fx-warp' },
        { name: 'VECTORS',    fn: drawVectors,    dur: 7000, cls: 'fx-vectors' },
        { name: 'RIPPLE',     fn: drawRipple,     dur: 6000, cls: 'fx-ripple' },
        { name: 'CHECKER',    fn: drawChecker,    dur: 7000, cls: 'fx-checker' },
        { name: 'FIRE',       fn: drawFire,       dur: 6000, cls: 'fx-fire' },
        { name: 'LANDSCAPE',  fn: drawLandscape,  dur: 7000, cls: 'fx-landscape' },
        { name: 'LIFE',       fn: drawLife,       dur: 7000, cls: 'fx-life' },
        { name: 'TUNNEL',     fn: drawTunnel,     dur: 6000, cls: 'fx-tunnel' },
        { name: 'VOXEL',      fn: drawVoxel,      dur: 8000, cls: 'fx-voxel' },
        { name: 'COPPER',     fn: drawCopper,     dur: 6000, cls: 'fx-copper' },
        { name: 'ROTOZOOM',   fn: drawRotozoom,   dur: 7000, cls: 'fx-rotozoom' },
        { name: 'TORUS',      fn: drawTorus,      dur: 7000, cls: 'fx-torus' },
        { name: 'TWISTER',    fn: drawTwister,    dur: 6000, cls: 'fx-twister' },
        { name: 'KALEIDO',    fn: drawKaleido,    dur: 6000, cls: 'fx-kaleido' },
        { name: 'SPHERE3D',   fn: drawSphere3d,   dur: 7000, cls: 'fx-sphere3d' },
        { name: 'MATRIX',     fn: drawMatrix,     dur: 5000, cls: 'fx-matrix' },
        { name: 'MOIRE',      fn: drawMoire,      dur: 6000, cls: 'fx-moire' },
        { name: 'CELLULAR',   fn: drawCellular,   dur: 7000, cls: 'fx-cellular' },
        { name: 'WAVE3D',     fn: drawWave3d,     dur: 7000, cls: 'fx-wave3d' },
        { name: 'RIBBON',     fn: drawRibbon,     dur: 7000, cls: 'fx-ribbon' },
        { name: 'METABALLS',  fn: drawMetaballs,  dur: 7000, cls: 'fx-meta' },
        { name: 'MANDELBROT', fn: drawMandelbrot, dur: 7000, cls: 'fx-mandel' },
        { name: 'SPIRAL',     fn: drawSpiral,     dur: 6000, cls: 'fx-spiral' },
        { name: 'SHADEBOBS',  fn: drawShadebobs,  dur: 7000, cls: 'fx-shadebobs' },
        { name: 'BOBS',       fn: drawBobs,       dur: 6000, cls: 'fx-bobs' },
        { name: 'FOUNTAIN',   fn: drawFountain,   dur: 7000, cls: 'fx-fountain' },
        { name: 'SNAKE',      fn: drawSnake,      dur: 7000, cls: 'fx-snake' },
        { name: 'LIGHTNING',  fn: drawLightning,  dur: 6000, cls: 'fx-lightning' },
        { name: 'RAIN',       fn: drawRain,       dur: 7000, cls: 'fx-rain' },
        { name: 'CLIFFORD',   fn: drawClifford,   dur: 7000, cls: 'fx-clifford' },
        { name: 'LORENZ',     fn: drawLorenz,     dur: 8000, cls: 'fx-lorenz' },
        { name: 'TREE',       fn: drawFractalTree,dur: 7000, cls: 'fx-tree' },
        { name: 'MANDELBOX',  fn: drawMandelbox,  dur: 7000, cls: 'fx-mbox' },
        { name: 'HILBERT',    fn: drawHilbert,    dur: 8000, cls: 'fx-hilbert' },
        { name: 'JULIA',      fn: drawJulia,      dur: 7000, cls: 'fx-julia' },
        { name: 'REACTION',   fn: drawRD,         dur: 8000, cls: 'fx-rd' },
        { name: 'VORONOI',    fn: drawVoronoi,    dur: 7000, cls: 'fx-voronoi' },
        { name: 'HEXGRID',    fn: drawHexGrid,    dur: 6000, cls: 'fx-hex' },
        { name: 'HIGHLIFE',   fn: drawHighLife,   dur: 7000, cls: 'fx-highlife' },
        { name: 'RAYMARCH',   fn: drawRaymarch,   dur: 8000, cls: 'fx-sdf' },
        { name: 'DNA',        fn: drawDnaHelix,   dur: 7000, cls: 'fx-dna' },
        { name: 'GALAXY',     fn: drawGalaxy,     dur: 7000, cls: 'fx-galaxy' },
        { name: 'KD.FX',      fn: drawGlitchText, dur: KDFX_DUR, cls: 'fx-kdfx' },
      ];
      // KD.FX is the transition scene woven between every random effect.
      const KDFX_IDX = SCENES.findIndex(s => s.name === 'KD.FX');
      let sceneIdx = KDFX_IDX; // KD.FX opens the show, then alternates with random effects
      let sceneStart = 0;
      let rafId = 0;
      let lastFrame = 0;
      let running = true;
      let skipFrame = false;
      const DISSOLVE_MS = 900;
      const startTime = performance.now();

      // Bag-style random scene picker: play through every scene once before any repeats.
      let sceneBag = [];
      if (cfg.installDebug) window.__fxBagHistory = [];
      function refillBag(excludeIdx) {
        // KD.FX is the transition between effects, not a random scene — keep it out of the bag.
        sceneBag = SCENES.map((_, i) => i).filter(i => i !== KDFX_IDX);
        for (let i = sceneBag.length - 1; i > 0; i--) {
          const j = (Math.random() * (i + 1)) | 0;
          const t = sceneBag[i]; sceneBag[i] = sceneBag[j]; sceneBag[j] = t;
        }
        // If the next pop (end of array) matches the just-played scene,
        // swap it with the first entry so we don't get a back-to-back repeat.
        if (sceneBag.length > 1 && sceneBag[sceneBag.length - 1] === excludeIdx) {
          const t = sceneBag[0]; sceneBag[0] = sceneBag[sceneBag.length - 1]; sceneBag[sceneBag.length - 1] = t;
        }
        if (cfg.installDebug) window.__fxBagHistory.push({ size: sceneBag.length, snapshot: sceneBag.slice(), excludeIdx });
      }
      let prevRandomIdx = sceneIdx; // last real (non-transition) scene, for anti-repeat across refills
      function nextSceneFromBag() {
        if (sceneBag.length === 0) refillBag(prevRandomIdx);
        prevRandomIdx = sceneBag.pop();
        return prevRandomIdx;
      }
      // Seed the bag. KD.FX opens (it's not in the bag), so no random effect
      // needs excluding from round 1 — the first random pick can be anything.
      refillBag(sceneIdx);

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
      setSceneClass(SCENES[sceneIdx].cls, false);
      if (sceneLabel) sceneLabel.textContent = SCENES[sceneIdx].name;

      function frame(now) {
        if (!running) return;
        const dt = lastFrame ? Math.min(64, now - lastFrame) : 16;
        lastFrame = now;
        if (!sceneStart) sceneStart = now;
        const elapsed = now - sceneStart;
        const cur = SCENES[sceneIdx];

        // Advance scene: KD.FX transition alternates with bag-style random effects
        // (every random effect plays once before any repeats).
        if (elapsed >= cur.dur) {
          prevBuf.set(buf);
          hasPrev = true;
          // KD.FX is the transition: alternate <random FX> → KD.FX → <random FX> → …
          sceneIdx = (sceneIdx === KDFX_IDX) ? nextSceneFromBag() : KDFX_IDX;
          sceneStart = now;
          const next = SCENES[sceneIdx];
          setSceneClass(next.cls, true);
          if (sceneLabel) sceneLabel.textContent = next.name;
          if (cfg.installDebug && window.__fxTransitionLog) window.__fxTransitionLog.push(next.name);
        }

        // Mobile frame-skip: if dt > 28ms and we haven't just skipped, render scene but allow skipping next
        if (isMobile && dt > 30 && !skipFrame) {
          skipFrame = true;
          rafId = requestAnimationFrame(frame);
          return;
        }
        skipFrame = false;

        // Draw current scene
        SCENES[sceneIdx].fn(now, dt);

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

        // Dissolve from previous scene during first DISSOLVE_MS
        if (hasPrev) {
          const wipeElapsed = now - sceneStart;
          if (wipeElapsed < DISSOLVE_MS) {
            const p = 1 - (wipeElapsed / DISSOLVE_MS);
            const sz = buf.length;
            for (let i = 0; i < sz; i++) {
              if (Math.random() < p) buf[i] = prevBuf[i];
            }
          } else {
            hasPrev = false;
          }
        }

        commit();
        stepTicker(dt);

        if (timerLabel) {
          const secs = ((now - startTime) / 1000).toFixed(1);
          timerLabel.textContent = 'T+' + secs + 's';
        }

        rafId = requestAnimationFrame(frame);
      }

      function teardown() {
        running = false;
        if (rafId) { cancelAnimationFrame(rafId); rafId = 0; }
        document.removeEventListener('visibilitychange', onVisibility);
        if (ro) ro.disconnect();
        if (resizeTimer) clearTimeout(resizeTimer);
      }

      if (cfg.installDebug) {
        // Debug hook: jump to any scene by name. Used by the test harness.
        window.__fxJump = function(name) {
          const i = SCENES.findIndex(s => s.name === name);
          if (i < 0) return false;
          prevBuf.set(buf);
          hasPrev = true;
          sceneIdx = i;
          sceneStart = performance.now();
          setSceneClass(SCENES[i].cls, false);
          if (sceneLabel) sceneLabel.textContent = SCENES[i].name;
          return true;
        };
        // Freeze on a scene so cycling doesn't race the test harness.
        window.__fxFreeze = function(name) {
          for (const s of SCENES) s.dur = 9_999_999;
          if (name) return window.__fxJump(name);
          return true;
        };
        // Force every scene to a given duration in ms — used by the test harness
        // to observe many transitions quickly.
        window.__fxSpeedMs = function(ms) {
          for (const s of SCENES) s.dur = ms;
          return true;
        };
        window.__fxList = () => SCENES.map(s => s.name);
        window.__fxBag = () => sceneBag.map(i => SCENES[i].name);
        window.__fxBagSize = () => sceneBag.length;
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
      }

      if (reducedMotion) {
        // Static: one frame of plasma, no animation loop, ticker stays at top
        drawPlasma(performance.now());
        commit();
        if (sceneLabel) sceneLabel.textContent = 'STATIC';
      } else {
        // Seed with one frame so the box isn't empty during first paint
        drawPlasma(performance.now());
        commit();
        rafId = requestAnimationFrame(frame);
      }

      // Stop animation as soon as the entry gate is dismissed
      if (gate) {
        const obs = new MutationObserver(function() {
          if (gate.classList.contains('dismissing')) {
            teardown();
            obs.disconnect();
          }
        });
        obs.observe(gate, { attributes: true, attributeFilter: ['class'] });
      }

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

      // Pause when tab hidden to save CPU
      function onVisibility() {
        if (!running) return;
        if (document.hidden) {
          if (rafId) { cancelAnimationFrame(rafId); rafId = 0; }
        } else {
          if (!rafId) {
            lastFrame = 0;
            rafId = requestAnimationFrame(frame);
          }
        }
      }
      document.addEventListener('visibilitychange', onVisibility);

      // Expose the NFO ticker lines ([cls, txt] pairs) so a consumer can render
      // them however it likes (e.g. a teletype stream) instead of the built-in
      // scroller. Same content the scroller uses.
      return { stop: teardown, tickerLines: TICKER_LINES };
    };
