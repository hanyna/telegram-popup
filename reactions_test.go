package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reactPost(f *Feed, key, emoji string, ck *http.Cookie) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"key": key, "emoji": emoji})
	r := httptest.NewRequest("POST", "/api/reactions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	if ck != nil {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	f.handleReactions(rec, r)
	return rec
}

type reactAnswer struct {
	Reactions map[string]string         `json:"reactions"`
	Counts    map[string]map[string]int `json:"counts"`
}

func reactGet(f *Feed, ck *http.Cookie) reactAnswer {
	r := httptest.NewRequest("GET", "/api/reactions", nil)
	if ck != nil {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	f.handleReactions(rec, r)
	var got reactAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	return got
}

// Desktop: one user ("local"), reactions survive a restart.
func TestReactionStorePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reactions.json")
	f := NewFeed()
	f.reacts = newReactStore(path)

	if reactPost(f, "alpha_10", "🔥", nil).Code != 200 {
		t.Fatal("set failed")
	}
	reactPost(f, "beta_3", "👍", nil)

	got := reactGet(f, nil)
	if got.Reactions["alpha_10"] != "🔥" || got.Reactions["beta_3"] != "👍" || got.Counts["alpha_10"]["🔥"] != 1 {
		t.Fatalf("wrong reactions: %+v", got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	f2 := NewFeed()
	f2.reacts = newReactStore(path)
	if reactGet(f2, nil).Reactions["alpha_10"] != "🔥" {
		t.Fatal("reaction did not survive reload")
	}

	reactPost(f, "alpha_10", "", nil)
	if got := reactGet(f, nil); got.Reactions["alpha_10"] != "" || len(got.Counts["alpha_10"]) != 0 {
		t.Fatalf("clear failed: %+v", got)
	}

	// Junk is refused.
	if reactPost(f, "alpha_10", "<b>", nil).Code != 400 || reactPost(f, "a/../b", "👍", nil).Code != 400 {
		t.Fatal("bad emoji / key must be refused")
	}
}

// Online (also on an open site): every visitor can like, each device has one
// reaction per message, everybody sees the totals, nobody changes another's.
func TestReactionsPerDevice(t *testing.T) {
	f := NewFeed()
	f.BindAddr = "0.0.0.0"
	f.AccessKey = "" // open site
	f.reacts = newReactStore("")
	a := &http.Cookie{Name: "tgpopup_dev", Value: "device-aaaaaaaa"}
	b := &http.Cookie{Name: "tgpopup_dev", Value: "device-bbbbbbbb"}

	if reactPost(f, "alpha_1", "👍", a).Code != 200 || reactPost(f, "alpha_1", "👍", b).Code != 200 {
		t.Fatal("visitors must be able to like")
	}
	reactPost(f, "alpha_1", "👍", a) // same device again: still one like
	got := reactGet(f, a)
	if got.Counts["alpha_1"]["👍"] != 2 || got.Reactions["alpha_1"] != "👍" {
		t.Fatalf("want 2 likes, got %+v", got)
	}
	// b switches to a heart; a's like is untouched.
	reactPost(f, "alpha_1", "❤️", b)
	got = reactGet(f, a)
	if got.Counts["alpha_1"]["👍"] != 1 || got.Counts["alpha_1"]["❤️"] != 1 || got.Reactions["alpha_1"] != "👍" {
		t.Fatalf("after switch: %+v", got)
	}
	// b removes theirs.
	reactPost(f, "alpha_1", "", b)
	got = reactGet(f, b)
	if got.Counts["alpha_1"]["👍"] != 1 || got.Reactions["alpha_1"] != "" {
		t.Fatalf("after remove: %+v", got)
	}
	// A visitor without a cookie gets one and can like too.
	rec := reactPost(f, "alpha_2", "🔥", nil)
	if rec.Code != 200 || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("cookie-less visitor: code %d", rec.Code)
	}
}

// reactions.json from before 8.4 ({key: emoji}) is still read.
func TestReactionsOldFileFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reactions.json")
	_ = os.WriteFile(path, []byte(`{"alpha_5":"🙏"}`), 0o644)
	f := NewFeed()
	f.reacts = newReactStore(path)
	if got := reactGet(f, nil); got.Reactions["alpha_5"] != "🙏" || got.Counts["alpha_5"]["🙏"] != 1 {
		t.Fatalf("old file not migrated: %+v", got)
	}
}

func TestReactionsNilFeedSafe(t *testing.T) {
	f := NewFeed() // reacts == nil
	rec := httptest.NewRecorder()
	f.handleReactions(rec, httptest.NewRequest("GET", "/api/reactions", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "reactions") {
		t.Fatalf("nil-feed reactions should be safe, got %d %s", rec.Code, rec.Body.String())
	}
}
