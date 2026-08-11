package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Live feed: a chat-style HTML page served on localhost. Multiple channels
// merge into one timeline (filterable per channel); new messages arrive at
// the BOTTOM over Server-Sent Events, like any messenger.
// ---------------------------------------------------------------------------

type FeedItem struct {
	ID      int    `json:"id"`
	Channel string `json:"channel"`
	Key     string `json:"key"`     // channel_id — unique across channels
	TS      int64  `json:"ts"`      // unix seconds, for cross-channel ordering
	Preview string `json:"preview"` // one-line text for the sidebar
	HTML    string `json:"html"`
}

type Feed struct {
	mu      sync.Mutex
	items   []FeedItem // sorted newest first
	keys    map[string]bool
	clients map[chan string]struct{}

	infoMu sync.Mutex
	infos  map[string]ChannelInfo // channel username -> display identity

	// Wired in by main so the feed stays free of scanning/config logic.
	FetchOlder    func(channel string, beforeID int) ([]Message, error)
	ListChannels  func() []string
	AddChannel    func(name string) (string, error)
	RemoveChannel func(name string) error
	GetSettings   func() (popups, sound bool)
	SetSettings   func(popups, sound *bool) (bool, bool)
	IsMuted       func(name string) bool
	SetMuted      func(name string, muted bool) error
	Refresh       func() // trigger an immediate scan of all channels
	ResolveVideo  func(channel string, id int) (string, error)
	// BigVideo downloads a large video via an authenticated Telegram account
	// and returns a local file path. nil when the account feature is off.
	BigVideo    func(channel string, id int) (string, error)
	HasTGAccount func() bool
	Search        func(query, channel string, limit int) []Message
	ArchiveCount  func() int
	GetAutostart  func() bool
	SetAutostart  func(on bool) error
	GetTheme      func() string
	SetTheme      func(theme string)

	videoMu    sync.Mutex
	videoCache map[string]string    // "channel/id" -> direct mp4 URL
	pending    map[string]time.Time // failed resolutions awaiting background retry

	// Cloud hosting knobs (see cloud.go). Zero values = desktop behaviour:
	// bind loopback only, no access key.
	BindAddr  string
	AccessKey string
}

// SetChannelInfo records a channel's display name and photo.
func (f *Feed) SetChannelInfo(name string, info ChannelInfo) {
	if info.Name == "" && info.Photo == "" {
		return
	}
	f.infoMu.Lock()
	f.infos[name] = info
	f.infoMu.Unlock()
}

// GetChannelInfo returns the recorded identity, if any.
func (f *Feed) GetChannelInfo(name string) (ChannelInfo, bool) {
	f.infoMu.Lock()
	defer f.infoMu.Unlock()
	info, ok := f.infos[name]
	return info, ok
}

const feedMaxItems = 800

func NewFeed() *Feed {
	return &Feed{
		keys:       make(map[string]bool),
		clients:    make(map[chan string]struct{}),
		infos:      make(map[string]ChannelInfo),
		videoCache: make(map[string]string),
		pending:    make(map[string]time.Time),
	}
}

func itemKey(m Message) string { return m.Channel + "_" + strconv.Itoa(m.ID) }

func toItem(m Message) FeedItem {
	ts := int64(0)
	if t, err := time.Parse(time.RFC3339, m.Time); err == nil {
		ts = t.Unix()
	}
	prev := strings.TrimSpace(strings.ReplaceAll(m.Text, "\n", " "))
	if prev == "" {
		switch {
		case m.Video != "" || m.VideoThumb != "":
			prev = "🎬 סרטון"
		case m.Photo != "":
			prev = "📷 תמונה"
		}
	} else if m.Video != "" || m.VideoThumb != "" {
		prev = "🎬 " + prev
	} else if m.Photo != "" {
		prev = "📷 " + prev
	}
	if r := []rune(prev); len(r) > 80 {
		prev = string(r[:80]) + "…"
	}
	return FeedItem{ID: m.ID, Channel: m.Channel, Key: itemKey(m), TS: ts, Preview: prev, HTML: renderFeedItem(m)}
}

func (f *Feed) sortLocked() {
	sort.Slice(f.items, func(i, j int) bool {
		if f.items[i].TS != f.items[j].TS {
			return f.items[i].TS > f.items[j].TS
		}
		return f.items[i].ID > f.items[j].ID
	})
}

