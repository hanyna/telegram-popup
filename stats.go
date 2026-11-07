package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Lightweight, self-contained visit stats — no third-party trackers, no
// cookies beyond one anonymous device id. Counts page opens, logins and
// distinct devices, keeps a per-day tally, and reports how many viewers are
// connected right now (open live streams). Persisted to stats.json.
// (On the free cloud plan the disk resets on a redeploy, like the archive.)
type statsStore struct {
	mu        sync.Mutex
	path      string
	Views     int              `json:"views"`
	Logins    int              `json:"logins"`
	Devices   map[string]int64 `json:"devices"` // deviceID -> last-seen unix
	Days      map[string]int   `json:"days"`    // YYYY-MM-DD -> views
	Since     int64            `json:"since"`
	dirty     bool
	lastFlush time.Time
}

func newStatsStore(path string) *statsStore {
	s := &statsStore{path: path, Devices: map[string]int64{}, Days: map[string]int{}, Since: time.Now().Unix()}
	if path != "" {
		if data, err := readFileMaybe(path); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, s)
			if s.Devices == nil {
				s.Devices = map[string]int64{}
			}
			if s.Days == nil {
				s.Days = map[string]int{}
			}
		}
	}
	return s
}

func newDeviceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func today() string { return time.Now().Format("2006-01-02") }

func (s *statsStore) recordView(deviceID string) {
	s.mu.Lock()
	s.Views++
	s.Days[today()]++
	if deviceID != "" {
		s.Devices[deviceID] = time.Now().Unix()
	}
	// Keep the per-day map from growing without bound (~3 months).
	if len(s.Days) > 95 {
		cutoff := time.Now().AddDate(0, 0, -95).Format("2006-01-02")
		for d := range s.Days {
			if d < cutoff {
				delete(s.Days, d)
			}
		}
	}
	s.dirty = true
	s.maybeFlushLocked()
	s.mu.Unlock()
}

func (s *statsStore) recordLogin() {
	s.mu.Lock()
	s.Logins++
	s.dirty = true
	s.maybeFlushLocked()
	s.mu.Unlock()
}

// maybeFlushLocked writes at most every few seconds — page views can burst.
func (s *statsStore) maybeFlushLocked() {
	if s.path == "" || !s.dirty {
		return
	}
	if time.Since(s.lastFlush) < 5*time.Second {
		return
	}
	s.flushLocked()
}

func (s *statsStore) flushLocked() {
	data, _ := json.Marshal(s)
	s.dirty = false
	s.lastFlush = time.Now()
	path := s.path
	// Write outside the lock would need a copy; data is already serialized.
	go func() { _ = writeFileAtomic(path, data, 0o644) }()
}

// snapshot returns the numbers for the stats panel. activeNow is passed in
// (the feed knows its open streams).
func (s *statsStore) snapshot(activeNow int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	active24 := 0
	for _, seen := range s.Devices {
		if now-seen < 24*3600 {
			active24++
		}
	}
	// Last 7 days as an ordered list for a tiny chart.
	var days []map[string]any
	for i := 6; i >= 0; i-- {
		d := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		days = append(days, map[string]any{"d": d, "v": s.Days[d]})
	}
	return map[string]any{
		"views":      s.Views,
		"logins":     s.Logins,
		"devices":    len(s.Devices),
		"devices24h": active24,
		"today":      s.Days[today()],
		"activeNow":  activeNow,
		"since":      s.Since,
		"days":       days,
	}
}

func (f *Feed) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if f.stats == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"disabled": true})
		return
	}
	f.mu.Lock()
	active := len(f.clients)
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(f.stats.snapshot(active))
}

// ensureDeviceID reads or sets an anonymous device cookie, used only to count
// distinct devices. It is not tied to identity and carries no personal data.
func ensureDeviceID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie("tgpopup_dev"); err == nil && len(c.Value) >= 8 {
		return c.Value
	}
	id := newDeviceID()
	http.SetCookie(w, &http.Cookie{
		Name: "tgpopup_dev", Value: id, Path: "/",
		MaxAge: 365 * 24 * 3600, HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	return id
}
