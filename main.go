package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ─── Visit Counter (SQLite) ───

var (
	visitDB    *sql.DB
	humanCount int64 // atomic, cached in memory
	botCount   int64 // atomic, cached in memory
)

// archives mirrors the JS ARCHIVES registry in static/index.html.
// Both sides need the seven IDs and URLs; with seven entries that change
// rarely, mirroring beats a JSON file shared by both runtimes.
var archives = []struct {
	ID, URL string
}{
	{"wiki", "https://wiki.kunaldawn.com"},
	{"pdf", "https://pdf.kunaldawn.com"},
	{"os", "https://os.kunaldawn.com"},
	{"iso", "https://iso.kunaldawn.com"},
	{"chiptune", "https://chiptune.kunaldawn.com"},
	{"tube", "https://tube.kunaldawn.com"},
	{"audio", "https://audio.kunaldawn.com"},
}


func initVisitDB(dataDir string) {
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "visits.db")

	var err error
	visitDB, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Printf("[VISITS] failed to open db: %v", err)
		return
	}

	visitDB.SetMaxOpenConns(1) // SQLite is single-writer
	visitDB.SetMaxIdleConns(1)

	// Single-row aggregate table. Replaces the old per-visit row append,
	// which grew without bound and made startup `SELECT COUNT(*)` linear.
	_, err = visitDB.Exec(`CREATE TABLE IF NOT EXISTS visit_count (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		n  INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		log.Printf("[VISITS] failed to create visit_count: %v", err)
		return
	}
	if _, err = visitDB.Exec(`INSERT OR IGNORE INTO visit_count (id, n) VALUES (1, 0)`); err != nil {
		log.Printf("[VISITS] failed to seed visit_count: %v", err)
		return
	}

	// Add the human/bot split columns. ADD COLUMN errors on the second boot
	// ("duplicate column name"), which is benign; any other error is real and
	// would leave the columns absent, so surface it. The old `n` column is
	// retired — no longer read/written. (col is a fixed constant set here.)
	for _, col := range []string{"human", "bot"} {
		if _, err := visitDB.Exec(`ALTER TABLE visit_count ADD COLUMN ` + col + ` INTEGER NOT NULL DEFAULT 0`); err != nil &&
			!strings.Contains(err.Error(), "duplicate column name") {
			log.Printf("[VISITS] failed to add column %s: %v", col, err)
		}
	}

	// One-time reset, gated by PRAGMA user_version so restarts never re-zero
	// accumulating counts. The pre-existing total can't be split (no UA was
	// ever stored), so the human/bot tally starts clean at the 0 → 1 bump.
	var userVersion int
	visitDB.QueryRow(`PRAGMA user_version`).Scan(&userVersion)
	if userVersion < 1 {
		if _, err := visitDB.Exec(`UPDATE visit_count SET n = 0, human = 0, bot = 0 WHERE id = 1`); err != nil {
			log.Printf("[VISITS] reset migration failed: %v", err)
		} else if _, err := visitDB.Exec(`PRAGMA user_version = 1`); err != nil {
			// Counters were reset but the gate didn't advance; without this
			// bump the next restart would reset them again. Surface it.
			log.Printf("[VISITS] failed to bump user_version after reset: %v", err)
		} else {
			log.Printf("[VISITS] reset visit counters to 0 (human/bot split migration)")
		}
	}

	var human, bot int64
	visitDB.QueryRow(`SELECT human, bot FROM visit_count WHERE id = 1`).Scan(&human, &bot)
	atomic.StoreInt64(&humanCount, human)
	atomic.StoreInt64(&botCount, bot)

	// Daily probe rollup table. One row per (UTC date, archive_id). Updated
	// via UPSERT from the status probe; aggregates total/ok probe counts
	// and latency stats so the 90-day uptime ribbon can be rendered from
	// ~630 rows max. Pruned to a 90-day window on each probe tick.
	_, err = visitDB.Exec(`CREATE TABLE IF NOT EXISTS probe_daily (
		date        TEXT NOT NULL,
		archive_id  TEXT NOT NULL,
		ok_count    INTEGER NOT NULL DEFAULT 0,
		total_count INTEGER NOT NULL DEFAULT 0,
		lat_sum_ms  INTEGER NOT NULL DEFAULT 0,
		lat_max_ms  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (date, archive_id)
	)`)
	if err != nil {
		log.Printf("[PROBE] failed to create probe_daily: %v", err)
	}

	log.Printf("[VISITS] initialized — humans: %d, bots: %d", human, bot)
}

// botUA matches self-declaring crawlers, monitors, HTTP libraries, and
// link-preview fetchers. It's a deny-list: real browser User-Agents carry none
// of these tokens and fall through to "human". Grouped only for maintenance —
// order within the alternation is irrelevant. All tokens are literal (no regexp
// metacharacters), so none need escaping.
var botUA = regexp.MustCompile(`(?i)` + strings.Join([]string{
	// generic
	"bot", "crawl", "spider", "slurp", "scrape",
	// AI / research
	"gptbot", "oai-searchbot", "chatgpt", "claudebot", "anthropic-ai",
	"ccbot", "perplexitybot", "google-extended", "bytespider", "amazonbot",
	"applebot", "diffbot", "cohere-ai", "imagesiftbot", "omgili",
	// search
	"googlebot", "bingbot", "duckduckbot", "baiduspider", "yandexbot",
	// SEO
	"ahrefsbot", "semrushbot", "mj12bot", "dotbot", "dataforseo",
	// monitors
	"uptimerobot", "pingdom", "statuscake", "site24x7",
	// HTTP libs / headless
	"curl", "wget", "libwww", "python-requests", "go-http-client", "okhttp",
	"java/", "node-fetch", "axios", "headlesschrome", "phantomjs",
	// link preview
	"facebookexternalhit", "twitterbot", "slackbot", "discordbot",
	// NOTE: "whatsapp" deliberately excluded — WhatsApp's in-app browser
	// (a real human) shares the "WhatsApp/" UA token with its link-unfurl
	// bot, so matching it would undercount real visitors.
	"telegrambot", "linkedinbot", "embedly", "redditbot",
}, "|"))

// isBot reports whether a request's User-Agent looks automated. An empty or
// whitespace-only UA counts as a bot (scripts that send no UA).
func isBot(ua string) bool {
	if strings.TrimSpace(ua) == "" {
		return true
	}
	return botUA.MatchString(ua)
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
		// col is from a fixed two-value set (never user input), so building the
		// SQL by concatenation is safe here.
		if _, err := visitDB.Exec(`UPDATE visit_count SET ` + col + ` = ` + col + ` + 1 WHERE id = 1`); err != nil {
			log.Printf("[VISITS] update error (%s): %v", col, err)
			return
		}
		atomic.AddInt64(ctr, 1)
	}()
}

func visitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `{"humans":%d,"bots":%d}`,
		atomic.LoadInt64(&humanCount), atomic.LoadInt64(&botCount))
}

// ─── Middleware & Handlers ───

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://www.googletagmanager.com https://www.google-analytics.com; "+
				"style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; "+
				"img-src 'self' data:; "+
				"media-src 'self' blob:; "+
				"worker-src 'self' blob:; "+
				"connect-src 'self' https://www.google-analytics.com https://analytics.google.com; "+
				"frame-src https://wiki.kunaldawn.com https://pdf.kunaldawn.com https://os.kunaldawn.com https://iso.kunaldawn.com https://chiptune.kunaldawn.com https://tube.kunaldawn.com https://audio.kunaldawn.com; "+
				"frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// ─── Playlist Scanner ───

type PlaylistTrack struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

var trackerExts = map[string]bool{
	".mod": true, ".xm": true, ".s3m": true, ".it": true,
	".mptm": true, ".stm": true, ".med": true, ".mtm": true,
}

func playlistHandler(staticDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		musicDir := filepath.Join(staticDir, "music")
		entries, err := os.ReadDir(musicDir)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		var tracks []PlaylistTrack
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !trackerExts[ext] {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			// Clean up name: replace underscores/hyphens with spaces, title case
			name = strings.ReplaceAll(name, "_", " ")
			name = strings.ReplaceAll(name, "-", " ")
			tracks = append(tracks, PlaylistTrack{
				URL:  "/music/" + e.Name(),
				Name: name,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		json.NewEncoder(w).Encode(tracks)
	}
}

type noDirFS struct {
	fs http.FileSystem
}

func (n noDirFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		index := filepath.Join(name, "index.html")
		if _, err := n.fs.Open(index); err != nil {
			f.Close()
			return nil, fs.ErrNotExist
		}
	}
	return f, nil
}

// ─── Static cache: in-memory + pre-gzipped + ETag + Cache-Control ───

// Extensions whose payloads compress well. Skipped for already-compressed
// formats (webp, png, mp3, woff2, tracker modules, etc).
var compressibleExt = map[string]bool{
	".html":        true,
	".js":          true,
	".css":         true,
	".json":        true,
	".svg":         true,
	".webmanifest": true,
	".txt":         true,
	".xml":         true,
	".wasm":        true,
	".mem":         true,
	".map":         true,
}

// Tracker modules are streamed straight from disk by FileServer rather than
// being held in the in-memory cache — they're binary and add up to multiple MB.
var trackerExt = map[string]bool{
	".mod": true, ".xm": true, ".s3m": true, ".it": true,
	".mptm": true, ".stm": true, ".med": true, ".mtm": true,
}

type cachedFile struct {
	contentType  string
	raw          []byte
	gz           []byte // nil if not worth gzipping
	etag         string
	modTime      time.Time
	cacheControl string
}

var staticCache map[string]*cachedFile

// cacheControlFor returns a Cache-Control value based on the URL path. Assets
// whose contents change only via a deploy get long max-age + immutable; the
// HTML shell and crawler-facing files get a short max-age so updates aren't
// stuck behind stale browser caches.
func cacheControlFor(urlPath string) string {
	switch {
	case urlPath == "/" || urlPath == "/index.html",
		urlPath == "/site.webmanifest",
		urlPath == "/sitemap.xml",
		urlPath == "/robots.txt",
		urlPath == "/llms.txt":
		return "public, max-age=300, must-revalidate"
	case strings.HasPrefix(urlPath, "/music/"),
		strings.HasPrefix(urlPath, "/fonts/"),
		strings.HasPrefix(urlPath, "/infra"),
		urlPath == "/og-image.png",
		strings.HasPrefix(urlPath, "/favicon"),
		urlPath == "/apple-touch-icon.png",
		strings.HasPrefix(urlPath, "/android-chrome-"):
		return "public, max-age=31536000, immutable"
	default:
		return "public, max-age=3600"
	}
}

// buildStaticCache walks staticDir, loads every non-tracker file under 8 MiB
// into memory, computes a content-hash ETag, and pre-gzips compressible types
// at gzip.BestCompression.
func buildStaticCache(staticDir string) (map[string]*cachedFile, error) {
	cache := map[string]*cachedFile{}
	err := filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if trackerExt[ext] {
			return nil
		}
		if info.Size() > 8*1024*1024 {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(staticDir, path)
		urlPath := "/" + filepath.ToSlash(rel)

		ct := mime.TypeByExtension(ext)
		if ct == "" {
			ct = http.DetectContentType(data)
		}

		sum := sha256.Sum256(data)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`

		var gz []byte
		if compressibleExt[ext] {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(data)
			zw.Close()
			if buf.Len() < len(data) {
				gz = buf.Bytes()
			}
		}

		cache[urlPath] = &cachedFile{
			contentType:  ct,
			raw:          data,
			gz:           gz,
			etag:         etag,
			modTime:      info.ModTime().UTC(),
			cacheControl: cacheControlFor(urlPath),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if idx, ok := cache["/index.html"]; ok {
		cache["/"] = idx
	}
	return cache, nil
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name := strings.TrimSpace(part)
		if i := strings.IndexByte(name, ';'); i >= 0 {
			name = strings.TrimSpace(name[:i])
		}
		if name == "gzip" {
			return true
		}
	}
	return false
}

