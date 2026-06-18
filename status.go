package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

// ─── Status probe ───
//
// Probes each archive subdomain with HEAD on a 1 min ticker. Reachability is
// debounced Kubernetes-style: an archive is only reported down after
// failureThreshold consecutive failed probes, and recovers on the first
// success (successThreshold = 1). Everything is cached so the
// /api/status.json handler does no I/O on the request path. Consumed by
// the in-page status window in static/index.html.

const (
	// probeInterval is the gap between probe sweeps (the "1 min gap").
	probeInterval = 1 * time.Minute
	// failureThreshold is how many consecutive failed probes mark an archive
	// down — like a Kubernetes livenessProbe failureThreshold.
	failureThreshold = 5
	// hideThreshold is how long an archive must stay (debounced) down before
	// it's dropped from the featured tiles / Start menu.
	hideThreshold = 1 * time.Hour
	// recoverWindow is how long a hidden archive must stay up before it's
	// shown again — guards against flapping.
	recoverWindow = 1 * time.Minute
)

type probeResult struct {
	ID        string
	URL       string
	OK        bool
	Status    int
	LatencyMS int64
	CheckedAt time.Time
	Err       string
	Hidden    bool
}

type statusSnapshot struct {
	GeneratedAt time.Time
	Probes      []probeResult
	UptimeSec   int64
}

var (
	statusMu    sync.RWMutex
	statusSnap  statusSnapshot
	statusStart = time.Now()
)

// probeState holds the per-archive debounce counters. effectiveOK is the
// reported (debounced) status; consecFails counts failures since the last
// success; seenSuccess records whether the archive has ever responded OK since
// the process started. State is in-memory and resets on restart.
type probeState struct {
	consecFails int
	effectiveOK bool
	seenSuccess bool
	downSince   time.Time // when effectiveOK last became false (zero when up)
	upSince     time.Time // when effectiveOK last became true (zero when down)
	hidden      bool      // current featured-visibility decision
}

var (
	probeStateMu sync.Mutex
	probeStates  = map[string]*probeState{}
)

// debounceProbe folds a single raw probe result into the archive's running
// state and returns the effective status. A success resets the failure count
// and reports up immediately. Failures only flip an *established* archive down
// once failureThreshold consecutive failures accumulate — but an archive that
// has never succeeded (no "up" baseline to protect, e.g. a dead DNS name right
// after a restart) is reported down on its first failure rather than showing a
// misleading "online".
func debounceProbe(id string, rawOK bool) bool {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	st := probeStates[id]
	if st == nil {
		st = &probeState{}
		probeStates[id] = st
	}
	if rawOK {
		st.consecFails = 0
		st.effectiveOK = true
		st.seenSuccess = true
	} else {
		st.consecFails++
		if st.consecFails >= failureThreshold || !st.seenSuccess {
			st.effectiveOK = false
		}
	}
	updateVisibility(st, time.Now())
	return st.effectiveOK
}

// updateVisibility folds the current (debounced) effectiveOK into the
// hide/restore state machine, using an explicit `now` so it's unit-testable.
// Down for >= hideThreshold hides the archive; once hidden, it's shown again
// only after staying up for >= recoverWindow. Caller holds probeStateMu.
func updateVisibility(st *probeState, now time.Time) {
	if st.effectiveOK {
		if st.upSince.IsZero() {
			st.upSince = now
		}
		st.downSince = time.Time{}
		if st.hidden && now.Sub(st.upSince) >= recoverWindow {
			st.hidden = false
		}
	} else {
		if st.downSince.IsZero() {
			st.downSince = now
		}
		st.upSince = time.Time{}
		if now.Sub(st.downSince) >= hideThreshold {
			st.hidden = true
		}
	}
}

// isHidden reports the current featured-visibility decision for an archive.
func isHidden(id string) bool {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	st := probeStates[id]
	return st != nil && st.hidden
}

func startStatusProbe() {
	refresh := func() {
		next := statusSnapshot{
			GeneratedAt: time.Now().UTC(),
			UptimeSec:   int64(time.Since(statusStart).Seconds()),
			Probes:      make([]probeResult, 0, len(archives)),
		}

		client := &http.Client{Timeout: 3 * time.Second}
		var wg sync.WaitGroup
		resCh := make(chan probeResult, len(archives))
		for _, a := range archives {
			wg.Add(1)
			go func(id, url string) {
				defer wg.Done()
				resCh <- probeOne(client, id, url)
			}(a.ID, a.URL)
		}
		wg.Wait()
		close(resCh)
		for r := range resCh {
			next.Probes = append(next.Probes, r)
		}
		sort.Slice(next.Probes, func(i, j int) bool {
			return next.Probes[i].ID < next.Probes[j].ID
		})

		statusMu.Lock()
		statusSnap = next
		statusMu.Unlock()

		pruneOldProbes()
	}

	refresh()
	go func() {
		t := time.NewTicker(probeInterval)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()
}

func probeOne(client *http.Client, id, url string) probeResult {
	start := time.Now()
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		r := probeResult{ID: id, URL: url, OK: debounceProbe(id, false), CheckedAt: time.Now().UTC(), Err: err.Error()}
		recordProbeSample(id, r.OK, r.LatencyMS)
		r.Hidden = isHidden(id)
		return r
	}
	req.Header.Set("User-Agent", "kdhome-status/1 (+https://kunaldawn.com)")
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	r := probeResult{ID: id, URL: url, LatencyMS: latency, CheckedAt: time.Now().UTC()}
	if err != nil {
		r.Err = err.Error()
		r.OK = debounceProbe(id, false)
		recordProbeSample(id, r.OK, latency)
		r.Hidden = isHidden(id)
		return r
	}
	resp.Body.Close()
	r.Status = resp.StatusCode
	// Subdomains behind a reverse proxy may answer 200, 301/302, or 401/403
	// on HEAD without the service being down. Treat any < 500 as reachable,
	// then debounce so a single blip doesn't flip the reported status.
	r.OK = debounceProbe(id, resp.StatusCode < 500)
	recordProbeSample(id, r.OK, latency)
	r.Hidden = isHidden(id)
	return r
}

