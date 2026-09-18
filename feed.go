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
	Text    string `json:"text"` // full plain text, untruncated, tags stripped — for external consumers (e.g. TTS/IVR integrations)
}

type Feed struct {
	mu      sync.Mutex
	items   []FeedItem // sorted newest first
	keys    map[string]bool
	clients map[chan string]struct{}

	infoMu sync.Mutex
	infos  map[string]ChannelInfo // channel username -> display identity

	// Avatar bytes, cached server-side. Telegram's photo URLs carry rotating
	// tokens and expire like video URLs do — serving the image ourselves
	// gives the page one stable URL per channel that never breaks.
	avatarMu    sync.Mutex
	avatarCache map[string]avatarEntry

	// Wired in by main so the feed stays free of scanning/config logic.
	FetchOlder    func(channel string, beforeID int) ([]Message, error)
	ListChannels  func() []string
	AddChannel    func(name string) (string, error)
	RemoveChannel func(name string) error
	GetSettings   func() (popups, sound bool)
	SetSettings   func(popups, sound *bool) (bool, bool)
	IsMuted       func(name string) bool
	SetMuted      func(name string, muted bool) error
	IsPinned      func(name string) bool
	SetPinned     func(name string, pinned bool) error
	GetKeywords   func() (include, exclude []string)
	SetKeywords   func(include, exclude []string)
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
	// Self-update hooks (desktop only): check the repo for a newer version,
	// and download+apply it. Nil in the cloud (Render updates itself).
	CheckUpdate func() (latest string, err error)
	ApplyUpdate func() error

	videoMu    sync.Mutex
	videoCache map[string]string    // "channel/id" -> direct mp4 URL
	pending    map[string]time.Time // failed resolutions awaiting background retry

	// Cloud hosting knobs (see cloud.go). Zero values = desktop behaviour:
	// bind loopback only, no access key.
	BindAddr  string
	AccessKey string
}

// SetChannelInfo records a channel's display name and photo. New or changed
// identities are pushed to every open tab immediately — the sidebar's avatars
// must not wait for a page reload (the paced scanner delivers infos slowly,
// well after the page has booted).
func (f *Feed) SetChannelInfo(name string, info ChannelInfo) {
	if info.Name == "" && info.Photo == "" {
		return
	}
	f.infoMu.Lock()
	old, had := f.infos[name]
	f.infos[name] = info
	f.infoMu.Unlock()
	// Telegram rotates photo URLs on every fetch, so comparing raw URLs would
	// re-broadcast (and re-render the sidebar) every sweep. Only a real
	// change matters: a new display name, or an avatar appearing at all.
	changed := !had || old.Name != info.Name || (old.Photo == "") != (info.Photo == "")
	if changed {
		photo := ""
		if info.Photo != "" {
			photo = "/api/avatar?channel=" + name
		}
		f.broadcast(map[string]any{
			"type": "chaninfo", "channel": name,
			"title": info.Name, "photo": photo,
		})
	}
}

// GetChannelInfo returns the recorded identity, if any.
func (f *Feed) GetChannelInfo(name string) (ChannelInfo, bool) {
	f.infoMu.Lock()
	defer f.infoMu.Unlock()
	info, ok := f.infos[name]
	return info, ok
}

const feedMaxItems = 800

type avatarEntry struct {
	data      []byte
	ctype     string
	fetchedAt time.Time
}

func NewFeed() *Feed {
	return &Feed{
		keys:        make(map[string]bool),
		clients:     make(map[chan string]struct{}),
		infos:       make(map[string]ChannelInfo),
		videoCache:  make(map[string]string),
		pending:     make(map[string]time.Time),
		avatarCache: make(map[string]avatarEntry),
	}
}

