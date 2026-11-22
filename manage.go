package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// The owner's management tools (admin only):
//
//   - channels: add / remove from the page (the list is still locked for
//     everybody else);
//   - hide a message for all viewers;
//   - a site notice ("banner") shown to everybody;
//   - block a visitor's address (IP);
//   - reset likes, for one message or all of them;
//   - status of the long-video bridge and of the permanent backup.
//
// Everything the owner sets lives in adminstate.json. On the free cloud plan
// the disk does not survive a restart, so persist.go backs the file up
// (together with likes and visit stats) and restores it on start.

type hiddenEntry struct {
	At      int64  `json:"at"`
	Channel string `json:"channel"`
	Preview string `json:"preview"`
}

type manageState struct {
	// Channels: the owner's channel list. nil = never edited, the defaults
	// (config / TGPOPUP_CHANNELS) apply.
	Channels []string               `json:"channels,omitempty"`
	Hidden   map[string]hiddenEntry `json:"hidden"`
	Banner   string                 `json:"banner"`
	BannerAt int64                  `json:"banner_at"`
	Blocked  map[string]int64       `json:"blocked"` // ip -> since
}

const (
	maxHidden  = 500
	maxBlocked = 500
	maxBanner  = 300 // runes
)

type manageStore struct {
	mu   sync.Mutex
	path string
	s    manageState
}

func newManageStore(path string) *manageStore {
	m := &manageStore{path: path}
	if path != "" {
		if data, err := readFileMaybe(path); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &m.s)
		}
	}
	if m.s.Hidden == nil {
		m.s.Hidden = map[string]hiddenEntry{}
	}
	if m.s.Blocked == nil {
		m.s.Blocked = map[string]int64{}
	}
	return m
}

func (m *manageStore) saveLocked() {
	if m.path == "" {
		return
	}
	if data, err := json.MarshalIndent(&m.s, "", " "); err == nil {
		_ = writeFileAtomic(m.path, data, 0o600)
	}
}

func (m *manageStore) isHidden(key string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	_, ok := m.s.Hidden[key]
	m.mu.Unlock()
	return ok
}

func (m *manageStore) hiddenKeys() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.s.Hidden))
	for k := range m.s.Hidden {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *manageStore) hide(key string, e hiddenEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s.Hidden[key] = e
	for len(m.s.Hidden) > maxHidden { // forget the oldest
		oldest, at := "", int64(1<<62)
		for k, v := range m.s.Hidden {
			if v.At < at {
				oldest, at = k, v.At
			}
		}
		delete(m.s.Hidden, oldest)
	}
	m.saveLocked()
}

func (m *manageStore) unhide(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.s.Hidden[key]; !ok {
		return false
	}
	delete(m.s.Hidden, key)
	m.saveLocked()
	return true
}

func (m *manageStore) banner() (string, int64) {
	if m == nil {
		return "", 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.s.Banner, m.s.BannerAt
}

func (m *manageStore) setBanner(text string) (string, int64) {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxBanner {
		text = string(r[:maxBanner])
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s.Banner = text
	m.s.BannerAt = time.Now().Unix()
	m.saveLocked()
	return m.s.Banner, m.s.BannerAt
}

func (m *manageStore) isBlocked(ip string) bool {
	if m == nil || ip == "" {
		return false
	}
	m.mu.Lock()
	_, ok := m.s.Blocked[ip]
	m.mu.Unlock()
	return ok
}

func (m *manageStore) block(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.s.Blocked[ip]; !ok && len(m.s.Blocked) >= maxBlocked {
		return false
	}
	m.s.Blocked[ip] = time.Now().Unix()
	m.saveLocked()
	return true
}

func (m *manageStore) unblock(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.s.Blocked, ip)
	m.saveLocked()
}

// channels returns the owner's list, ok=false when it was never edited.
func (m *manageStore) channels() ([]string, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.s.Channels) == 0 {
		return nil, false
	}
	return append([]string{}, m.s.Channels...), true
}

func (m *manageStore) setChannels(names []string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s.Channels = append([]string{}, names...)
	m.saveLocked()
}

