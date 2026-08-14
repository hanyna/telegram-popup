package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A popup click must reuse the open tab (broadcast a goto) and only report
// clients:0 — the "open a new page" fallback — when no tab is listening.

func TestGotoWithNoOpenTabReportsZeroClients(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(url + "/api/goto?channel=elisha_yered&id=123")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Clients int `json:"clients"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Clients != 0 {
		t.Fatalf("no tab is open — clients must be 0, got %d", got.Clients)
	}
}

func TestGotoBroadcastsToOpenTab(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}

	// Open an SSE "tab".
	stream, err := http.Get(url + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	time.Sleep(150 * time.Millisecond) // let the client register

	resp, err := http.Get(url + "/api/goto?channel=Moshepargod&id=456")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Clients int `json:"clients"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Clients != 1 {
		t.Fatalf("one tab is open — clients must be 1, got %d", got.Clients)
	}

	// The tab must receive the goto event.
	deadline := time.After(3 * time.Second)
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	for {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"goto"`) {
				if !strings.Contains(line, "Moshepargod") || !strings.Contains(line, "456") {
					t.Fatalf("goto event malformed: %s", line)
				}
				return
			}
		case <-deadline:
			t.Fatal("tab never received the goto broadcast")
		}
	}
}

func TestGotoRejectsBadInput(t *testing.T) {
	f := NewFeed()
	url, _ := f.Start(0)
	for _, q := range []string{"", "channel=x", "channel=x&id=0", "id=5"} {
		resp, err := http.Get(url + "/api/goto?" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("query %q: want 400, got %d", q, resp.StatusCode)
		}
	}
}

// The page must carry the tab-side machinery, and the popup the caller side.
func TestGotoWiringExistsOnBothEnds(t *testing.T) {
	for _, want := range []string{"function gotoMessage", "'goto'", "#msg=", "handleHashTarget"} {
		if !strings.Contains(feedPage, want) {
			t.Errorf("feed page missing goto wiring: %q", want)
		}
	}
	for _, want := range []string{"$Goto", "Invoke-RestMethod", "AppActivate"} {
		if !strings.Contains(popupScript, want) {
			t.Errorf("popup script missing goto wiring: %q", want)
		}
	}
}
