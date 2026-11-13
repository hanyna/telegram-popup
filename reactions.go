package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Personal reactions live in "ערוץ חי" itself, not in one browser: the app
// keeps them in reactions.json and serves them to every device that logs in
// with the same key. They are never sent to Telegram — there is no account
// to send one with. (On the free cloud plan the disk is wiped on a redeploy,
// like the archive; on the desktop they persist for good.)
type reactStore struct {
	mu   sync.Mutex
	path string
	m    map[string]string // message key ("channel_id") -> emoji
}

func newReactStore(path string) *reactStore {
	rs := &reactStore{path: path, m: map[string]string{}}
	if path != "" {
		if data, err := readFileMaybe(path); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &rs.m)
		}
	}
	return rs
}

func (rs *reactStore) all() map[string]string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make(map[string]string, len(rs.m))
	for k, v := range rs.m {
		out[k] = v
	}
	return out
}

func (rs *reactStore) set(key, emoji string) {
	rs.mu.Lock()
	if emoji == "" {
		delete(rs.m, key)
	} else {
		rs.m[key] = emoji
	}
	data, _ := json.Marshal(rs.m)
	path := rs.path
	rs.mu.Unlock()
	if path != "" {
		_ = writeFileAtomic(path, data, 0o644)
	}
}

// handleReactions: GET returns every saved reaction; POST {key,emoji} sets or
// (empty emoji) clears one.
func (f *Feed) handleReactions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if f.reacts == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"reactions": map[string]string{}})
		return
	}
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"reactions": f.reacts.all()})
	case http.MethodPost:
		if !f.canReact(r) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		var body struct {
			Key   string `json:"key"`
			Emoji string `json:"emoji"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.Key == "" {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if len([]rune(body.Emoji)) > 8 {
			http.Error(w, `{"error":"bad emoji"}`, http.StatusBadRequest)
			return
		}
		f.reacts.set(body.Key, body.Emoji)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "key": body.Key, "emoji": body.Emoji})
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}