// ----- feed side -------------------------------------------------------------

// hideMessage removes a post from the live timeline for everybody and
// remembers it, so it does not come back from the archive or a later scan.
func (f *Feed) hideMessage(key string) bool {
	if f.manage == nil || key == "" || !reReactKey.MatchString(key) || len(key) > maxReactKeyLength {
		return false
	}
	entry := hiddenEntry{At: time.Now().Unix()}
	f.mu.Lock()
	kept := f.items[:0]
	for _, it := range f.items {
		if it.Key == key {
			entry.Channel = it.Channel
			entry.Preview = it.Preview
			if r := []rune(entry.Preview); len(r) > 90 {
				entry.Preview = string(r[:90]) + "…"
			}
			delete(f.keys, key)
			continue
		}
		kept = append(kept, it)
	}
	f.items = kept
	f.snap = nil
	f.mu.Unlock()
	if entry.Channel == "" {
		if i := strings.LastIndex(key, "_"); i > 0 {
			entry.Channel = key[:i]
		}
	}
	f.manage.hide(key, entry)
	f.broadcast(map[string]any{"type": "hide", "key": key})
	return true
}

// dropHidden filters hidden posts out of an answer built outside the live
// timeline (older history, search).
func (f *Feed) dropHidden(items []FeedItem) []FeedItem {
	if f.manage == nil {
		return items
	}
	out := items[:0]
	for _, it := range items {
		if !f.manage.isHidden(it.Key) {
			out = append(out, it)
		}
	}
	return out
}

// blockGuard turns blocked addresses away. The owner is never locked out:
// a valid admin cookie passes, and so does the "?admin=" unlock link.
func (f *Feed) blockGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.manage != nil && f.BindAddr != "" && f.manage.isBlocked(clientIP(r)) &&
			!f.isAdmin(r) && r.URL.Query().Get("admin") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<!doctype html><html lang="he" dir="rtl"><meta charset="utf-8">` +
				`<meta name="viewport" content="width=device-width, initial-scale=1"><title>הגישה נחסמה</title>` +
				`<body style="font-family:system-ui,Arial,sans-serif;background:#0d1016;color:#e6e9ef;text-align:center;padding:60px 20px">` +
				`<h1 style="font-size:22px">הגישה לאתר נחסמה</h1><p>מפעיל האתר חסם את הגישה מהכתובת הזו.</p></body></html>`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// canEditChannels: desktop — as configured (allow_channel_edit). Online — the
// owner only.
func (f *Feed) canEditChannels(r *http.Request) bool {
	if f.AddChannel == nil && f.RemoveChannel == nil {
		return false
	}
	if f.AdminChannels {
		return f.isAdmin(r)
	}
	return true
}

// handleManage: GET = everything the management panel shows; POST = one
// action. Admin only.
func (f *Feed) handleManage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !f.isAdmin(r) || f.manage == nil {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "admin required"})
		return
	}
	fail := func(msg string) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var body struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			IP     string `json:"ip"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		switch body.Action {
		case "hide":
			if !f.hideMessage(body.Key) {
				fail("לא ניתן להסתיר את ההודעה הזו")
				return
			}
		case "unhide":
			if f.manage.unhide(body.Key) {
				f.broadcast(map[string]any{"type": "unhide", "key": body.Key})
				if f.Refresh != nil {
					f.Refresh() // a recent post comes straight back with the next scan
				}
			}
		case "banner":
			text, at := f.manage.setBanner(body.Text)
			f.broadcast(map[string]any{"type": "banner", "text": text, "at": at})
		case "block":
			ip := strings.TrimSpace(body.IP)
			parsed := net.ParseIP(ip)
			if parsed == nil {
				fail("כתובת IP לא תקינה")
				return
			}
			ip = parsed.String()
			if ip == clientIP(r) {
				fail("זו הכתובת שלך — לא חוסמים את עצמך")
				return
			}
			if !f.manage.block(ip) {
				fail("רשימת החסימות מלאה")
				return
			}
		case "unblock":
			f.manage.unblock(strings.TrimSpace(body.IP))
		case "likes_clear":
			if f.reacts != nil {
				if body.Key == "" {
					f.reacts.clearAll()
				} else {
					f.reacts.clearKey(body.Key)
				}
				f.broadcast(map[string]any{"type": "reacts"})
			}
		default:
			http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
			return
		}
		if f.persist != nil {
			f.persist.kick()
		}
	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(f.manageSnapshot(r))
}