// staticHandler serves cached files with negotiated gzip + ETag-based
// conditional GET. Anything not in the cache (e.g. tracker modules dropped
// into static/music/ after startup) falls through to the disk-backed
// FileServer.
func staticHandler(fallback http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fallback.ServeHTTP(w, r)
			return
		}

		// Server-side visit counter. Counts every GET to the index page
		// (including conditional 304 responses below) so cache, bfcache, and
		// no-JS clients are all reflected; isBot(UA) routes each hit into the
		// human or bot bucket. Skips HEAD on purpose — HEAD is metadata-only,
		// not a real page view.
		if r.Method == http.MethodGet &&
			(r.URL.Path == "/" || r.URL.Path == "/index.html") {
			recordVisit(isBot(r.UserAgent()))
		}

		cf, ok := staticCache[r.URL.Path]
		if !ok {
			// Disk fallback gets the same Cache-Control treatment, but
			// nothing else — it's the slow path for unfingerprinted files.
			w.Header().Set("Cache-Control", cacheControlFor(r.URL.Path))
			w.Header().Add("Vary", "Accept-Encoding")
			fallback.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		h.Set("ETag", cf.etag)
		h.Set("Cache-Control", cf.cacheControl)
		h.Add("Vary", "Accept-Encoding")

		// Conditional GET. ETag wins over If-Modified-Since per RFC 7232.
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			if inm == cf.etag || inm == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		} else if ims := r.Header.Get("If-Modified-Since"); ims != "" {
			if t, err := http.ParseTime(ims); err == nil &&
				!cf.modTime.Truncate(time.Second).After(t) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		h.Set("Content-Type", cf.contentType)
		h.Set("Last-Modified", cf.modTime.Format(http.TimeFormat))

		body := cf.raw
		if cf.gz != nil && acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			body = cf.gz
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))

		if r.Method == http.MethodHead {
			return
		}
		w.Write(body)
	}
}

