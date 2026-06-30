package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestVisitHandlerJSON(t *testing.T) {
	atomic.StoreInt64(&visitCount, 42)
	req := httptest.NewRequest(http.MethodGet, "/api/visit", nil)
	rec := httptest.NewRecorder()
	visitHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := rec.Body.String(), `{"visits":42}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestInitVisitDBConsolidates(t *testing.T) {
	tmp := t.TempDir()

	// Seed a legacy split DB: user_version=1 (v1 already ran), n=0, with
	// accumulated human/bot counts that v2 must fold into the total.
	seed, err := sql.Open("sqlite3", filepath.Join(tmp, "visits.db"))
	if err != nil {
		t.Fatalf("open seed: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE visit_count (id INTEGER PRIMARY KEY CHECK (id = 1), n INTEGER NOT NULL DEFAULT 0, human INTEGER NOT NULL DEFAULT 0, bot INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO visit_count (id, n, human, bot) VALUES (1, 0, 5, 3)`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	seed.Exec(`PRAGMA user_version = 1`)
	seed.Close()

	// v2 should fold human+bot into n (=8) and bump user_version to 2.
	initVisitDB(tmp)
	var n, uv int64
	if err := visitDB.QueryRow(`SELECT n FROM visit_count WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("read after init: %v", err)
	}
	visitDB.QueryRow(`PRAGMA user_version`).Scan(&uv)
	if n != 8 {
		t.Fatalf("after consolidation n = %d, want 8 (5+3)", n)
	}
	if uv != 2 {
		t.Fatalf("user_version = %d, want 2", uv)
	}
	if v := atomic.LoadInt64(&visitCount); v != 8 {
		t.Fatalf("visitCount atomic = %d, want 8", v)
	}

	// Re-init must NOT consolidate again (idempotent via user_version) and must
	// hydrate the atomic from disk.
	visitDB.Exec(`UPDATE visit_count SET n = 20 WHERE id = 1`)
	visitDB.Close()
	initVisitDB(tmp)
	visitDB.QueryRow(`SELECT n FROM visit_count WHERE id = 1`).Scan(&n)
	if n != 20 {
		t.Fatalf("consolidation re-ran: n = %d, want 20", n)
	}
	if v := atomic.LoadInt64(&visitCount); v != 20 {
		t.Fatalf("visitCount after re-init = %d, want 20", v)
	}
	visitDB.Close()
}

func TestRecordVisitIncrementsTotal(t *testing.T) {
	tmp := t.TempDir()
	initVisitDB(tmp) // fresh DB → n=0
	t.Cleanup(func() { visitDB.Close() })

	recordVisit()
	recordVisit()
	recordVisit()

	// recordVisit writes on a goroutine; poll the atomic briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&visitCount) == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v := atomic.LoadInt64(&visitCount); v != 3 {
		t.Fatalf("visitCount = %d, want 3", v)
	}
	var n int64
	visitDB.QueryRow(`SELECT n FROM visit_count WHERE id = 1`).Scan(&n)
	if n != 3 {
		t.Fatalf("db n = %d, want 3", n)
	}
}