// Add stores new posts and pushes them to every open tab.
func (f *Feed) Add(msgs []Message) {
	if len(msgs) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, m := range msgs {
		if strings.TrimSpace(m.Text) == "" && !m.HasMedia() {
			continue // service posts with no content just add noise
		}
		k := itemKey(m)
		if f.keys[k] {
			continue
		}
		f.keys[k] = true
		item := toItem(m)
		f.items = append(f.items, item)
		if payload, err := json.Marshal(item); err == nil {
			for ch := range f.clients {
				select {
				case ch <- string(payload):
				default:
				}
			}
		}
	}
	f.sortLocked()
	f.trimLocked()
}

// Seed stores a startup snapshot without broadcasting. Broadcasting seeds
// would make every already-open tab light up old posts as "new" after a
// restart — the tabs resync themselves over /api/messages instead.
func (f *Feed) Seed(msgs []Message) {
	_, _ = f.AddOlder(msgs)
}

// AddOlder stores history without broadcasting (these are not new posts).
func (f *Feed) AddOlder(msgs []Message) (added, minID int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, m := range msgs {
		if minID == 0 || m.ID < minID {
			minID = m.ID
		}
		if strings.TrimSpace(m.Text) == "" && !m.HasMedia() {
			continue
		}
		k := itemKey(m)
		if f.keys[k] {
			continue
		}
		f.keys[k] = true
		f.items = append(f.items, toItem(m))
		added++
	}
	f.sortLocked()
	return added, minID
}

// RemoveChannelItems drops a removed channel's posts from the timeline.
func (f *Feed) RemoveChannelItems(channel string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.items[:0]
	for _, it := range f.items {
		if it.Channel == channel {
			delete(f.keys, it.Key)
			continue
		}
		kept = append(kept, it)
	}
	f.items = kept
}

func (f *Feed) trimLocked() {
	if len(f.items) > feedMaxItems {
		for _, it := range f.items[feedMaxItems:] {
			delete(f.keys, it.Key)
		}
		f.items = f.items[:feedMaxItems]
	}
}

// Start binds the local server. Returns the URL it serves on.
func (f *Feed) Start(port int) (string, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", f.handlePage)
	mux.HandleFunc("/api/messages", f.handleMessages)
	mux.HandleFunc("/api/older", f.handleOlder)
	mux.HandleFunc("/api/stream", f.handleStream)
	mux.HandleFunc("/api/channels", f.handleChannels)
	mux.HandleFunc("/api/settings", f.handleSettings)
	mux.HandleFunc("/api/mute", f.handleMute)
	mux.HandleFunc("/api/refresh", f.handleRefresh)
	mux.HandleFunc("/api/video", f.handleVideo)
	mux.HandleFunc("/api/media", f.handleMedia)
	mux.HandleFunc("/api/search", f.handleSearch)
	mux.HandleFunc("/api/autostart", f.handleAutostart)
	mux.HandleFunc("/api/status", f.handleStatus)

	bind := f.BindAddr
	if bind == "" {
		bind = "127.0.0.1" // desktop: never expose the feed to the network
	}
	ln, err := net.Listen("tcp", bind+":"+strconv.Itoa(port))
	if err != nil {
		return "", err
	}
	// The access key (cloud mode) wraps the ENTIRE mux — page, APIs, stream.
	handler := withAccessKey(mux, f.AccessKey)
	go func() { _ = http.Serve(ln, handler) }()
	return "http://" + ln.Addr().String(), nil
}

func (f *Feed) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Never let the browser serve a stale copy of the UI after an upgrade.
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	page := strings.ReplaceAll(feedPage, "%%VERSION%%", version)
	_, _ = w.Write([]byte(page))
}

