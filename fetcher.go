package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Polite fetching.
//
// Telegram serves t.me/s/<channel> to anyone, but it watches how fast you ask.
// Eleven channels polled every 15 seconds is ~2,600 requests an hour from one
// IP — that reads as a scraper, and Telegram starts refusing. The symptom is
// brutal and silent: every channel comes back empty and the page looks broken.
//
// This layer makes the app behave like a person with a browser rather than a
// crawler:
//
//   - a global minimum gap between ANY two requests to Telegram
//   - per-channel exponential backoff after a refusal, so a blocked channel
//     stops hammering instead of retrying every tick
//   - a status board recording, per channel, what actually happened — which
//     the page shows the user directly
//
// Nothing here changes what is fetched. It only changes the pace.
// ---------------------------------------------------------------------------

// minRequestGap is the floor between two consecutive requests to Telegram,
// across all channels and all goroutines. 1.5s ≈ 40 requests/minute worst
// case, which sits comfortably inside what a normal browsing session looks
// like even with many channels configured.
const minRequestGap = 1500 * time.Millisecond

// Backoff ladder applied per channel after a refusal. Capped so a channel that
// recovers is retried within a few minutes rather than being parked forever.
var backoffLadder = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
	20 * time.Minute,
}

// ChannelStatus is one row of the diagnostics board shown in the page.
type ChannelStatus struct {
	Channel   string `json:"channel"`
	OK        bool   `json:"ok"`
	Messages  int    `json:"messages"`   // messages seen in the last good fetch
	Error     string `json:"error"`      // human-readable, in Hebrew
	Blocked   bool   `json:"blocked"`    // Telegram is refusing this channel
	LastOK    int64  `json:"last_ok"`    // unix seconds, 0 = never
	LastTry   int64  `json:"last_try"`   // unix seconds
	RetryIn   int    `json:"retry_in"`   // seconds until the next attempt
	Failures  int    `json:"failures"`   // consecutive failures
}

type limiter struct {
	mu   sync.Mutex
	last time.Time

	// per-channel backoff bookkeeping
	stateMu sync.Mutex
	state   map[string]*chanState
}

type chanState struct {
	failures  int
	nextTry   time.Time
	lastOK    time.Time
	lastTry   time.Time
	lastErr   string
	blocked   bool
	lastCount int
}

var tgLimiter = &limiter{state: map[string]*chanState{}}

// errEmptyPage is a 200 response that carried no posts. For a channel that was
// working a moment ago this is a soft block, not a missing channel.
var errEmptyPage = fmt.Errorf("הדף נטען אך לא הוחזרו הודעות (ככל הנראה הגבלת קצב זמנית)")

// wait blocks until enough time has passed since the previous request. Every
// outbound call to Telegram goes through here, so the global pace holds no
// matter how many goroutines are scanning.
func (l *limiter) wait() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if gap := time.Since(l.last); gap < minRequestGap {
		time.Sleep(minRequestGap - gap)
	}
	l.last = time.Now()
}

func (l *limiter) get(channel string) *chanState {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	st, ok := l.state[channel]
	if !ok {
		st = &chanState{}
		l.state[channel] = st
	}
	return st
}

// ready reports whether a channel may be fetched now, and if not, how long is
// left on its backoff.
func (l *limiter) ready(channel string) (bool, time.Duration) {
	st := l.get(channel)
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	if st.nextTry.IsZero() || time.Now().After(st.nextTry) {
		return true, 0
	}
	return false, time.Until(st.nextTry)
}

// success clears a channel's backoff and records what it returned.
func (l *limiter) success(channel string, count int) {
	st := l.get(channel)
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	st.failures = 0
	st.nextTry = time.Time{}
	st.lastOK = time.Now()
	st.lastTry = st.lastOK
	st.lastErr = ""
	st.blocked = false
	st.lastCount = count
}

// failure advances the channel's backoff. blocked marks the refusals that mean
// "Telegram is throttling us" as opposed to an ordinary network hiccup.
func (l *limiter) failure(channel string, err error, blocked bool) time.Duration {
	st := l.get(channel)
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	st.lastTry = time.Now()
	if err != nil {
		st.lastErr = err.Error()
	}
	st.blocked = blocked
	if st.failures < len(backoffLadder) {
		st.failures++
	}
	wait := backoffLadder[st.failures-1]
	st.nextTry = time.Now().Add(wait)
	return wait
}

