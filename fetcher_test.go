package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The bug this file guards against: eleven channels polled every 15 seconds
// meant ~2,600 requests an hour to t.me, Telegram started refusing, and every
// channel came back empty. These tests pin down the pacing that prevents it.

func TestPollIntervalScalesWithChannelCount(t *testing.T) {
	cases := []struct {
		configured, channels int
		wantAtLeast          time.Duration
	}{
		{15, 1, 15 * time.Second},
		{15, 5, 30 * time.Second},
		{15, 11, 66 * time.Second},
		{15, 20, 120 * time.Second},
	}
	for _, c := range cases {
		got := pollInterval(c.configured, c.channels)
		if got < c.wantAtLeast {
			t.Errorf("pollInterval(%d, %d) = %v, want >= %v",
				c.configured, c.channels, got, c.wantAtLeast)
		}
	}
}

// The headline regression: the exact configuration the user was running must
// no longer produce a scraper-grade request rate.
func TestUsersElevenChannelsStayUnderRateLimit(t *testing.T) {
	const channels = 11
	d := pollInterval(15, channels)
	perHour := float64(channels) * 3600 / d.Seconds()

	if perHour > 700 {
		t.Errorf("11 channels would issue %.0f requests/hour — that is what got us blocked", perHour)
	}
	t.Logf("11 channels → poll every %v → %.0f requests/hour (was 2640)", d, perHour)
}

func TestPollIntervalIsCapped(t *testing.T) {
	if got := pollInterval(15, 500); got > 300*time.Second {
		t.Errorf("interval must stay usable, got %v", got)
	}
}

func TestPollIntervalRespectsLargerConfiguredValue(t *testing.T) {
	if got := pollInterval(120, 2); got != 120*time.Second {
		t.Errorf("a deliberately slow config must be honoured, got %v", got)
	}
}

// The global pacer must actually space requests out, including when several
// goroutines call it at once.
func TestLimiterEnforcesGapUnderConcurrency(t *testing.T) {
	l := &limiter{state: map[string]*chanState{}}

	const n = 4
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.wait() }()
	}
	wg.Wait()

	// n calls means at least (n-1) gaps between them.
	want := time.Duration(n-1) * minRequestGap
	if elapsed := time.Since(start); elapsed < want {
		t.Errorf("4 concurrent requests took %v, want >= %v — pacing is not holding", elapsed, want)
	}
}

func TestBackoffGrowsThenCaps(t *testing.T) {
	l := &limiter{state: map[string]*chanState{}}
	err := errors.New("429")

	var prev time.Duration
	for i := 0; i < len(backoffLadder)+3; i++ {
		got := l.failure("ch", err, true)
		if i < len(backoffLadder) && got <= prev && i > 0 {
			t.Errorf("backoff must grow: step %d gave %v after %v", i, got, prev)
		}
		prev = got
	}
	if prev != backoffLadder[len(backoffLadder)-1] {
		t.Errorf("backoff must cap at %v, got %v", backoffLadder[len(backoffLadder)-1], prev)
	}
}

func TestBackoffBlocksThenClears(t *testing.T) {
	l := &limiter{state: map[string]*chanState{}}

	if ok, _ := l.ready("ch"); !ok {
		t.Fatal("a fresh channel must be fetchable immediately")
	}
	l.failure("ch", errors.New("429"), true)
	if ok, left := l.ready("ch"); ok {
		t.Error("a just-failed channel must be on backoff")
	} else if left <= 0 {
		t.Error("backoff must report remaining time")
	}
	l.success("ch", 20)
	if ok, _ := l.ready("ch"); !ok {
		t.Error("success must clear the backoff immediately")
	}
}

func TestThrottleDetection(t *testing.T) {
	throttles := []int{429, 403, 502, 503, 504}
	for _, code := range throttles {
		if !throttled(&httpStatusError{Code: code}) {
			t.Errorf("status %d must count as a throttle", code)
		}
	}
	if throttled(&httpStatusError{Code: 404}) {
		t.Error("404 is a missing channel, not a throttle — it must not trigger backoff escalation")
	}
	for _, msg := range []string{"dial tcp: connection refused", "i/o timeout", "unexpected EOF"} {
		if !throttled(errors.New(msg)) {
			t.Errorf("transient network error %q should back off", msg)
		}
	}
}