func (f *Feed) handleMessages(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	items := make([]FeedItem, len(f.items))
	copy(items, f.items)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func (f *Feed) handleOlder(w http.ResponseWriter, r *http.Request) {
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	before, err := strconv.Atoi(r.URL.Query().Get("before"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil || before < 2 || channel == "" || f.FetchOlder == nil {
		_, _ = w.Write([]byte(`{"items":[]}`))
		return
	}

	msgs, err := f.FetchOlder(channel, before)
	if err != nil {
		http.Error(w, `{"error":"fetch failed"}`, http.StatusBadGateway)
		return
	}
	var older []Message
	for _, m := range msgs {
		if m.ID < before {
			older = append(older, m)
		}
	}
	f.AddOlder(older)

	items := make([]FeedItem, 0, len(older))
	for _, m := range older {
		items = append(items, toItem(m))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

// handleChannels: GET lists, POST adds, DELETE removes. Changes persist to
// config.json through the callbacks main wires in.
func (f *Feed) handleChannels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	switch r.Method {
	case http.MethodGet:
		var names []string
		if f.ListChannels != nil {
			names = f.ListChannels()
		}
		type entry struct {
			Name  string `json:"name"`
			Title string `json:"title"`
			Photo string `json:"photo"`
			Muted bool   `json:"muted"`
		}
		entries := make([]entry, 0, len(names))
		for _, n := range names {
			e := entry{Name: n, Title: "@" + n}
			if info, ok := f.GetChannelInfo(n); ok {
				if info.Name != "" {
					e.Title = info.Name
				}
				e.Photo = info.Photo
			}
			if f.IsMuted != nil {
				e.Muted = f.IsMuted(n)
			}
			entries = append(entries, e)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"channels": entries})

	case http.MethodPost:
		if f.AddChannel == nil {
			http.Error(w, `{"error":"unsupported"}`, http.StatusNotImplemented)
			return
		}
		var body struct {
			Channel string `json:"channel"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		name, err := f.AddChannel(body.Channel)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": name})

	case http.MethodDelete:
		if f.RemoveChannel == nil {
			http.Error(w, `{"error":"unsupported"}`, http.StatusNotImplemented)
			return
		}
		name := normalizeChannel(r.URL.Query().Get("name"))
		if err := f.RemoveChannel(name); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		f.RemoveChannelItems(name)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})

	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

// handleVideo resolves a thumbnail-only post to its direct mp4 URL, so the
// page can play long videos in its own native player. Successful lookups are
// cached — each video is resolved against Telegram at most once.
func (f *Feed) handleVideo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	channel := normalizeChannel(r.URL.Query().Get("channel"))
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || channel == "" || id < 1 || f.ResolveVideo == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "בקשה לא תקינה"})
		return
	}
	key := channel + "/" + strconv.Itoa(id)

	f.videoMu.Lock()
	cached, ok := f.videoCache[key]
	f.videoMu.Unlock()
	if ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": cached})
		return
	}

	src, err := f.ResolveVideo(channel, id)
	if err != nil {
		// Anonymous resolution failed. If a Telegram account is connected,
		// this video CAN be fetched via the API (the large-video path).
		if f.BigVideo != nil && f.HasTGAccount != nil && f.HasTGAccount() {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    true,
				"proxy": "/api/media?channel=" + channel + "&id=" + strconv.Itoa(id),
				"big":   true, // played through the account-download proxy
			})
			return
		}
		f.registerPending(channel, id)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "pending": true})
		return
	}
	f.videoMu.Lock()
	f.videoCache[key] = src
	f.videoMu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    true,
		"url":   src,
		"proxy": "/api/media?channel=" + channel + "&id=" + strconv.Itoa(id),
	})
}

// serveLocalFile streams a downloaded mp4 with Range support so the player can
// seek through a large account-fetched video.
func serveLocalFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// handleAutostart: GET reads whether the app starts with Windows, POST flips.
func (f *Feed) handleAutostart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.Method {
	case http.MethodGet:
		on := false
		if f.GetAutostart != nil {
			on = f.GetAutostart()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"on": on})
	case http.MethodPost:
		if f.SetAutostart == nil {
			http.Error(w, `{"error":"unsupported"}`, http.StatusNotImplemented)
			return
		}
		var body struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 256)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if err := f.SetAutostart(body.On); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "on": body.On})
	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

// handleStatus reports, per channel, whether Telegram is actually answering.
// This exists because "the page is empty" was previously indistinguishable
// from "the app is broken" — now the page can say exactly which it is.
func (f *Feed) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	var names []string
	if f.ListChannels != nil {
		names = f.ListChannels()
	}
	rows := tgLimiter.Statuses(names)

	okCount := 0
	for _, s := range rows {
		if s.OK {
			okCount++
		}
	}

	f.mu.Lock()
	itemCount := len(f.items)
	f.mu.Unlock()

	banner := ""
	switch {
	case len(names) == 0:
		banner = "לא מוגדרים ערוצים. הוסף ערוץ בכפתור למטה."
	case tgLimiter.AnyBlocked() && okCount == 0:
		banner = "טלגרם מגבילה זמנית את הבקשות מהמחשב הזה. התוכנה ממתינה ותנסה שוב לבד — " +
			"בדרך כלל זה חוזר לעצמו תוך כמה דקות. אין צורך לעשות כלום."
	case tgLimiter.AnyBlocked():
		banner = "חלק מהערוצים מוגבלים זמנית על ידי טלגרם. הם יתמלאו לבד בהמשך."
	case okCount == 0 && itemCount == 0:
		banner = "עדיין טוען ערוצים…"
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"channels": rows,
		"ok":       okCount,
		"total":    len(names),
		"items":    itemCount,
		"banner":   banner,
		"blocked":  tgLimiter.AnyBlocked(),
	})
}

// handleSearch scans the whole on-disk archive and returns rendered cards.
func (f *Feed) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if f.Search == nil {
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
		return
	}
	q := r.URL.Query().Get("q")
	channel := normalizeChannel(r.URL.Query().Get("channel"))

	msgs := f.Search(q, channel, 120)
	items := make([]FeedItem, 0, len(msgs))
	for _, m := range msgs {
		items = append(items, toItem(m))
	}
	total := 0
	if f.ArchiveCount != nil {
		total = f.ArchiveCount()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": total})
}

// broadcast pushes any JSON-serializable event to every connected tab.
func (f *Feed) broadcast(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	f.mu.Lock()
	for ch := range f.clients {
		select {
		case ch <- string(payload):
		default:
		}
	}
	f.mu.Unlock()
}

// registerPending queues a failed video resolution for background retries —
// fresh posts often become playable a few minutes later, once Telegram
// finishes processing the web version.
func (f *Feed) registerPending(channel string, id int) {
	key := channel + "/" + strconv.Itoa(id)
	f.videoMu.Lock()
	if _, exists := f.pending[key]; !exists {
		f.pending[key] = time.Now()
	}
	f.videoMu.Unlock()
}

// RetryPending re-attempts every queued video. Success moves it to the cache
// and notifies open tabs; entries older than 12h are dropped.
func (f *Feed) RetryPending() {
	if f.ResolveVideo == nil {
		return
	}
	f.videoMu.Lock()
	keys := make([]string, 0, len(f.pending))
	for k, first := range f.pending {
		if time.Since(first) > 12*time.Hour {
			delete(f.pending, k)
			continue
		}
		keys = append(keys, k)
	}
	f.videoMu.Unlock()

	for _, key := range keys {
		parts := strings.SplitN(key, "/", 2)
		id, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		src, err := f.ResolveVideo(parts[0], id)
		if err != nil {
			continue
		}
		f.videoMu.Lock()
		f.videoCache[key] = src
		delete(f.pending, key)
		f.videoMu.Unlock()
		f.broadcast(map[string]any{"type": "videoready", "channel": parts[0], "id": id})
	}
}

// PendingCount is used by tests and diagnostics.
func (f *Feed) PendingCount() int {
	f.videoMu.Lock()
	defer f.videoMu.Unlock()
	return len(f.pending)
}

// resolveCached returns the direct video URL for a post, from cache or by
// resolving it now.
func (f *Feed) resolveCached(channel string, id int) (string, error) {
	key := channel + "/" + strconv.Itoa(id)
	f.videoMu.Lock()
	cached, ok := f.videoCache[key]
	f.videoMu.Unlock()
	if ok {
		return cached, nil
	}
	if f.ResolveVideo == nil {
		return "", errNoResolver
	}
	src, err := f.ResolveVideo(channel, id)
	if err != nil {
		return "", err
	}
	f.videoMu.Lock()
	f.videoCache[key] = src
	f.videoMu.Unlock()
	return src, nil
}

var errNoResolver = fmt.Errorf("resolver not wired")

// mediaClient has no overall timeout — a long video streams for as long as
// it plays. Connection setup still uses Go's sane transport defaults.
var mediaClient = &http.Client{}

// handleMedia streams the video THROUGH the app: the server fetches the file
// from Telegram's CDN with browser-like headers and pipes the bytes to the
// player, passing Range through so seeking works. This sidesteps anything
// that might stop the browser from playing the CDN URL directly.
func (f *Feed) handleMedia(w http.ResponseWriter, r *http.Request) {
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || channel == "" || id < 1 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	src, err := f.resolveCached(channel, id)
	if err != nil {
		// Anonymous paths exhausted — try the authenticated account download
		// (large videos). Success streams the local file with seeking.
		if f.BigVideo != nil {
			if path, dErr := f.BigVideo(channel, id); dErr == nil && path != "" {
				serveLocalFile(w, r, path)
				return
			}
		}
		f.registerPending(channel, id)
		http.Error(w, "unavailable", http.StatusNotFound)
		return
	}

	fetchUpstream := func(target string) (*http.Response, error) {
		req, rErr := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if rErr != nil {
			return nil, rErr
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		return mediaClient.Do(req)
	}

	resp, err := fetchUpstream(src)
	if err != nil {
		http.Error(w, "upstream failed", http.StatusBadGateway)
		return
	}
	// CDN URLs carry short-lived tokens. If a cached URL has gone stale,
	// drop it, resolve fresh, and retry once — invisible to the player.
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		key := channel + "/" + strconv.Itoa(id)
		f.videoMu.Lock()
		delete(f.videoCache, key)
		f.videoMu.Unlock()
		src2, rErr := f.resolveCached(channel, id)
		if rErr != nil {
			f.registerPending(channel, id)
			http.Error(w, "unavailable", http.StatusNotFound)
			return
		}
		resp, err = fetchUpstream(src2)
		if err != nil {
			http.Error(w, "upstream failed", http.StatusBadGateway)
			return
		}
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "video/mp4")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleRefresh kicks the scanner right now instead of waiting for the next
// polling tick. Results arrive through the regular SSE stream.
func (f *Feed) handleRefresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost || f.Refresh == nil {
		http.Error(w, `{"error":"unsupported"}`, http.StatusMethodNotAllowed)
		return
	}
	f.Refresh()
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleMute silences (or unsilences) popups for one channel — the feed page
// keeps receiving its messages either way.
func (f *Feed) handleMute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost || f.SetMuted == nil {
		http.Error(w, `{"error":"unsupported"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel string `json:"channel"`
		Muted   bool   `json:"muted"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	name := normalizeChannel(body.Channel)
	if err := f.SetMuted(name, body.Muted); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": name, "muted": body.Muted})
}

// handleSettings: GET reads the runtime toggles, POST flips them. Changes
// persist to config.json through the callbacks.
func (f *Feed) handleSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	switch r.Method {
	case http.MethodGet:
		popups, sound := true, true
		if f.GetSettings != nil {
			popups, sound = f.GetSettings()
		}
		theme := "dark"
		if f.GetTheme != nil {
			theme = f.GetTheme()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"popups": popups, "sound": sound, "theme": theme})

	case http.MethodPost:
		if f.SetSettings == nil {
			http.Error(w, `{"error":"unsupported"}`, http.StatusNotImplemented)
			return
		}
		var body struct {
			Popups *bool   `json:"popups"`
			Sound  *bool   `json:"sound"`
			Theme  *string `json:"theme"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		popups, sound := f.SetSettings(body.Popups, body.Sound)
		theme := "dark"
		if body.Theme != nil && f.SetTheme != nil && (*body.Theme == "dark" || *body.Theme == "light") {
			f.SetTheme(*body.Theme)
		}
		if f.GetTheme != nil {
			theme = f.GetTheme()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "popups": popups, "sound": sound, "theme": theme})

	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

func (f *Feed) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 32)
	f.mu.Lock()
	f.clients[ch] = struct{}{}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.clients, ch)
		f.mu.Unlock()
	}()

	_, _ = fmt.Fprint(w, ": connected\n\n")
	fl.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			fl.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// renderFeedItem builds one chat bubble. Sanitising happens in the shared
// helpers, so hostile channel content never becomes live markup.
func renderFeedItem(m Message) string {
	var b strings.Builder
	b.WriteString(`<article class="msg" id="m` + html.EscapeString(itemKey(m)) + `" data-channel="` + html.EscapeString(m.Channel) + `">`)

	// No outbound links: the channel name filters the timeline in-place and
	// the timestamp is plain text. Nothing here ever opens Telegram.
	b.WriteString(`<div class="msghead">` +
		`<span class="msgchan" data-chan="` + html.EscapeString(m.Channel) + `">@` + html.EscapeString(m.Channel) + `</span>` +
		`<span class="msgtime">` + html.EscapeString(feedStamp(m.Time)) + `</span>` +
		`</div>`)

	b.WriteString(feedMediaHTML(m))

	if strings.TrimSpace(m.Text) != "" {
		b.WriteString(`<div class="msgtext">` + richText(m.Text) + `</div>`)
	}
	b.WriteString(`</article>`)
	return b.String()
}

// feedMediaHTML renders a post's attachment for the chat page (a real
// browser — unlike the popup, which runs on the legacy engine):
//   - a clip without a duration is GIF-like → silent auto-looping preview
//   - a clip with a duration is a real video → full controls: play/pause,
//     seeking, and sound
//   - a long video Telegram serves only as a preview frame → the frame with
//     a play button; clicking swaps in Telegram's embedded player, which
//     streams the full video right inside the page
func feedMediaHTML(m Message) string {
	badge := ""
	if m.Duration != "" {
		badge = `<span class="durbadge">` + html.EscapeString(m.Duration) + `</span>`
	}
	poster := ""
	if m.VideoThumb != "" {
		poster = ` poster="` + html.EscapeString(m.VideoThumb) + `"`
	}

	proxy := ` data-proxy="/api/media?channel=` + html.EscapeString(m.Channel) + `&amp;id=` + strconv.Itoa(m.ID) + `"`

	switch {
	case m.Video != "" && m.Duration == "":
		return `<div class="photo"><div class="vidwrap">` +
			`<video src="` + html.EscapeString(m.Video) + `"` + poster + proxy +
			` autoplay muted loop playsinline></video></div></div>`

	case m.Video != "":
		return `<div class="photo"><div class="vidwrap">` +
			`<video src="` + html.EscapeString(m.Video) + `"` + poster + proxy +
			` controls preload="metadata" playsinline></video>` +
			`<span class="durbadge durtop">` + html.EscapeString(m.Duration) + `</span>` +
			`</div></div>`

	case m.VideoThumb != "":
		embed := html.EscapeString(m.Channel) + "/" + strconv.Itoa(m.ID)
		return `<div class="photo"><div class="vidwrap embedwrap" data-embed="` + embed + `">` +
			`<img src="` + html.EscapeString(m.VideoThumb) + `" alt="">` +
			`<div class="playbtn"><span></span></div>` + badge + `</div></div>`

	case m.Photo != "":
		return `<div class="photo"><img src="` + html.EscapeString(m.Photo) + `" alt=""></div>`
	}
	return ""
}

func feedStamp(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return t.Local().Format("02/01 15:04")
}

// OpenBrowser opens the feed. With appWindow=false (the default setup) it
// opens a regular TAB in Chrome — reusing an already-open Chrome window when
// there is one. With appWindow=true it opens a standalone app-style window.
// Either way it falls back gracefully to the system default browser.
func OpenBrowser(url string, appWindow bool) {
	if runtime.GOOS != "windows" {
		fmt.Printf("דף הערוץ החי: %s\n", url)
		return
	}

	if appWindow {
		for _, p := range browserCandidates(false) {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			cmd := exec.Command(p, "--app="+url, "--window-size=1000,900")
			hideWindow(cmd)
			if cmd.Start() == nil {
				return
			}
		}
	} else {
		// Plain URL argument → opens as a tab in the existing window.
		for _, p := range browserCandidates(true) {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			cmd := exec.Command(p, url)
			hideWindow(cmd)
			if cmd.Start() == nil {
				return
			}
		}
	}

	cmd := exec.Command("cmd", "/c", "start", "", url)
	hideWindow(cmd)
	_ = cmd.Start()
}

// browserCandidates lists likely install locations; chromeFirst puts Chrome
// ahead of Edge (used for tab mode, where the user asked for Chrome).
func browserCandidates(chromeFirst bool) []string {
	var chromes, edges []string
	add := func(list *[]string, root, rel string) {
		if root != "" {
			*list = append(*list, filepath.Join(root, rel))
		}
	}
	edge := `Microsoft\Edge\Application\msedge.exe`
	chrome := `Google\Chrome\Application\chrome.exe`
	add(&chromes, os.Getenv("ProgramFiles"), chrome)
	add(&chromes, os.Getenv("ProgramFiles(x86)"), chrome)
	add(&chromes, os.Getenv("LocalAppData"), chrome)
	add(&edges, os.Getenv("ProgramFiles(x86)"), edge)
	add(&edges, os.Getenv("ProgramFiles"), edge)
	if chromeFirst {
		return append(chromes, edges...)
	}
	return append(edges, chromes...)
}
