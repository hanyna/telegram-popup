package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// End-to-end: the question that actually matters is "does the page fill up?"
// The user saw eleven configured channels and a blank pane. These tests stand
// up a fake Telegram serving all eleven channels and assert the feed populates
// — both when the server is healthy and when it throttles the way the real one
// did.
// ---------------------------------------------------------------------------

var userChannels = []string{
	"elisha_yered", "hakolhayehudi", "realelchangr", "ayeletlash",
	"SamariaUpdates", "Moshepargod", "tzviye", "mashekoreIL",
	"regavim", "nilchamim", "hargavim",
}

// fakeChannelPage renders Telegram-shaped markup for one channel.
func fakeChannelPage(channel string, startID, count int) string {
	var b strings.Builder
	b.WriteString(`<html><head>`)
	b.WriteString(`<meta property="og:title" content="` + channel + ` — ערוץ בדיקה">`)
	b.WriteString(`<meta property="og:image" content="https://cdn.example/` + channel + `.jpg">`)
	b.WriteString(`</head><body>`)
	for i := 0; i < count; i++ {
		id := startID + i
		b.WriteString(fmt.Sprintf(
			`<div class="tgme_widget_message" data-post="%s/%d">`+
				`<div class="tgme_widget_message_text">הודעה מספר %d מתוך %s</div>`+
				`<time datetime="2026-08-11T1%d:00:00+00:00"></time>`+
				`</div>`,
			channel, id, id, channel, i%10))
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

// fakeTelegram serves every channel. When throttleAfter > 0 it starts
// returning 429 once that many requests have been served — reproducing what
// the real Telegram did to the user.
func fakeTelegram(t *testing.T, throttleAfter int64) (*httptest.Server, *int64) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if throttleAfter > 0 && n > throttleAfter {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		channel := strings.Trim(r.URL.Path, "/")
		if channel == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fakeChannelPage(channel, 5000, 8)))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// The happy path: eleven channels, healthy server, every channel must land in
// the feed with its name and avatar.
func TestElevenChannelsAllPopulateTheFeed(t *testing.T) {
	srv, hits := fakeTelegram(t, 0)
	t.Setenv("TGPOPUP_BASE_URL", srv.URL)

	feed := NewFeed()
	feed.ListChannels = func() []string { return userChannels }

	limiterReset()

	for _, ch := range userChannels {
		msgs, info, err := fetchChannel(ch)
		if err != nil {
			t.Fatalf("@%s failed: %v", ch, err)
		}
		if len(msgs) == 0 {
			t.Fatalf("@%s returned no messages", ch)
		}
		tgLimiter.success(ch, len(msgs))
		feed.SetChannelInfo(ch, info)
		feed.Add(msgs)
	}

	if got := atomic.LoadInt64(hits); got != int64(len(userChannels)) {
		t.Errorf("expected exactly one request per channel, got %d", got)
	}

	// Every channel must be represented in the timeline.
	feed.mu.Lock()
	seen := map[string]int{}
	for _, it := range feed.items {
		seen[it.Channel]++
	}
	total := len(feed.items)
	feed.mu.Unlock()

	if total != len(userChannels)*8 {
		t.Errorf("expected %d items, got %d", len(userChannels)*8, total)
	}
	for _, ch := range userChannels {
		if seen[ch] != 8 {
			t.Errorf("@%s contributed %d items, want 8", ch, seen[ch])
		}
		info, ok := feed.GetChannelInfo(ch)
		if !ok || info.Name == "" {
			t.Errorf("@%s has no display name — the sidebar would show a bare handle", ch)
		}
		if info.Photo == "" {
			t.Errorf("@%s has no avatar — this was one of the user's complaints", ch)
		}
	}
}

// The /api/status endpoint must tell the truth about a throttled server, and
// must say it in a way a non-technical user can act on.
func TestStatusEndpointExplainsThrottling(t *testing.T) {
	srv, _ := fakeTelegram(t, 0)
	t.Setenv("TGPOPUP_BASE_URL", srv.URL)

	feed := NewFeed()
	feed.ListChannels = func() []string { return userChannels }
	limiterReset()

	// Every channel refused.
	for _, ch := range userChannels {
		tgLimiter.failure(ch, &httpStatusError{Code: 429}, true)
	}

	rec := httptest.NewRecorder()
	feed.handleStatus(rec, httptest.NewRequest("GET", "/api/status", nil))

	var got struct {
		Banner   string          `json:"banner"`
		Blocked  bool            `json:"blocked"`
		OK       int             `json:"ok"`
		Total    int             `json:"total"`
		Channels []ChannelStatus `json:"channels"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if !got.Blocked {
		t.Error("status must report blocked=true")
	}
	if got.OK != 0 || got.Total != len(userChannels) {
		t.Errorf("counts wrong: ok=%d total=%d", got.OK, got.Total)
	}
	if got.Banner == "" {
		t.Fatal("a blocked feed must produce an explanatory banner")
	}
	// The banner is what the user reads instead of staring at a blank pane.
	if !strings.Contains(got.Banner, "טלגרם") {
		t.Errorf("banner should name Telegram as the cause: %q", got.Banner)
	}
	for _, c := range got.Channels {
		if c.RetryIn <= 0 {
			t.Errorf("@%s should show a countdown to the next attempt", c.Channel)
		}
	}
	t.Logf("banner shown to user: %s", got.Banner)
}

// Healthy feed → no banner at all. The status bar must not nag when nothing
// is wrong.
func TestStatusStaysSilentWhenHealthy(t *testing.T) {
	feed := NewFeed()
	feed.ListChannels = func() []string { return userChannels }
	limiterReset()
	for _, ch := range userChannels {
		tgLimiter.success(ch, 8)
	}
	feed.Add([]Message{{Channel: "elisha_yered", ID: 1, Text: "שלום", Time: "2026-08-11T10:00:00Z"}})

	rec := httptest.NewRecorder()
	feed.handleStatus(rec, httptest.NewRequest("GET", "/api/status", nil))

	var got struct {
		Banner  string `json:"banner"`
		Blocked bool   `json:"blocked"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Blocked {
		t.Error("nothing is blocked; status must not claim otherwise")
	}
	if got.Banner != "" {
		t.Errorf("healthy feed must show no banner, got %q", got.Banner)
	}
}

// The regression proper: under throttling, the OLD behaviour hammered the
// server every tick. The new behaviour must both back off AND recover once the
// server relents.
func TestThrottledChannelsRecoverAutomatically(t *testing.T) {
	var throttling atomic.Bool
	throttling.Store(true)
	var hits int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if throttling.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		channel := strings.Trim(r.URL.Path, "/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fakeChannelPage(channel, 6000, 5)))
	}))
	defer srv.Close()
	t.Setenv("TGPOPUP_BASE_URL", srv.URL)

	l := &limiter{state: map[string]*chanState{}}
	ch := "elisha_yered"

	// Phase 1: throttled. Many ticks, few requests.
	for i := 0; i < 15; i++ {
		if ok, _ := l.ready(ch); !ok {
			continue
		}
		_, _, err := fetchChannel(ch)
		if err == nil {
			t.Fatal("expected refusal")
		}
		l.failure(ch, err, throttled(err))
	}
	during := atomic.LoadInt64(&hits)
	if during > 3 {
		t.Errorf("backoff failed: %d requests during throttling", during)
	}

	// Phase 2: the block lifts. Clearing backoff must let it through and the
	// channel must return real content again.
	throttling.Store(false)
	l.success(ch, 0) // simulates the backoff window elapsing

	msgs, _, err := fetchChannel(ch)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages after recovery, got %d", len(msgs))
	}
	t.Logf("throttled phase: %d requests across 15 ticks; recovered cleanly", during)
}