func itemKey(m Message) string { return m.Channel + "_" + strconv.Itoa(m.ID) }

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func toItem(m Message) FeedItem {
	ts := int64(0)
	if t, err := time.Parse(time.RFC3339, m.Time); err == nil {
		ts = t.Unix()
	}
	prev := strings.TrimSpace(strings.ReplaceAll(m.Text, "\n", " "))
	icon := ""
	switch {
	case len(m.Photos) > 1:
		icon, prev = "📷", nonEmpty(prev, "אלבום · "+strconv.Itoa(len(m.Photos))+" תמונות")
	case m.Video != "" || m.VideoThumb != "" || m.Round != "":
		icon, prev = "🎬", nonEmpty(prev, "סרטון")
	case m.Voice != "":
		icon, prev = "🎤", nonEmpty(prev, "הודעה קולית")
	case m.Sticker != "":
		icon, prev = "😊", nonEmpty(prev, "סטיקר")
	case m.Poll != "":
		icon, prev = "📊", nonEmpty(prev, m.Poll)
	case m.Doc != "":
		icon, prev = "📎", nonEmpty(prev, m.Doc)
	case m.Photo != "":
		icon, prev = "📷", nonEmpty(prev, "תמונה")
	}
	if icon != "" {
		prev = icon + " " + prev
	}
	if r := []rune(prev); len(r) > 80 {
		prev = string(r[:80]) + "…"
	}
	fullText := strings.TrimSpace(m.Text)
	if fullText == "" {
		// Fall back to the same non-text description used for the preview
		// (e.g. "תמונה", "סרטון"), just without the icon prefix or truncation.
		fullText = strings.TrimSpace(strings.ReplaceAll(prev, icon+" ", ""))
	}
	return FeedItem{ID: m.ID, Channel: m.Channel, Key: itemKey(m), TS: ts, Preview: prev, HTML: renderFeedItem(m), Text: fullText}
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
	mux.HandleFunc("/api/pin", f.handlePin)
	mux.HandleFunc("/api/keywords", f.handleKeywords)
	mux.HandleFunc("/api/refresh", f.handleRefresh)
	mux.HandleFunc("/api/video", f.handleVideo)
	mux.HandleFunc("/api/media", f.handleMedia)
	mux.HandleFunc("/api/search", f.handleSearch)
	mux.HandleFunc("/api/autostart", f.handleAutostart)
	mux.HandleFunc("/api/status", f.handleStatus)
	mux.HandleFunc("/api/goto", f.handleGoto)
	mux.HandleFunc("/api/avatar", f.handleAvatar)
	mux.HandleFunc("/api/update", f.handleUpdate)

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
			Name   string `json:"name"`
			Title  string `json:"title"`
			Photo  string `json:"photo"`
			Muted  bool   `json:"muted"`
			Pinned bool   `json:"pinned"`
		}
		entries := make([]entry, 0, len(names))
		for _, n := range names {
			e := entry{Name: n, Title: "@" + n}
			if info, ok := f.GetChannelInfo(n); ok {
				if info.Name != "" {
					e.Title = info.Name
				}
				if info.Photo != "" {
					// One stable, never-expiring address per channel — the
					// app serves the image itself (Telegram's URLs rot).
					e.Photo = "/api/avatar?channel=" + n
				}
			}
			if f.IsMuted != nil {
				e.Muted = f.IsMuted(n)
			}
			if f.IsPinned != nil {
				e.Pinned = f.IsPinned(n)
			}
			entries = append(entries, e)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"channels": entries,
			// locked=true → the page hides "+ הוסף ערוץ" and the ✕ buttons.
			"locked": f.AddChannel == nil && f.RemoveChannel == nil,
		})

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

	blocked := tgLimiter.AnyBlocked(names)
	banner := ""
	switch {
	case len(names) == 0:
		banner = "לא מוגדרים ערוצים. הוסף ערוץ בכפתור למטה."
	case blocked && okCount == 0:
		banner = "טלגרם מגבילה זמנית את הבקשות מהמחשב הזה. התוכנה ממתינה ותנסה שוב לבד — " +
			"בדרך כלל זה חוזר לעצמו תוך כמה דקות. אין צורך לעשות כלום."
	case blocked:
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
		"blocked":  blocked,
	})
}

// handleAvatar serves a channel's profile picture from the app's own cache.
// Telegram's CDN URLs rotate and expire; this endpoint is the page's one
// stable address per channel. Bytes are cached for a day, then refreshed
// against whatever URL the latest scan recorded.
func (f *Feed) handleAvatar(w http.ResponseWriter, r *http.Request) {
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	f.avatarMu.Lock()
	cached, ok := f.avatarCache[channel]
	f.avatarMu.Unlock()
	if ok && time.Since(cached.fetchedAt) < 24*time.Hour {
		w.Header().Set("Content-Type", cached.ctype)
		w.Header().Set("Cache-Control", "public, max-age=21600") // browser: 6h
		_, _ = w.Write(cached.data)
		return
	}

	info, has := f.GetChannelInfo(channel)
	if !has || info.Photo == "" {
		http.Error(w, "no avatar", http.StatusNotFound)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, info.Photo, nil)
	if err != nil {
		http.Error(w, "bad upstream", http.StatusBadGateway)
		return
	}
	req.Header.Set("User-Agent", desktopUA)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		// A stale cached copy beats a broken circle.
		if ok {
			w.Header().Set("Content-Type", cached.ctype)
			_, _ = w.Write(cached.data)
			return
		}
		http.Error(w, "unavailable", http.StatusNotFound)
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || len(data) == 0 {
		http.Error(w, "unavailable", http.StatusNotFound)
		return
	}
	ctype := resp.Header.Get("Content-Type")
	if ctype == "" {
		ctype = "image/jpeg"
	}
	f.avatarMu.Lock()
	f.avatarCache[channel] = avatarEntry{data: data, ctype: ctype, fetchedAt: time.Now()}
	f.avatarMu.Unlock()

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=21600")
	_, _ = w.Write(data)
}

