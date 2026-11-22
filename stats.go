package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Lightweight, self-contained visit stats — no third-party trackers, no
// cookies beyond one anonymous device id. Counts page opens, logins and
// distinct devices, keeps a per-day tally, and reports how many viewers are
// connected right now (open live streams). Persisted to stats.json.
// (On the free cloud plan the disk resets on a redeploy, like the archive.)
type statsStore struct {
	mu      sync.Mutex
	path    string
	Views   int              `json:"views"`
	Logins  int              `json:"logins"`
	Devices map[string]int64 `json:"devices"` // deviceID -> last-seen unix
	Days    map[string]int   `json:"days"`    // YYYY-MM-DD -> views
	// Visitors: who came in — one row per device, for the owner's panel
	// only: address (IP), kind of device, first / last visit, visit count.
	// Kept for 30 days, at most maxVisitors rows. Disclosed in /terms.
	Visitors  map[string]*visitor `json:"visitors"`
	Since     int64               `json:"since"`
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

type visitor struct {
	IP    string `json:"ip"`
	UA    string `json:"ua"` // short description, e.g. "מחשב · Windows · Chrome"
	First int64  `json:"first"`
	Last  int64  `json:"last"`
	Views int    `json:"views"`
}

const (
	maxVisitors   = 300
	visitorMaxAge = 30 * 24 * time.Hour
)

// describeUA turns a User-Agent into a short human line. No fingerprinting —
// just "what kind of device is this".
func describeUA(ua string) string {
	if ua == "" {
		return "לא ידוע"
	}
	l := strings.ToLower(ua)
	has := func(s string) bool { return strings.Contains(l, s) }
	kind, os := "מחשב", ""
	switch {
	case has("ipad"), has("tablet"), has("android") && !has("mobile"):
		kind = "טאבלט"
	case has("iphone"), has("mobile"):
		kind = "טלפון"
	case has("bot"), has("spider"), has("crawl"), has("curl/"), has("python"), has("go-http-client"), has("headless"):
		kind = "רובוט / בדיקה אוטומטית"
	}
	switch {
	case has("windows"):
		os = "Windows"
	case has("android"):
		os = "Android"
	case has("iphone"), has("ipad"), has("ios"):
		os = "iOS"
	case has("mac os"), has("macintosh"):
		os = "Mac"
	case has("cros"):
		os = "ChromeOS"
	case has("linux"):
		os = "Linux"
	}
	br := ""
	switch {
	case has("edg/"), has("edga/"), has("edgios/"):
		br = "Edge"
	case has("opr/"), has("opera"):
		br = "Opera"
	case has("samsungbrowser"):
		br = "Samsung"
	case has("firefox"), has("fxios"):
		br = "Firefox"
	case has("chrome/"), has("crios/"):
		br = "Chrome"
	case has("safari"):
		br = "Safari"
	}
	out := kind
	if os != "" {
		out += " · " + os
	}
	if br != "" {
		out += " · " + br
	}
	return out
}

func newDeviceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func today() string { return time.Now().Format("2006-01-02") }

func (s *statsStore) recordView(deviceID, ip, userAgent string) {
	s.mu.Lock()
	s.Views++
	s.Days[today()]++
	if deviceID != "" {
		now := time.Now().Unix()
		s.Devices[deviceID] = now
		if s.Visitors == nil {
			s.Visitors = map[string]*visitor{}
		}
		v := s.Visitors[deviceID]
		if v == nil {
			v = &visitor{First: now}
			s.Visitors[deviceID] = v
		}
		if len(ip) > 64 {
			ip = ip[:64]
		}
		v.IP, v.UA, v.Last = ip, describeUA(userAgent), now
		v.Views++
		s.pruneVisitorsLocked(now)
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

// pruneVisitorsLocked: 30 days, and never more than maxVisitors rows.
func (s *statsStore) pruneVisitorsLocked(now int64) {
	cutoff := now - int64(visitorMaxAge/time.Second)
	for id, v := range s.Visitors {
		if v.Last < cutoff {
			delete(s.Visitors, id)
		}
	}
	for len(s.Visitors) > maxVisitors {
		oldest, at := "", now+1
		for id, v := range s.Visitors {
			if v.Last < at {
				oldest, at = id, v.Last
			}
		}
		delete(s.Visitors, oldest)
	}
}

// visitorList: newest first, for the owner's panel. me marks the row of the
// device that is asking.
func (s *statsStore) visitorList(me string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.Visitors))
	for id := range s.Visitors {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return s.Visitors[ids[i]].Last > s.Visitors[ids[j]].Last })
	if len(ids) > 150 {
		ids = ids[:150]
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		v := s.Visitors[id]
		out = append(out, map[string]any{
			"ip": v.IP, "ua": v.UA, "first": v.First, "last": v.Last, "views": v.Views, "me": id == me,
		})
	}
	return out
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

// flushSync writes the stats file right now (before a final backup).
func (s *statsStore) flushSync() {
	s.mu.Lock()
	data, _ := json.Marshal(s)
	s.dirty = false
	s.lastFlush = time.Now()
	path := s.path
	s.mu.Unlock()
	if path != "" {
		_ = writeFileAtomic(path, data, 0o644)
	}
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
	if !f.isAdmin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "admin required"})
		return
	}
	if f.stats == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"disabled": true})
		return
	}
	f.mu.Lock()
	active := len(f.clients)
	f.mu.Unlock()
	snap := f.stats.snapshot(active)
	me := ""
	if c, err := r.Cookie("tgpopup_dev"); err == nil {
		me = c.Value
	}
	snap["visitors"] = f.stats.visitorList(me)
	// In claim-mode (no env key), hand the admin a link that unlocks the
	// panel on another of their devices (phone, etc.).
	if f.AdminKey == "" && f.admin != nil {
		if tok := f.admin.token(); tok != "" {
			snap["unlockPath"] = "/?admin=" + tok
		}
	}
	_ = json.NewEncoder(w).Encode(snap)
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
