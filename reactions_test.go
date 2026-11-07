package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReactionStorePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reactions.json")
	f := NewFeed()
	f.reacts = newReactStore(path)

	// set two
	post := func(key, emoji string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"key": key, "emoji": emoji})
		r := httptest.NewRequest("POST", "/api/reactions", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.handleReactions(rec, r)
		return rec
	}
	if post("alpha_10", "🔥").Code != 200 {
		t.Fatal("set failed")
	}
	post("beta_3", "👍")

	// GET returns both
	rec := httptest.NewRecorder()
	f.handleReactions(rec, httptest.NewRequest("GET", "/api/reactions", nil))
	var got struct {
		Reactions map[string]string `json:"reactions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Reactions["alpha_10"] != "🔥" || got.Reactions["beta_3"] != "👍" {
		t.Fatalf("wrong reactions: %+v", got.Reactions)
	}

	// persisted to disk and reloaded by a fresh store
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	f2 := NewFeed()
	f2.reacts = newReactStore(path)
	if f2.reacts.all()["alpha_10"] != "🔥" {
		t.Fatal("reaction did not survive reload")
	}

	// clearing removes it
	post("alpha_10", "")
	if f.reacts.all()["alpha_10"] != "" {
		t.Fatal("clear failed")
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