func TestStatusErrorsAreInHebrew(t *testing.T) {
	for _, code := range []int{429, 403, 404, 500} {
		msg := (&httpStatusError{Code: code}).Error()
		hasHebrew := false
		for _, r := range msg {
			if r >= 0x0590 && r <= 0x05FF {
				hasHebrew = true
				break
			}
		}
		if !hasHebrew {
			t.Errorf("status %d message must be in Hebrew, got %q", code, msg)
		}
	}
}

func TestStatusBoardReportsPerChannel(t *testing.T) {
	l := &limiter{state: map[string]*chanState{}}
	l.success("good", 42)
	l.failure("bad", &httpStatusError{Code: 429}, true)

	rows := l.Statuses([]string{"good", "bad", "untouched"})
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	byName := map[string]ChannelStatus{}
	for _, r := range rows {
		byName[r.Channel] = r
	}
	if !byName["good"].OK || byName["good"].Messages != 42 {
		t.Errorf("healthy channel misreported: %+v", byName["good"])
	}
	if byName["bad"].OK || !byName["bad"].Blocked || byName["bad"].RetryIn <= 0 {
		t.Errorf("blocked channel misreported: %+v", byName["bad"])
	}
	if byName["untouched"].OK {
		t.Error("a never-fetched channel must not report OK")
	}
	if !l.AnyBlocked() {
		t.Error("AnyBlocked must be true while a channel is blocked")
	}
}

// A 200 response with no posts is Telegram's quiet throttle, and previously
// looked identical to "this channel is fine but idle".
func TestEmptyPageIsTreatedAsThrottle(t *testing.T) {
	l := &limiter{state: map[string]*chanState{}}
	l.failure("ch", errEmptyPage, true)
	rows := l.Statuses([]string{"ch"})
	if !rows[0].Blocked {
		t.Error("an empty 200 page must be treated as a soft block")
	}
	if rows[0].RetryIn <= 0 {
		t.Error("an empty page must schedule a retry")
	}
}

// End-to-end: a server that refuses fast (like a throttling Telegram) must not
// be hammered. This is the behaviour that was previously absent.
func TestScanDoesNotHammerARefusingServer(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	t.Setenv("TGPOPUP_BASE_URL", srv.URL)

	l := &limiter{state: map[string]*chanState{}}
	channel := "spam"

	// Simulate twenty polling ticks against a refusing server.
	for i := 0; i < 20; i++ {
		if ok, _ := l.ready(channel); !ok {
			continue // backoff correctly suppressed this tick
		}
		_, _, err := fetchChannel(channel)
		if err == nil {
			t.Fatal("expected the refusal to surface as an error")
		}
		l.failure(channel, err, throttled(err))
	}

	got := atomic.LoadInt64(&hits)
	if got > 3 {
		t.Errorf("hit a refusing server %d times across 20 ticks — backoff is not suppressing retries", got)
	}
	t.Logf("20 polling ticks against a 429 server produced only %d requests", got)
}

// A healthy server must still be read correctly through the new header set —
// in particular, the gzip handling must not have been broken by adding
// browser-like headers.
func TestFetchStillDecodesGzippedPages(t *testing.T) {
	const marker = "בדיקת-דחיסה"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ae := r.Header.Get("Accept-Encoding"); !strings.Contains(ae, "gzip") {
			t.Errorf("Go should be advertising gzip itself, got %q", ae)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>" + marker + "</body></html>"))
	}))
	defer srv.Close()

	page, err := fetchRaw(srv.URL)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if !strings.Contains(page, marker) {
		t.Errorf("page content mangled — gzip handling is broken. got: %q", page)
	}
}

func TestFetchSurfacesStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := fetchRaw(srv.URL)
	if err == nil {
		t.Fatal("expected an error")
	}
	se, ok := err.(*httpStatusError)
	if !ok {
		t.Fatalf("error must carry the status code, got %T", err)
	}
	if se.Code != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", se.Code)
	}
}

func TestBackfillWorkerIsSerial(t *testing.T) {
	// The queue must be buffered enough that enqueueing never blocks a scan.
	if cap(backfillQueue) < 16 {
		t.Errorf("backfill queue too small (%d) — a scan could block on it", cap(backfillQueue))
	}
}

func TestDescribeRateIsHonest(t *testing.T) {
	s := describeRate(11, pollInterval(15, 11))
	if !strings.Contains(s, "11") {
		t.Errorf("rate description should name the channel count: %q", s)
	}
	t.Logf("startup line: %s", s)
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
