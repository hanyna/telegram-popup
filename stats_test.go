package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatsCountsAndSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	st := newStatsStore(path)
	st.recordView("dev-a", "1.2.3.4", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0 Safari/537.36")
	st.recordView("dev-a", "1.2.3.4", "") // same device, two views
	st.recordView("dev-b", "5.6.7.8", "")
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

// The owner's panel lists who came in: address, kind of device, visits.
func TestVisitorList(t *testing.T) {
	st := newStatsStore("")
	win := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	phone := "Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36"
	st.recordView("dev-a", "1.2.3.4", win)
	st.recordView("dev-b", "5.6.7.8", phone)
	st.recordView("dev-a", "1.2.3.9", win) // same computer, new address
	list := st.visitorList("dev-a")
	if len(list) != 2 {
		t.Fatalf("want 2 visitors, got %d", len(list))
	}
	var a, b map[string]any
	for _, v := range list {
		if v["me"] == true {
			a = v
		} else {
			b = v
		}
	}
	if a == nil || a["ip"] != "1.2.3.9" || a["views"] != 2 || a["ua"] != "מחשב · Windows · Chrome" {
		t.Fatalf("computer row wrong: %+v", a)
	}
	if b == nil || b["ip"] != "5.6.7.8" || b["ua"] != "טלפון · Android · Chrome" {
		t.Fatalf("phone row wrong: %+v", b)
	}

	// Old rows expire and the list is capped.
	st.Visitors["dev-b"].Last -= int64(31 * 24 * 3600)
	for i := 0; i < maxVisitors+20; i++ {
		st.recordView("bulk-"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26)), "9.9.9.9", "")
	}
	if _, still := st.Visitors["dev-b"]; still {
		t.Fatal("visitor older than 30 days should be dropped")
	}
	if len(st.Visitors) > maxVisitors {
		t.Fatalf("visitor list not capped: %d", len(st.Visitors))
	}
}

// Only the admin gets the list.
func TestVisitorsAdminOnly(t *testing.T) {
	f := NewFeed()
	f.BindAddr = "0.0.0.0"
	f.AdminKey = "owner-secret"
	f.stats = newStatsStore("")
	f.stats.recordView("dev-a", "1.2.3.4", "")
	rec := httptest.NewRecorder()
	f.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	if rec.Code != 403 || strings.Contains(rec.Body.String(), "1.2.3.4") {
		t.Fatalf("visitor addresses leaked to a non-admin: %d %s", rec.Code, rec.Body.String())
	}
}
