package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestStatsCountsAndSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	st := newStatsStore(path)
	st.recordView("dev-a")
	st.recordView("dev-a") // same device, two views
	st.recordView("dev-b")
	st.recordLogin()
	snap := st.snapshot(3)
	if snap["views"] != 3 {
		t.Fatalf("views: %v", snap["views"])
	}
	if snap["devices"] != 2 {
		t.Fatalf("devices: %v", snap["devices"])
	}
	if snap["logins"] != 1 {
		t.Fatalf("logins: %v", snap["logins"])
	}
	if snap["today"] != 3 {
		t.Fatalf("today: %v", snap["today"])
	}
	if snap["activeNow"] != 3 {
		t.Fatalf("activeNow: %v", snap["activeNow"])
	}
	days, ok := snap["days"].([]map[string]any)
	if !ok || len(days) != 7 {
		t.Fatalf("days should have 7 entries, got %v", snap["days"])
	}

	// handler: nil stats is safe
	f := NewFeed()
	rec := httptest.NewRecorder()
	f.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	var d map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d["disabled"] != true {
		t.Fatalf("nil stats should report disabled, got %v", d)
	}
	// handler: with stats returns numbers
	f.stats = st
	rec = httptest.NewRecorder()
	f.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d["views"].(float64) != 3 {
		t.Fatalf("handler views: %v", d["views"])
	}
}
