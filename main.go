package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ─── System Stats ───

type DiskInfo struct {
	Name       string  `json:"name"`
	MountPoint string  `json:"mount_point"`
	FSType     string  `json:"fs_type"`
	TotalGB    float64 `json:"total_gb"`
	UsedGB     float64 `json:"used_gb"`
	AvailGB    float64 `json:"avail_gb"`
	UsePct     float64 `json:"use_pct"`
}

type SystemStats struct {
	Uptime      string     `json:"uptime"`
	LoadAvg     string     `json:"load_avg"`
	CPUModel    string     `json:"cpu_model"`
	CPUCores    int        `json:"cpu_cores"`
	MemTotalMB  float64    `json:"mem_total_mb"`
	MemUsedMB   float64    `json:"mem_used_mb"`
	MemAvailMB  float64    `json:"mem_avail_mb"`
	MemUsePct   float64    `json:"mem_use_pct"`
	Disks       []DiskInfo `json:"disks"`
	CollectedAt string     `json:"collected_at"`
}

var (
	cachedJSON []byte
	statsMu    sync.RWMutex
	procPath   = "/proc"
)

func init() {
	if _, err := os.Stat("/host/proc/uptime"); err == nil {
		procPath = "/host/proc"
	}
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func getUptime() string {
	content, err := readFile(filepath.Join(procPath, "uptime"))
	if err != nil {
		return "unknown"
	}
	parts := strings.Fields(content)
	if len(parts) < 1 {
		return "unknown"
	}
	secs, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return "unknown"
	}
	days := int(secs) / 86400
	hours := (int(secs) % 86400) / 3600
	mins := (int(secs) % 3600) / 60
	return fmt.Sprintf("%d days, %02d:%02d", days, hours, mins)
}

func getLoadAvg() string {
	content, err := readFile(filepath.Join(procPath, "loadavg"))
	if err != nil {
		return "unknown"
	}
	parts := strings.Fields(content)
	if len(parts) >= 3 {
		return strings.Join(parts[:3], ", ")
	}
	return content
}

func getCPUInfo() (string, int) {
	content, err := readFile(filepath.Join(procPath, "cpuinfo"))
	if err != nil {
		return "unknown", 0
	}
	model := ""
	cores := 0
	for _, line := range strings.Split(content, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "model name":
			model = val
		case "Model":
			// ARM/Raspberry Pi: use "Model" if "model name" wasn't found
			if model == "" {
				model = val
			}
		case "Hardware":
			if model == "" {
				model = val
			}
		case "processor":
			cores++
		}
	}
	if model == "" {
		model = "unknown"
	}
	return model, cores
}


func getMemInfo() (total, used, avail, pct float64) {
	content, err := readFile(filepath.Join(procPath, "meminfo"))
	if err != nil {
		return
	}
	values := make(map[string]float64)
	for _, line := range strings.Split(content, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			val, err := strconv.ParseFloat(parts[1], 64)
			if err == nil {
				values[key] = val
			}
		}
	}
	total = values["MemTotal"] / 1024
	avail = values["MemAvailable"] / 1024
	used = total - avail
	if total > 0 {
		pct = (used / total) * 100
	}
	return
}

func getDisks() []DiskInfo {
	// Read partition sizes from /proc/partitions (works without mount access)
	partContent, err := readFile(filepath.Join(procPath, "partitions"))
	if err != nil {
		return nil
	}
	// Map partition name -> size in bytes
	partSizes := make(map[string]float64)
	for _, line := range strings.Split(partContent, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		blocks, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		partSizes[fields[3]] = blocks * 1024 // /proc/partitions uses 1K blocks
	}

	// Read mounts to map devices to mount points and filesystem types
	mountContent, err := readFile(filepath.Join(procPath, "mounts"))
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var disks []DiskInfo

	for _, line := range strings.Split(mountContent, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		device := parts[0]
		fsType := parts[2]

		if !strings.HasPrefix(device, "/dev/") {
			continue
		}
		if strings.Contains(device, "loop") || strings.Contains(device, "ram") {
			continue
		}
		if seen[device] {
			continue
		}
		seen[device] = true

		// Extract partition name (e.g. /dev/sda1 -> sda1)
		driveName := device
		if idx := strings.LastIndex(device, "/"); idx >= 0 {
			driveName = device[idx+1:]
		}

		totalBytes, ok := partSizes[driveName]
		if !ok || totalBytes < 0.1*(1<<30) {
			continue
		}

		disks = append(disks, DiskInfo{
			Name:    driveName,
			FSType:  fsType,
			TotalGB: math.Round(totalBytes/(1<<30)*100) / 100,
		})
	}
	return disks
}

func collectStats() *SystemStats {
	uptime := getUptime()
	loadAvg := getLoadAvg()
	cpuModel, cpuCores := getCPUInfo()
	memTotal, memUsed, memAvail, memPct := getMemInfo()
	disks := getDisks()

	return &SystemStats{
		Uptime:      uptime,
		LoadAvg:     loadAvg,
		CPUModel:    cpuModel,
		CPUCores:    cpuCores,
		MemTotalMB:  math.Round(memTotal*10) / 10,
		MemUsedMB:   math.Round(memUsed*10) / 10,
		MemAvailMB:  math.Round(memAvail*10) / 10,
		MemUsePct:   math.Round(memPct*10) / 10,
		Disks:       disks,
		CollectedAt: time.Now().Format(time.RFC3339),
	}
}

func refreshStats() {
	stats := collectStats()
	data, err := json.Marshal(stats)
	if err != nil {
		log.Printf("[STATS] marshal error: %v", err)
		return
	}
	statsMu.Lock()
	cachedJSON = data
	statsMu.Unlock()
	log.Printf("[STATS] refreshed — mem=%.0f%%", stats.MemUsePct)
}

func refreshLoop() {
	for {
		time.Sleep(1 * time.Hour)
		refreshStats()
	}
}

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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	statsMu.RLock()
	data := cachedJSON
	statsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
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

	// Initialize systems
	initVisitDB(dataDir)
	refreshStats()
	go refreshLoop()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/stats", statsHandler)
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
