package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Avatars vanished because identities arrived after page boot and were lost
// on restart. These tests pin both halves of the fix.

func TestChannelInfoIsBroadcastToOpenTabs(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := http.Get(url + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	time.Sleep(150 * time.Millisecond)

	f.SetChannelInfo("elisha_yered", ChannelInfo{Name: "אלישע ירד", Photo: "https://cdn.example/p.jpg"})

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
			if strings.Contains(line, "chaninfo") {
				// The broadcast must carry the STABLE proxy address, never the
				// rotting CDN URL.
				if !strings.Contains(line, "אלישע ירד") || !strings.Contains(line, "/api/avatar?channel=elisha_yered") {
					t.Fatalf("chaninfo event incomplete: %s", line)
				}
				return
			}
		case <-deadline:
			t.Fatal("open tab never received the channel identity")
		}
	}
}

func TestChannelInfoNotRebroadcastWhenUnchanged(t *testing.T) {
	f := NewFeed()
	info := ChannelInfo{Name: "א", Photo: "p"}
	f.SetChannelInfo("ch", info)

	ch := make(chan string, 4)
	f.mu.Lock()
	f.clients[ch] = struct{}{}
	f.mu.Unlock()

	f.SetChannelInfo("ch", info) // identical — must stay silent
	select {
	case msg := <-ch:
		t.Fatalf("unchanged identity must not spam tabs, got: %s", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStateRemembersChannelInfos(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"
	s := State{
		LastIDs: map[string]int{"ch": 42},
		Infos:   map[string]ChannelInfo{"ch": {Name: "ערוץ", Photo: "https://cdn.example/a.jpg"}},
	}
	saveState(path, s)

	loaded := loadState(path)
	got, ok := loaded.Infos["ch"]
	if !ok || got.Name != "ערוץ" || got.Photo != "https://cdn.example/a.jpg" {
		t.Fatalf("channel identity lost across restart: %+v", loaded.Infos)
	}
}

func TestChannelsAPIServesRememberedInfo(t *testing.T) {
	f := NewFeed()
	f.ListChannels = func() []string { return []string{"ch"} }
	f.SetChannelInfo("ch", ChannelInfo{Name: "שם אמיתי", Photo: "https://cdn.example/x.jpg"})
	url, _ := f.Start(0)
	resp, err := http.Get(url + "/api/channels")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Channels []struct {
			Title string `json:"title"`
			Photo string `json:"photo"`
		} `json:"channels"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if len(got.Channels) != 1 || got.Channels[0].Title != "שם אמיתי" || got.Channels[0].Photo == "" {
		t.Fatalf("channels API missing identity: %+v", got.Channels)
	}
}

func TestPageHandlesChaninfoAndAvatarReferrer(t *testing.T) {
	for _, want := range []string{"'chaninfo'", "referrerPolicy = 'no-referrer'"} {
		if !strings.Contains(feedPage, want) {
			t.Errorf("feed page missing %q", want)
		}
	}
}

// The avatar proxy: one stable URL per channel, bytes cached server-side.
func TestAvatarProxyCachesAndServes(t *testing.T) {
	var cdnHits int
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEGDATA-ELISHA"))
	}))
	defer cdn.Close()

	f := NewFeed()
	f.SetChannelInfo("elisha_yered", ChannelInfo{Name: "אלישע", Photo: cdn.URL + "/photo.jpg?token=abc"})
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		resp, err := http.Get(url + "/api/avatar?channel=elisha_yered")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != "JPEGDATA-ELISHA" {
			t.Fatalf("request %d: got %d / %q", i, resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("content type %q", ct)
		}
	}
	if cdnHits != 1 {
		t.Errorf("CDN must be hit exactly once (cache!), got %d", cdnHits)
	}
}

func TestAvatarProxyUnknownChannel(t *testing.T) {
	f := NewFeed()
	url, _ := f.Start(0)
	resp, err := http.Get(url + "/api/avatar?channel=nobody")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown channel: want 404, got %d", resp.StatusCode)
	}
}

// Rotating CDN URLs must not re-broadcast identities every sweep.
func TestRotatingPhotoURLDoesNotSpamTabs(t *testing.T) {
	f := NewFeed()
	f.SetChannelInfo("ch", ChannelInfo{Name: "שם", Photo: "https://cdn.example/p.jpg?token=1"})

	ch := make(chan string, 4)
	f.mu.Lock()
	f.clients[ch] = struct{}{}
	f.mu.Unlock()

	// Same name, same photo-presence, new token — cosmetic churn.
	f.SetChannelInfo("ch", ChannelInfo{Name: "שם", Photo: "https://cdn.example/p.jpg?token=2"})
	select {
	case msg := <-ch:
		t.Fatalf("token rotation must not rebroadcast, got: %s", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

// The page must receive the STABLE proxy address, not the rotting CDN URL.
func TestChannelsAPIExposesStableAvatarURL(t *testing.T) {
	f := NewFeed()
	f.ListChannels = func() []string { return []string{"ch"} }
	f.SetChannelInfo("ch", ChannelInfo{Name: "שם", Photo: "https://cdn.example/p.jpg?token=zzz"})
	url, _ := f.Start(0)
	resp, err := http.Get(url + "/api/channels")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Channels []struct {
			Photo string `json:"photo"`
		} `json:"channels"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if len(got.Channels) != 1 || got.Channels[0].Photo != "/api/avatar?channel=ch" {
		t.Fatalf("page must get the stable proxy URL, got %+v", got.Channels)
	}
}
