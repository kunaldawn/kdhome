package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// maintenanceConfig captures the env-driven maintenance settings, read once
// at startup. When Enabled is false the middleware is never installed.
type maintenanceConfig struct {
	Enabled bool
	End     time.Time
	HasEnd  bool
	Message string
}

// loadMaintenanceConfig reads MAINTENANCE_MODE / MAINTENANCE_END /
// MAINTENANCE_MESSAGE. MODE is truthy on 1/true/on/yes (case-insensitive).
// END is RFC3339; an unset or unparseable value leaves HasEnd false (page
// renders with no countdown). MESSAGE defaults to a friendly line.
func loadMaintenanceConfig() maintenanceConfig {
	cfg := maintenanceConfig{Message: "We'll be back soon."}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAINTENANCE_MODE"))) {
	case "1", "true", "on", "yes":
		cfg.Enabled = true
	}

	if raw := strings.TrimSpace(os.Getenv("MAINTENANCE_END")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			cfg.End = t.UTC()
			cfg.HasEnd = true
		} else if cfg.Enabled {
			log.Printf("[MAINT] ignoring unparseable MAINTENANCE_END %q: %v", raw, err)
		}
	}

	if msg := strings.TrimSpace(os.Getenv("MAINTENANCE_MESSAGE")); msg != "" {
		cfg.Message = msg
	}

	return cfg
}

// buildMaintenancePage renders the self-contained themed 503 page once at
// startup. It carries inline CSS + JS only (every asset request is blocked
// by the middleware, so it can't depend on external files). When HasEnd is
// set, the embedded ISO timestamp drives a client-side countdown.
func buildMaintenancePage(cfg maintenanceConfig) []byte {
	endISO := ""
	if cfg.HasEnd {
		endISO = cfg.End.Format(time.RFC3339)
	}
	return []byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Under Maintenance — kunaldawn.com</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  html, body { height: 100%%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    background: #05110d;
    color: #cfeee0;
    font-family: 'Share Tech Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
    padding: 24px;
    background-image: radial-gradient(circle at 50%% 0%%, rgba(127,209,179,0.08), transparent 60%%);
  }
  .box {
    width: 100%%; max-width: 560px; text-align: center;
    border: 1px solid rgba(127,209,179,0.35);
    border-radius: 10px;
    padding: 40px 28px;
    background: rgba(10,28,22,0.6);
    box-shadow: 0 0 40px rgba(0,0,0,0.5);
  }
  h1 { font-size: 22px; margin: 0 0 6px; color: #7fd1b3; letter-spacing: 1px; }
  .tag { font-size: 12px; opacity: 0.7; margin-bottom: 22px; }
  .msg { font-size: 15px; line-height: 1.5; margin: 0 0 26px; }
  .countdown { display: flex; gap: 14px; justify-content: center; margin-top: 8px; }
  .unit { min-width: 58px; }
  .num { font-size: 30px; color: #7fd1b3; }
  .lbl { font-size: 10px; text-transform: uppercase; opacity: 0.6; letter-spacing: 1px; }
  .done { font-size: 15px; color: #7fd1b3; }
  .footer { margin-top: 28px; font-size: 11px; opacity: 0.5; }
</style>
</head>
<body>
  <div class="box">
    <h1>// UNDER MAINTENANCE</h1>
    <div class="tag">KD's Homebrew Digital Archive</div>
    <p class="msg">%s</p>
    <div id="cd" class="countdown" hidden>
      <div class="unit"><div class="num" id="cd-d">0</div><div class="lbl">days</div></div>
      <div class="unit"><div class="num" id="cd-h">0</div><div class="lbl">hours</div></div>
      <div class="unit"><div class="num" id="cd-m">0</div><div class="lbl">min</div></div>
      <div class="unit"><div class="num" id="cd-s">0</div><div class="lbl">sec</div></div>
    </div>
    <div id="done" class="done" hidden>back any moment now&hellip;</div>
    <div class="footer">no cloud · no CDN · just a shelf and enough panels to cover the draw</div>
  </div>
  <script>
    (function () {
      var endISO = %q;
      if (!endISO) return;
      var end = new Date(endISO).getTime();
      if (isNaN(end)) return;
      var cd = document.getElementById('cd');
      var done = document.getElementById('done');
      function pad(n) { return n < 10 ? '0' + n : '' + n; }
      function tick() {
        var diff = end - Date.now();
        if (diff <= 0) {
          cd.hidden = true;
          done.hidden = false;
          return;
        }
        cd.hidden = false;
        var s = Math.floor(diff / 1000);
        document.getElementById('cd-d').textContent = Math.floor(s / 86400);
        document.getElementById('cd-h').textContent = pad(Math.floor((s %% 86400) / 3600));
        document.getElementById('cd-m').textContent = pad(Math.floor((s %% 3600) / 60));
        document.getElementById('cd-s').textContent = pad(s %% 60);
      }
      tick();
      setInterval(tick, 1000);
    })();
  </script>
</body>
</html>`, html.EscapeString(cfg.Message), endISO))
}

// maintenanceMiddleware serves the pre-rendered page with 503 + Retry-After
// for every request, short-circuiting the real handler chain entirely.
func maintenanceMiddleware(page []byte, cfg maintenanceConfig) func(http.Handler) http.Handler {
	retry := "3600"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ra := retry
			if cfg.HasEnd {
				if secs := int(time.Until(cfg.End).Seconds()); secs > 0 {
					ra = strconv.Itoa(secs)
				} else {
					ra = "60"
				}
			}
			h := w.Header()
			h.Set("Content-Type", "text/html; charset=utf-8")
			h.Set("Cache-Control", "no-store")
			h.Set("Retry-After", ra)
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				w.Write(page)
			}
		})
	}
}