// Statuses returns the diagnostics board for every known channel.
func (l *limiter) Statuses(channels []string) []ChannelStatus {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	now := time.Now()
	rows := make([]ChannelStatus, 0, len(channels))
	for _, ch := range channels {
		st, ok := l.state[ch]
		if !ok {
			rows = append(rows, ChannelStatus{Channel: ch, Error: "טרם נבדק"})
			continue
		}
		row := ChannelStatus{
			Channel:  ch,
			OK:       st.failures == 0 && !st.lastOK.IsZero(),
			Messages: st.lastCount,
			Error:    st.lastErr,
			Blocked:  st.blocked,
			Failures: st.failures,
		}
		if !st.lastOK.IsZero() {
			row.LastOK = st.lastOK.Unix()
		}
		if !st.lastTry.IsZero() {
			row.LastTry = st.lastTry.Unix()
		}
		if !st.nextTry.IsZero() && st.nextTry.After(now) {
			row.RetryIn = int(time.Until(st.nextTry).Seconds())
		}
		rows = append(rows, row)
	}
	return rows
}

// AnyBlocked reports whether Telegram is currently throttling any of the
// GIVEN channels — the page turns this into a plain-language banner. Scoped
// to the live channel list so a channel that was removed while blocked can
// never leave the banner stuck on forever.
func (l *limiter) AnyBlocked(channels []string) bool {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	for _, ch := range channels {
		if st, ok := l.state[ch]; ok && st.blocked {
			return true
		}
	}
	return false
}

// isThrottle recognises the refusals that mean "slow down" rather than
// "this channel does not exist".
func isThrottle(status int) bool {
	switch status {
	case http.StatusTooManyRequests, // 429 — the explicit one
		http.StatusForbidden,        // 403 — Telegram's quiet block
		http.StatusServiceUnavailable,
		http.StatusBadGateway,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// httpStatusError carries the status code so callers can tell a throttle from
// a genuinely missing channel.
type httpStatusError struct {
	Code int
}

func (e *httpStatusError) Error() string {
	switch {
	case e.Code == http.StatusTooManyRequests:
		return "טלגרם מגבילה את קצב הבקשות (429) — ממתין ומנסה שוב"
	case e.Code == http.StatusForbidden:
		return "טלגרם חסמה זמנית את הבקשות (403) — ממתין ומנסה שוב"
	case e.Code == http.StatusNotFound:
		return "הערוץ לא נמצא (404) — ייתכן שהשם שגוי או שהערוץ נסגר"
	case e.Code >= 500:
		return "טלגרם מחזירה שגיאת שרת (" + strconv.Itoa(e.Code) + ") — ממתין ומנסה שוב"
	}
	return "הערוץ החזיר סטטוס " + strconv.Itoa(e.Code)
}

// throttled reports whether this error is a slow-down signal.
func throttled(err error) bool {
	if se, ok := err.(*httpStatusError); ok {
		return isThrottle(se.Code)
	}
	// Network-level failures are treated as transient too — backing off on a
	// dead connection costs nothing and avoids a tight retry loop.
	if err != nil {
		msg := strings.ToLower(err.Error())
		for _, s := range []string{"timeout", "connection", "refused", "reset", "no such host", "eof"} {
			if strings.Contains(msg, s) {
				return true
			}
		}
	}
	return false
}

// pollInterval scales the polling period to the number of channels so the
// total request rate stays civil no matter how many the user adds. The user's
// configured value is treated as a floor for few channels and is raised
// automatically once the list grows.
//
//	 1 channel  → configured value (min 15s)
//	 5 channels → ~30s
//	11 channels → ~66s
//
// A news channel checked every minute still feels instant; being blocked does
// not.
func pollInterval(configured, channels int) time.Duration {
	if channels < 1 {
		channels = 1
	}
	base := configured
	if base < 15 {
		base = 15
	}
	// Aim for roughly one request every 6 seconds overall.
	needed := channels * 6
	if needed > base {
		base = needed
	}
	if base > 300 {
		base = 300
	}
	return time.Duration(base) * time.Second
}

func describeRate(channels int, d time.Duration) string {
	if d <= 0 {
		return ""
	}
	perHour := float64(channels) * 3600 / d.Seconds()
	return fmt.Sprintf("%d ערוצים · בדיקה כל %.0f שניות · כ-%.0f בקשות בשעה",
		channels, d.Seconds(), perHour)
}