func (f *Feed) manageSnapshot(r *http.Request) map[string]any {
	m := f.manage
	m.mu.Lock()
	type hid struct {
		Key     string `json:"key"`
		Channel string `json:"channel"`
		Preview string `json:"preview"`
		At      int64  `json:"at"`
	}
	hidden := make([]hid, 0, len(m.s.Hidden))
	for k, e := range m.s.Hidden {
		hidden = append(hidden, hid{k, e.Channel, e.Preview, e.At})
	}
	type blk struct {
		IP string `json:"ip"`
		At int64  `json:"at"`
	}
	blocked := make([]blk, 0, len(m.s.Blocked))
	for ip, at := range m.s.Blocked {
		blocked = append(blocked, blk{ip, at})
	}
	banner, bannerAt := m.s.Banner, m.s.BannerAt
	m.mu.Unlock()
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].At > hidden[j].At })
	sort.Slice(blocked, func(i, j int) bool { return blocked[i].At > blocked[j].At })

	type chn struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	var chans []chn
	if f.ListChannels != nil {
		for _, n := range f.ListChannels() {
			c := chn{Name: n, Title: "@" + n}
			if info, ok := f.GetChannelInfo(n); ok && info.Name != "" {
				c.Title = info.Name
			}
			chans = append(chans, c)
		}
	}
	likes := 0
	if f.reacts != nil {
		likes = f.reacts.total()
	}
	out := map[string]any{
		"channels":     chans,
		"channelsEdit": f.canEditChannels(r),
		"hidden":       hidden,
		"banner":       map[string]any{"text": banner, "at": bannerAt},
		"blocked":      blocked,
		"likes":        likes,
		"myip":         clientIP(r),
		"bridge":       f.bridge.status(),
		"persist":      f.persist.status(),
		"cloud":        f.BindAddr != "",
	}
	return out
}

// ----- small additions to other stores ---------------------------------------

func (rs *reactStore) clearKey(key string) {
	rs.mu.Lock()
	delete(rs.m, key)
	rs.saveLocked()
	rs.mu.Unlock()
}

func (rs *reactStore) clearAll() {
	rs.mu.Lock()
	rs.m = map[string]map[string]string{}
	rs.saveLocked()
	rs.mu.Unlock()
}

func (rs *reactStore) saveLocked() {
	if rs.path == "" {
		return
	}
	data, _ := json.Marshal(rs.m)
	_ = writeFileAtomic(rs.path, data, 0o644)
}

func (rs *reactStore) total() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	n := 0
	for _, devs := range rs.m {
		n += len(devs)
	}
	return n
}

// status: what the owner's panel shows about long videos.
func (b *bridgeStore) status() map[string]any {
	if b == nil {
		return map[string]any{"on": false}
	}
	now := time.Now()
	type blk struct {
		Channel string `json:"channel"`
		Until   int64  `json:"until"`
	}
	b.mu.Lock()
	blocks := []blk{}
	for ch, until := range b.chanBlock {
		if until.After(now) {
			blocks = append(blocks, blk{ch, until.Unix()})
		}
	}
	var global int64
	if b.globalWait.After(now) {
		global = b.globalWait.Unix()
	}
	b.mu.Unlock()
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Channel < blocks[j].Channel })

	count, bytes := 0, int64(0)
	if entries, err := os.ReadDir(b.dir); err == nil {
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".mp4") {
				continue
			}
			if info, err := e.Info(); err == nil {
				count++
				bytes += info.Size()
			}
		}
	}
	return map[string]any{
		"on": true, "base": b.base, "blocked": blocks, "globalUntil": global,
		"cached": count, "cachedBytes": bytes, "maxBytes": b.maxTotal,
	}
}