// ─── Main ───

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	staticDir := "./static"

	// Register MIME types Go's mime package doesn't know by default.
	// X-Content-Type-Options: nosniff forces browsers to honour these, so
	// the PWA manifest needs the correct type or installability breaks.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	// Initialize systems
	initVisitDB(dataDir)

	// Pre-load and pre-gzip static assets so we serve compressed bytes
	// without paying CPU per request on the Pi.
	cache, err := buildStaticCache(staticDir)
	if err != nil {
		log.Fatalf("[STATIC] cache build failed: %v", err)
	}
	staticCache = cache
	log.Printf("[STATIC] cached %d files", len(cache))

	// Background probe for /api/status.json. Primes the cache before
	// the server starts taking traffic, then refreshes every 30 s.
	startStatusProbe()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/visit", visitHandler)
	mux.HandleFunc("/api/playlist", playlistHandler(staticDir))
	mux.HandleFunc("/api/status.json", statusJSONHandler)
	mux.HandleFunc("/api/status/history.json", statusHistoryHandler)
	mux.Handle("/", staticHandler(http.FileServer(noDirFS{http.Dir(staticDir)})))

	// Auth (env-gated). When enabled, register the OAuth routes and gate the
	// whole site; when AUTH_ENABLED is on but secrets are missing, refuse to
	// start rather than silently serving an ungated site.
	authCfg := loadAuthConfig()
	if authCfg.Enabled {
		if !authCfg.valid() {
			log.Fatal("[AUTH] AUTH_ENABLED is set but GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, or AUTH_SECRET is missing")
		}
		mux.HandleFunc("/login", authCfg.handleLogin)
		mux.HandleFunc("/auth/google/start", authCfg.handleGoogleStart)
		mux.HandleFunc("/auth/google/callback", authCfg.handleGoogleCallback)
		mux.HandleFunc("/logout", authCfg.handleLogout)
		log.Printf("[AUTH] Google auth ENABLED (cookie domain %s)", authCfg.CookieDomain)
	}

	// Order: maintenance( securityHeaders( authGate( mux ) ) ). securityHeaders
	// stays outermost (after maintenance) so the login page AND the gate's
	// redirect responses both carry the security headers.
	var handler http.Handler = mux
	if authCfg.Enabled {
		handler = authCfg.middleware(handler)
	}
	handler = securityHeaders(handler)

	// Maintenance mode (env-gated). When enabled, the middleware wraps the
	// whole chain and serves a themed 503 for every request; when disabled
	// it's never installed, so there's zero request-path overhead.
	if mcfg := loadMaintenanceConfig(); mcfg.Enabled {
		handler = maintenanceMiddleware(buildMaintenancePage(mcfg), mcfg)(handler)
		log.Printf("[MAINT] maintenance mode ENABLED (end set: %t)", mcfg.HasEnd)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("[SERVER] serving %s on :%s", staticDir, port)
	log.Fatal(srv.ListenAndServe())
}
