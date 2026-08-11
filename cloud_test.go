package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// Cloud mode must never leak into desktop behaviour, and the access key must
// actually keep strangers out — this feed is someone's private page once it
// is reachable from the whole internet.

func TestDesktopModeUntouchedWithoutPortEnv(t *testing.T) {
	t.Setenv("PORT", "")
	cfg := DefaultConfig()
	cfg.Popups = true
	bind, key := applyCloudEnv(&cfg)
	if bind != "" || key != "" {
		t.Fatalf("no PORT env must mean desktop mode, got bind=%q key=%q", bind, key)
	}
	if !cfg.Popups {
		t.Error("desktop config must not be modified")
	}
}

func TestCloudEnvRewritesConfig(t *testing.T) {
	t.Setenv("PORT", "10000")
	t.Setenv("TGPOPUP_KEY", "sod-gadol")
	t.Setenv("TGPOPUP_CHANNELS", "elisha_yered, @hakolhayehudi; https://t.me/s/Moshepargod")

	cfg := DefaultConfig()
	cfg.Popups = true
	cfg.Sound = true
	cfg.OpenFeedOnStart = true

	bind, key := applyCloudEnv(&cfg)
	if bind != "0.0.0.0" {
		t.Errorf("cloud must bind all interfaces, got %q", bind)
	}
	if key != "sod-gadol" {
		t.Errorf("access key not picked up, got %q", key)
	}
	if cfg.FeedPort != 10000 {
		t.Errorf("PORT must set the feed port, got %d", cfg.FeedPort)
	}
	if cfg.Popups || cfg.Sound || cfg.OpenFeedOnStart || cfg.AppWindow {
		t.Error("headless mode must switch every desktop feature off")
	}
	want := []string{"elisha_yered", "hakolhayehudi", "Moshepargod"}
	if len(cfg.Channels) != len(want) {
		t.Fatalf("channels = %v, want %v", cfg.Channels, want)
	}
	for i, ch := range want {
		if cfg.Channels[i] != ch {
			t.Errorf("channel %d = %q, want %q", i, cfg.Channels[i], ch)
		}
	}
}

func TestEnvChannelsHandlesAllPasteFormats(t *testing.T) {
	t.Setenv("TGPOPUP_CHANNELS", "a,b, @c ;https://t.me/d\ne,a")
	got := envChannels()
	want := []string{"a", "b", "c", "d", "e"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestDesktopStillBindsLoopbackOnly(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "127.0.0.1") {
		t.Errorf("desktop feed must stay on loopback, got %s", url)
	}
}

// The full lock-out / let-in cycle a real visitor goes through.
func TestAccessKeyProtectsEverything(t *testing.T) {
	f := NewFeed()
	f.AccessKey = "mafteah-123"
	f.Add([]Message{{Channel: "ch", ID: 1, Text: "סוד", Time: "2026-08-11T10:00:00Z"}})

	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}

	plain := &http.Client{}

	// 1. No key → the page is a login screen, not the feed.
	resp, err := plain.Get(url + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("page without key: want 401, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "מפתח") {
		t.Error("visitor should see the Hebrew login page")
	}
	if strings.Contains(string(body), "סוד") {
		t.Error("feed content LEAKED to an unauthenticated visitor")
	}

	// 2. No key → APIs refuse too (data, not just looks).
	resp, _ = plain.Get(url + "/api/messages")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || strings.Contains(string(body), "סוד") {
		t.Errorf("API without key must 401 with no data, got %d: %s", resp.StatusCode, body)
	}

	// 3. Wrong key → still out.
	resp, _ = plain.Get(url + "/?k=wrong")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong key: want 401, got %d", resp.StatusCode)
	}

	// 4. Right key → in, and the cookie carries the session from then on.
	jar, _ := cookiejar.New(nil)
	authed := &http.Client{Jar: jar}
	resp, err = authed.Get(url + "/?k=mafteah-123")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct key: want 200, got %d", resp.StatusCode)
	}

	// 5. Follow-up API call with cookie only (like the EventSource does).
	resp, _ = authed.Get(url + "/api/messages")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "סוד") {
		t.Errorf("cookie session must reach the data, got %d", resp.StatusCode)
	}
}

// Without a key configured, desktop behaviour is untouched — everything open.
func TestNoKeyMeansNoGate(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(url + "/api/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("no key configured → no gate, got %d", resp.StatusCode)
	}
}