// handleUpdate: GET checks the GitHub repo for a newer release; POST
// downloads it and restarts the app with the new binary. The page shows a
// one-click update pill when GET reports a newer version.
func (f *Feed) handleUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.Method {
	case http.MethodGet:
		if f.CheckUpdate == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"current": version, "available": false, "supported": false,
			})
			return
		}
		latest, err := f.CheckUpdate()
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"current": version, "available": false, "supported": true,
				"error": "בדיקת העדכון נכשלה — נסה שוב מאוחר יותר",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"current": version, "latest": latest, "supported": true,
			"available": compareVersions(latest, version) > 0,
		})
	case http.MethodPost:
		if f.ApplyUpdate == nil {
			http.Error(w, `{"error":"unsupported"}`, http.StatusNotImplemented)
			return
		}
		if err := f.ApplyUpdate(); err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		// The app is about to be stopped and swapped by the updater script.
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "restarting": true})
	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

// handleGoto is what a popup click calls: every open tab jumps to the given
// message (selecting its channel, scrolling to it, playing its video in
// place). The response tells the caller whether any tab was listening — if
// not, the popup opens a fresh page instead.
func (f *Feed) handleGoto(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || channel == "" || id < 1 {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	clients := len(f.clients)
	f.mu.Unlock()
	if clients > 0 {
		f.broadcast(map[string]any{"type": "goto", "channel": channel, "id": id})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"clients": clients})
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

// handlePin stars a channel to the top of the sidebar.
func (f *Feed) handlePin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost || f.SetPinned == nil {
		http.Error(w, `{"error":"unsupported"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel string `json:"channel"`
		Pinned  bool   `json:"pinned"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	name := normalizeChannel(body.Channel)
	if err := f.SetPinned(name, body.Pinned); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": name, "pinned": body.Pinned})
}

// handleKeywords lets the page read and edit the popup keyword filters that
// previously lived only in config.json.
func (f *Feed) handleKeywords(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.Method {
	case http.MethodGet:
		var inc, exc []string
		if f.GetKeywords != nil {
			inc, exc = f.GetKeywords()
		}
		if inc == nil {
			inc = []string{}
		}
		if exc == nil {
			exc = []string{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"include": inc, "exclude": exc})
	case http.MethodPost:
		if f.SetKeywords == nil {
			http.Error(w, `{"error":"unsupported"}`, http.StatusNotImplemented)
			return
		}
		var body struct {
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		f.SetKeywords(cleanKeywords(body.Include), cleanKeywords(body.Exclude))
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

// cleanKeywords trims, drops empties and dedupes, keeping order.
func cleanKeywords(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, k := range in {
		k = strings.TrimSpace(k)
		if k == "" || seen[strings.ToLower(k)] {
			continue
		}
		seen[strings.ToLower(k)] = true
		out = append(out, k)
	}
	return out
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
//   - photo albums become a grid, every photo opens in the lightbox
//   - a clip without a duration is GIF-like → silent auto-looping preview
//   - a clip with a duration is a real video → full controls
//   - round "video notes" play in a circle, like in Telegram
//   - voice messages get an audio player with a 🎤 label
//   - stickers, polls, file attachments and link-preview cards all render
//   - a long video Telegram serves only as a preview frame → the frame with
//     a play button; clicking streams it through the app
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

	// User-playable videos stream through the app's own proxy from the very
	// first byte: the proxy re-resolves Telegram's short-lived URLs on every
	// request, so a months-old archived post plays exactly like a fresh one —
	// no expired-token error, no retry dance. preload="none" means NOTHING is
	// fetched (and nothing can possibly sound) until the user presses play.
	proxySrc := "/api/media?channel=" + html.EscapeString(m.Channel) + "&amp;id=" + strconv.Itoa(m.ID)

	var b strings.Builder

	switch {
	case len(m.Photos) > 1:
		cls := "album"
		if len(m.Photos) == 2 {
			cls += " two"
		}
		b.WriteString(`<div class="photo"><div class="` + cls + `">`)
		for _, p := range m.Photos {
			b.WriteString(`<img src="` + html.EscapeString(p) + `" alt="" loading="lazy">`)
		}
		b.WriteString(`</div></div>`)

	case m.Round != "":
		b.WriteString(`<div class="photo"><div class="roundwrap">` +
			`<video class="roundvid" src="` + proxySrc + `"` +
			` controls playsinline preload="none"></video></div></div>`)

	case m.Video != "" && m.Duration == "":
		// GIF-like: silent looping preview — but NOT autoplay-in-markup.
		// Dozens of history GIFs all decoding at once made the whole page
		// crawl; the page plays each one only while it is actually on screen
		// (IntersectionObserver), and preload="none" keeps off-screen ones
		// from even downloading.
		b.WriteString(`<div class="photo"><div class="vidwrap">` +
			`<video class="gifvid" src="` + html.EscapeString(m.Video) + `"` + poster + proxy +
			` muted loop playsinline preload="none"></video></div></div>`)

	case m.Video != "":
		b.WriteString(`<div class="photo"><div class="vidwrap">` +
			`<video src="` + proxySrc + `"` + poster +
			` controls preload="none" playsinline></video>` +
			`<span class="durbadge durtop">` + html.EscapeString(m.Duration) + `</span>` +
			`</div></div>`)

	case m.VideoThumb != "":
		embed := html.EscapeString(m.Channel) + "/" + strconv.Itoa(m.ID)
		b.WriteString(`<div class="photo"><div class="vidwrap embedwrap" data-embed="` + embed + `">` +
			`<img src="` + html.EscapeString(m.VideoThumb) + `" alt="">` +
			`<div class="playbtn"><span></span></div>` + badge + `</div></div>`)

	case m.Sticker != "":
		b.WriteString(`<div class="stickerbox"><img src="` + html.EscapeString(m.Sticker) + `" alt="" loading="lazy"></div>`)

	case m.Photo != "":
		b.WriteString(`<div class="photo"><img src="` + html.EscapeString(m.Photo) + `" alt="" loading="lazy" decoding="async"></div>`)
	}

	if m.Voice != "" {
		dur := ""
		if m.VoiceDur != "" {
			dur = `<span class="voicedur">` + html.EscapeString(m.VoiceDur) + `</span>`
		}
		b.WriteString(`<div class="voicebox"><span class="voiceico">🎤</span>` +
			`<audio controls preload="none" src="` + html.EscapeString(m.Voice) + `"></audio>` + dur + `</div>`)
	}

	if m.Poll != "" {
		b.WriteString(`<div class="pollbox"><div class="pollq">📊 ` + html.EscapeString(m.Poll) + `</div>`)
		for _, opt := range m.PollOpts {
			pct := html.EscapeString(opt.Pct)
			width := strings.TrimSuffix(pct, "%")
			b.WriteString(`<div class="pollopt">` +
				`<div class="pollmeta"><span>` + html.EscapeString(opt.Text) + `</span><b>` + pct + `</b></div>` +
				`<div class="pollbar"><i style="width:` + width + `%"></i></div></div>`)
		}
		b.WriteString(`</div>`)
	}

	if m.Doc != "" {
		size := ""
		if m.DocSize != "" {
			size = `<div class="docsize">` + html.EscapeString(m.DocSize) + `</div>`
		}
		b.WriteString(`<div class="docbox"><span class="docico">📎</span><div class="docmeta">` +
			`<div class="docname">` + html.EscapeString(m.Doc) + `</div>` + size + `</div></div>`)
	}

	if m.LinkTitle != "" {
		inner := ""
		if m.LinkImage != "" {
			inner += `<img src="` + html.EscapeString(m.LinkImage) + `" alt="" loading="lazy">`
		}
		inner += `<div class="linkmeta"><div class="linktitle">` + html.EscapeString(m.LinkTitle) + `</div>`
		if m.LinkDesc != "" {
			desc := m.LinkDesc
			if r := []rune(desc); len(r) > 160 {
				desc = string(r[:160]) + "…"
			}
			inner += `<div class="linkdesc">` + html.EscapeString(desc) + `</div>`
		}
		inner += `</div>`
		if m.LinkHref != "" {
			b.WriteString(`<a class="linkcard" href="` + html.EscapeString(m.LinkHref) + `" target="_blank" rel="noopener">` + inner + `</a>`)
		} else {
			b.WriteString(`<div class="linkcard">` + inner + `</div>`)
		}
	}

	return b.String()
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
