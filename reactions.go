package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
)

// Likes / reactions of the site's own visitors. They live in the app itself
// (reactions.json) and are never sent to Telegram — there is no account to
// send one with.
//
// Every device has one reaction per message (an anonymous device cookie tells
// devices apart), and everybody sees the totals — so on an open site anyone
// can like, and nobody can change someone else's like. On the desktop there
// is a single "local" device. (On the free cloud plan the disk is wiped on a
// redeploy, like the archive; on the desktop reactions persist for good.)
type reactStore struct {
	mu   sync.Mutex
	path string
	m    map[string]map[string]string // message key ("channel_id") -> device -> emoji
}

const (
	localDevice       = "local"
	maxReactKeys      = 20000
	maxReactPerKey    = 3000
	maxReactKeyLength = 90
)

// reactAllowed mirrors REACT_SET in the page.
var reactAllowed = map[string]bool{"👍": true, "❤️": true, "🔥": true, "😮": true, "😢": true, "😂": true, "🙏": true, "✅": true}

var reReactKey = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func newReactStore(path string) *reactStore {
	rs := &reactStore{path: path, m: map[string]map[string]string{}}
	if path == "" {
		return rs
	}
	data, err := readFileMaybe(path)
	if err != nil || len(data) == 0 {
		return rs
	}
	if json.Unmarshal(data, &rs.m) != nil {
		// Before 8.4 the file was one reaction per message: {key: emoji}.
		rs.m = map[string]map[string]string{}
		var old map[string]string
		if json.Unmarshal(data, &old) == nil {
			for k, e := range old {
				if e != "" {
					rs.m[k] = map[string]string{localDevice: e}
				}
			}
		}
	}
	return rs
}

// mine: this device's reactions, key -> emoji.
func (rs *reactStore) mine(device string) map[string]string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := map[string]string{}
	for k, devs := range rs.m {
		if e := devs[device]; e != "" {
			out[k] = e
		}
	}
	return out
}

func countsOf(devs map[string]string) map[string]int {
	out := map[string]int{}
	for _, e := range devs {
		out[e]++
	}
	return out
}

// counts: totals for every message, key -> emoji -> how many devices.
func (rs *reactStore) counts() map[string]map[string]int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make(map[string]map[string]int, len(rs.m))
	for k, devs := range rs.m {
		out[k] = countsOf(devs)
	}
	return out
}

// set stores (or, with an empty emoji, clears) one device's reaction and
// returns the message's new totals. ok=false when the store is full.
func (rs *reactStore) set(key, device, emoji string) (map[string]int, bool) {
	rs.mu.Lock()
	devs := rs.m[key]
	if emoji == "" {
		delete(devs, device)
		if len(devs) == 0 {
			delete(rs.m, key)
		}
	} else {
		if devs == nil {
			if len(rs.m) >= maxReactKeys {
				rs.mu.Unlock()
				return nil, false
			}
			devs = map[string]string{}
			rs.m[key] = devs
		}
		if _, had := devs[device]; !had && len(devs) >= maxReactPerKey {
			rs.mu.Unlock()
			return countsOf(devs), false
		}
		devs[device] = emoji
	}
	totals := countsOf(rs.m[key])
	data, _ := json.Marshal(rs.m)
	path := rs.path
	rs.mu.Unlock()
	if path != "" {
		_ = writeFileAtomic(path, data, 0o644)
	}
	return totals, true
}

// reactDevice: who is reacting. Online: the anonymous device cookie (set here
// when missing). On the desktop there is one user.
func (f *Feed) reactDevice(w http.ResponseWriter, r *http.Request) string {
	if f.BindAddr == "" {
		return localDevice
	}
	return "d:" + ensureDeviceID(w, r)
}

// handleReactions: GET returns this device's reactions plus everyone's
// totals; POST {key,emoji} sets or (empty emoji) clears this device's one.
func (f *Feed) handleReactions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if f.reacts == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"reactions": map[string]string{}, "counts": map[string]any{}})
		return
	}
	switch r.Method {
	case http.MethodGet:
		dev := f.reactDevice(w, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"reactions": f.reacts.mine(dev), "counts": f.reacts.counts()})
	case http.MethodPost:
		var body struct {
			Key   string `json:"key"`
			Emoji string `json:"emoji"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil ||
			body.Key == "" || len(body.Key) > maxReactKeyLength || !reReactKey.MatchString(body.Key) {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if body.Emoji != "" && !reactAllowed[body.Emoji] {
			http.Error(w, `{"error":"bad emoji"}`, http.StatusBadRequest)
			return
		}
		dev := f.reactDevice(w, r)
		totals, ok := f.reacts.set(body.Key, dev, body.Emoji)
		if !ok {
			http.Error(w, `{"error":"full"}`, http.StatusInsufficientStorage)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "key": body.Key, "emoji": body.Emoji, "counts": totals})
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}
