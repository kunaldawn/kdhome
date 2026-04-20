package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ─── Visit Counter (SQLite) ───

var (
	visitDB    *sql.DB
	visitCount int64 // atomic, cached in memory
)

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

	_, err = visitDB.Exec(`CREATE TABLE IF NOT EXISTS visits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Printf("[VISITS] failed to create table: %v", err)
		return
	}

	// Load current count into memory
	var count int64
	visitDB.QueryRow("SELECT COUNT(*) FROM visits").Scan(&count)
	atomic.StoreInt64(&visitCount, count)
	log.Printf("[VISITS] initialized — total: %d", count)
}

func recordVisit() {
	if visitDB == nil {
		return
	}
	go func() {
		_, err := visitDB.Exec("INSERT INTO visits (ts) VALUES (CURRENT_TIMESTAMP)")
		if err != nil {
			log.Printf("[VISITS] insert error: %v", err)
			return
		}
		atomic.AddInt64(&visitCount, 1)
	}()
}

func visitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		recordVisit()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `{"count":%d}`, atomic.LoadInt64(&visitCount))
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `{"count":%d}`, atomic.LoadInt64(&visitCount))
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"img-src 'self' data:; "+
				"media-src 'self' blob:; "+
				"worker-src 'self' blob:; "+
				"connect-src 'self' https://www.google-analytics.com https://analytics.google.com; "+
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

	mux := http.NewServeMux()
	mux.HandleFunc("/api/visit", visitHandler)
	mux.HandleFunc("/api/playlist", playlistHandler(staticDir))
	mux.Handle("/", http.FileServer(noDirFS{http.Dir(staticDir)}))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           securityHeaders(mux),
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("[SERVER] serving %s on :%s", staticDir, port)
	log.Fatal(srv.ListenAndServe())
}