// recordProbeSample upserts today's row in probe_daily for this archive.
// Synchronous (called from probe goroutines that are already concurrent);
// SQLite serializes via single-writer connection in initVisitDB.
func recordProbeSample(id string, ok bool, latencyMS int64) {
	if visitDB == nil {
		return
	}
	date := time.Now().UTC().Format("2006-01-02")
	okN := 0
	if ok {
		okN = 1
	}
	_, err := visitDB.Exec(`
		INSERT INTO probe_daily (date, archive_id, ok_count, total_count, lat_sum_ms, lat_max_ms)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(date, archive_id) DO UPDATE SET
			ok_count    = ok_count + excluded.ok_count,
			total_count = total_count + 1,
			lat_sum_ms  = lat_sum_ms + excluded.lat_sum_ms,
			lat_max_ms  = MAX(lat_max_ms, excluded.lat_max_ms)
	`, date, id, okN, latencyMS, latencyMS)
	if err != nil {
		log.Printf("[PROBE] sample insert failed for %s: %v", id, err)
	}
}

// pruneOldProbes drops any probe_daily rows older than 90 days. Called once
// per refresh cycle — single fast DELETE on an indexed primary key.
func pruneOldProbes() {
	if visitDB == nil {
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -90).Format("2006-01-02")
	if _, err := visitDB.Exec(`DELETE FROM probe_daily WHERE date < ?`, cutoff); err != nil {
		log.Printf("[PROBE] prune failed: %v", err)
	}
}

// loadProbeHistory returns 90 daily aggregates per archive, oldest first,
// keyed by archive_id. Missing days are returned as nil entries so the
// client can render gaps (no data yet).
func loadProbeHistory(days int) map[string][]*dailyAgg {
	out := map[string][]*dailyAgg{}
	for _, a := range archives {
		out[a.ID] = make([]*dailyAgg, days)
	}
	if visitDB == nil {
		return out
	}
	now := time.Now().UTC()
	dateIndex := map[string]int{}
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		dateIndex[d] = i
	}

	cutoff := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := visitDB.Query(`
		SELECT date, archive_id, ok_count, total_count, lat_sum_ms, lat_max_ms
		FROM probe_daily
		WHERE date >= ?
	`, cutoff)
	if err != nil {
		log.Printf("[PROBE] history query failed: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var date, archiveID string
		var okN, totalN, latSum, latMax int64
		if err := rows.Scan(&date, &archiveID, &okN, &totalN, &latSum, &latMax); err != nil {
			continue
		}
		idx, ok := dateIndex[date]
		if !ok {
			continue
		}
		bucket, ok := out[archiveID]
		if !ok {
			continue
		}
		agg := &dailyAgg{Date: date, OK: okN, Total: totalN, LatMax: latMax}
		if totalN > 0 {
			agg.UpPct = float64(okN) / float64(totalN) * 100
			agg.LatAvg = float64(latSum) / float64(totalN)
		}
		bucket[idx] = agg
	}
	return out
}

type dailyAgg struct {
	Date   string
	OK     int64
	Total  int64
	UpPct  float64
	LatAvg float64
	LatMax int64
}

func statusHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if r.Method == http.MethodHead {
		return
	}
	hist := loadProbeHistory(90)
	// Stable order — match archives slice from main.go.
	fmt.Fprint(w, `{"days":90,"archives":{`)
	for i, a := range archives {
		if i > 0 {
			fmt.Fprint(w, ",")
		}
		fmt.Fprintf(w, `%q:[`, a.ID)
		entries := hist[a.ID]
		for j, e := range entries {
			if j > 0 {
				fmt.Fprint(w, ",")
			}
			if e == nil {
				fmt.Fprint(w, "null")
				continue
			}
			fmt.Fprintf(w,
				`{"date":%q,"up_pct":%.1f,"ok":%d,"total":%d,"lat_avg_ms":%.0f,"lat_max_ms":%d}`,
				e.Date, e.UpPct, e.OK, e.Total, e.LatAvg, e.LatMax)
		}
		fmt.Fprint(w, "]")
	}
	fmt.Fprint(w, "}}")
}

func statusJSONHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	statusMu.RLock()
	snap := statusSnap
	statusMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=10")
	if r.Method == http.MethodHead {
		return
	}
	fmt.Fprint(w, "{")
	fmt.Fprintf(w, `"generated_at":%q,`, snap.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(w, `"uptime_sec":%d,`, snap.UptimeSec)
	fmt.Fprint(w, `"archives":[`)
	for i, p := range snap.Probes {
		if i > 0 {
			fmt.Fprint(w, ",")
		}
		fmt.Fprintf(w, `{"id":%q,"url":%q,"ok":%t,"hidden":%t,"status":%d,"latency_ms":%d,"checked_at":%q`,
			p.ID, p.URL, p.OK, p.Hidden, p.Status, p.LatencyMS, p.CheckedAt.Format(time.RFC3339))
		if p.Err != "" {
			fmt.Fprintf(w, `,"err":%q`, p.Err)
		}
		fmt.Fprint(w, "}")
	}
	fmt.Fprint(w, "]}")
}
