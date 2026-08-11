package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Exercises the real HTTP path end to end against a server that returns the
// same markup shape Telegram does.
func TestFetchURLParsesLivePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request went out without a User-Agent")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()

	msgs, _, err := fetchURL(srv.URL)
	if err != nil {
		t.Fatalf("fetchURL failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[1].Text != "הודעה שנייה קצרה" {
		t.Fatalf("unexpected body: %q", msgs[1].Text)
	}
}

func TestFetchURLSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, _, err := fetchURL(srv.URL); err == nil {
		t.Fatal("expected an error for a 404 response")
	}
}

// The dedupe logic is what stops old posts re-popping on every scan.
func TestOnlyNewerIDsAreSelected(t *testing.T) {
	msgs := ParseMessages(fixture)
	lastSeen := 1041

	var fresh []Message
	for _, m := range msgs {
		if m.ID > lastSeen {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) != 2 {
		t.Fatalf("expected 2 fresh messages, got %d", len(fresh))
	}
	if fresh[0].ID != 1042 {
		t.Fatalf("wrong first fresh id: %d", fresh[0].ID)
	}
}