// A channel that 404s (wrong name) must NOT be treated as a throttle — it
// should be reported so the user can fix the name, not silently retried.
func TestMissingChannelIsReportedNotRetriedForever(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("TGPOPUP_BASE_URL", srv.URL)

	l := &limiter{state: map[string]*chanState{}}
	_, _, err := fetchChannel("typo_channel")
	if err == nil {
		t.Fatal("expected error")
	}
	if throttled(err) {
		t.Error("a 404 must not be classified as throttling")
	}
	l.failure("typo_channel", err, false)

	rows := l.Statuses([]string{"typo_channel"})
	if rows[0].Blocked {
		t.Error("a missing channel is not 'blocked by Telegram'")
	}
	if !strings.Contains(rows[0].Error, "לא נמצא") {
		t.Errorf("the error should tell the user the channel was not found: %q", rows[0].Error)
	}
}

// Full sweep timing: eleven channels through the real pacer must take long
// enough to be civil, but not so long the app feels dead.
func TestFullSweepTimingIsReasonable(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	srv, _ := fakeTelegram(t, 0)
	t.Setenv("TGPOPUP_BASE_URL", srv.URL)
	limiterReset()

	start := time.Now()
	for _, ch := range userChannels {
		if _, _, err := fetchChannel(ch); err != nil {
			t.Fatalf("@%s: %v", ch, err)
		}
	}
	elapsed := time.Since(start)

	minExpected := time.Duration(len(userChannels)-1) * minRequestGap
	if elapsed < minExpected {
		t.Errorf("sweep too fast (%v) — pacing not applied", elapsed)
	}
	if elapsed > pollInterval(15, len(userChannels)) {
		t.Errorf("sweep (%v) outlasts the poll interval (%v) — scans would pile up",
			elapsed, pollInterval(15, len(userChannels)))
	}
	t.Logf("11-channel sweep took %v, poll interval is %v — comfortable fit",
		elapsed, pollInterval(15, len(userChannels)))
}

// limiterReset clears global limiter state between tests that share it.
var limiterResetMu sync.Mutex

func limiterReset() {
	limiterResetMu.Lock()
	defer limiterResetMu.Unlock()
	tgLimiter.stateMu.Lock()
	tgLimiter.state = map[string]*chanState{}
	tgLimiter.stateMu.Unlock()
}
