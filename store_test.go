package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	s, err := NewStore(filepath.Join(t.TempDir(), "history"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return s
}

func TestStorePersistsAcrossRestarts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	s1, _ := NewStore(dir)
	s1.Append([]Message{
		{ID: 1, Channel: "chan", Text: "הודעה ראשונה", Time: "2026-08-01T10:00:00+00:00"},
		{ID: 2, Channel: "chan", Text: "הודעה שנייה", Time: "2026-08-02T10:00:00+00:00"},
	})

	// "Restart": a brand-new store over the same directory.
	s2, _ := NewStore(dir)
	got := s2.LoadRecent("chan", 10)
	if len(got) != 2 || got[0].ID != 1 || got[1].Text != "הודעה שנייה" {
		t.Fatalf("history lost across restart: %+v", got)
	}

	// The restarted index must dedupe re-appends of old messages.
	s2.Append([]Message{{ID: 1, Channel: "chan", Text: "הודעה ראשונה", Time: "2026-08-01T10:00:00+00:00"}})
	if n := len(s2.LoadRecent("chan", 10)); n != 2 {
		t.Fatalf("duplicate slipped in after restart: %d items", n)
	}
}

func TestStoreLoadRecentCapsAndOrders(t *testing.T) {
	s := newTestStore(t)
	var batch []Message
	for i := 1; i <= 50; i++ {
		batch = append(batch, Message{ID: i, Channel: "c", Text: "הודעה", Time: "2026-08-01T10:00:00+00:00"})
	}
	s.Append(batch)
	got := s.LoadRecent("c", 10)
	if len(got) != 10 || got[0].ID != 41 || got[9].ID != 50 {
		t.Fatalf("recent window wrong: first=%d last=%d n=%d", got[0].ID, got[len(got)-1].ID, len(got))
	}
}

func TestStoreSearch(t *testing.T) {
	s := newTestStore(t)
	s.Append([]Message{
		{ID: 1, Channel: "aaa", Text: "פיגוע בצומת תפוח", Time: "2026-08-01T10:00:00+00:00"},
		{ID: 2, Channel: "aaa", Text: "מזג אוויר נעים", Time: "2026-08-02T10:00:00+00:00"},
		{ID: 3, Channel: "bbb", Text: "עדכון על הפיגוע מהבוקר", Time: "2026-08-03T10:00:00+00:00"},
	})

	// Cross-channel, newest first.
	hits := s.Search("פיגוע", "", 10)
	if len(hits) != 2 || hits[0].ID != 3 || hits[1].ID != 1 {
		t.Fatalf("bad search results: %+v", hits)
	}
	// Channel-scoped.
	hits = s.Search("פיגוע", "aaa", 10)
	if len(hits) != 1 || hits[0].Channel != "aaa" {
		t.Fatalf("channel scope broken: %+v", hits)
	}
	// No match.
	if len(s.Search("לאנשוםמקום", "", 10)) != 0 {
		t.Fatal("phantom results")
	}
	// Empty query is a no-op, not a full dump.
	if len(s.Search("  ", "", 10)) != 0 {
		t.Fatal("empty query dumped archive")
	}
}

func TestStoreSkipsEmptyAndHostileChannelNames(t *testing.T) {
	s := newTestStore(t)
	s.Append([]Message{
		{ID: 1, Channel: "ok", Text: ""},                          // empty, no media → skipped
		{ID: 2, Channel: "../evil", Text: "טקסט"},                 // path traversal name → skipped
		{ID: 3, Channel: "ok", Text: "", Photo: "https://p.jpg"},  // media-only → kept
	})
	if n := s.Count(); n != 1 {
		t.Fatalf("expected exactly 1 stored, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "..", "evil.jsonl")); err == nil {
		t.Fatal("hostile channel name escaped the history directory")
	}
}

func TestStoreRemoveChannel(t *testing.T) {
	s := newTestStore(t)
	s.Append([]Message{{ID: 1, Channel: "gone", Text: "טקסט"}})
	s.RemoveChannel("gone")
	if len(s.LoadRecent("gone", 10)) != 0 {
		t.Fatal("archive survived channel removal")
	}
	// After removal the same message may be stored again (re-added channel).
	s.Append([]Message{{ID: 1, Channel: "gone", Text: "טקסט"}})
	if len(s.LoadRecent("gone", 10)) != 1 {
		t.Fatal("re-added channel cannot store again")
	}
}
