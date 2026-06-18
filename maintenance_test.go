package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadMaintenanceConfig(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		os.Unsetenv("MAINTENANCE_MODE")
		os.Unsetenv("MAINTENANCE_END")
		os.Unsetenv("MAINTENANCE_MESSAGE")
		cfg := loadMaintenanceConfig()
		if cfg.Enabled {
			t.Fatal("expected disabled when MAINTENANCE_MODE unset")
		}
		if cfg.Message != "We'll be back soon." {
			t.Fatalf("default message = %q", cfg.Message)
		}
	})

	t.Run("truthy values enable", func(t *testing.T) {
		for _, v := range []string{"1", "true", "TRUE", "on", "YES"} {
			os.Setenv("MAINTENANCE_MODE", v)
			if !loadMaintenanceConfig().Enabled {
				t.Fatalf("MAINTENANCE_MODE=%q should enable", v)
			}
		}
		for _, v := range []string{"", "0", "off", "no", "nope"} {
			os.Setenv("MAINTENANCE_MODE", v)
			if loadMaintenanceConfig().Enabled {
				t.Fatalf("MAINTENANCE_MODE=%q should NOT enable", v)
			}
		}
		os.Unsetenv("MAINTENANCE_MODE")
	})

	t.Run("parses end time and message", func(t *testing.T) {
		os.Setenv("MAINTENANCE_MODE", "on")
		os.Setenv("MAINTENANCE_END", "2026-06-20T15:00:00Z")
		os.Setenv("MAINTENANCE_MESSAGE", "Upgrading the rig.")
		defer func() {
			os.Unsetenv("MAINTENANCE_MODE")
			os.Unsetenv("MAINTENANCE_END")
			os.Unsetenv("MAINTENANCE_MESSAGE")
		}()
		cfg := loadMaintenanceConfig()
		if !cfg.HasEnd || cfg.End.IsZero() {
			t.Fatal("expected parsed end time")
		}
		if cfg.Message != "Upgrading the rig." {
			t.Fatalf("message = %q", cfg.Message)
		}
	})

	t.Run("invalid end time → HasEnd false", func(t *testing.T) {
		os.Setenv("MAINTENANCE_MODE", "on")
		os.Setenv("MAINTENANCE_END", "not-a-date")
		defer func() {
			os.Unsetenv("MAINTENANCE_MODE")
			os.Unsetenv("MAINTENANCE_END")
		}()
		cfg := loadMaintenanceConfig()
		if cfg.HasEnd {
			t.Fatal("invalid end time should leave HasEnd false")
		}
	})
}

func TestBuildMaintenancePage(t *testing.T) {
	cfg := maintenanceConfig{
		Enabled: true,
		End:     time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC),
		HasEnd:  true,
		Message: "Upgrading <the> rig.",
	}
	page := string(buildMaintenancePage(cfg))
	if !strings.Contains(page, "2026-06-20T15:00:00Z") {
		t.Fatal("page should embed the ISO end time")
	}
	// message must be HTML-escaped
	if !strings.Contains(page, "Upgrading &lt;the&gt; rig.") {
		t.Fatal("message should be HTML-escaped in the page")
	}
	if strings.Contains(page, "Upgrading <the> rig.") {
		t.Fatal("raw unescaped message must not appear")
	}
}

func TestMaintenanceMiddleware(t *testing.T) {
	cfg := maintenanceConfig{Enabled: true, Message: "soon"}
	page := buildMaintenancePage(cfg)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("REAL SITE"))
	})
	h := maintenanceMiddleware(page, cfg)(inner)

	for _, path := range []string{"/", "/api/status.json", "/static/x.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: code = %d, want 503", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "REAL SITE") {
			t.Fatalf("%s: inner handler should not run", path)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s: missing Retry-After", path)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control = %q", path, rec.Header().Get("Cache-Control"))
		}
	}
}
