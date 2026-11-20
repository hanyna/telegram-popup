package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Long videos ("Media is too big").
//
// Telegram's public pages refuse to hand out any video above ~20 MB: no mp4
// anywhere, only "VIEW IN TELEGRAM". The file exists only behind a Telegram
// account, and the user has no way to get one (the login SMS never arrives).
//
// The way out is a public bridge: TelegramRSS (https://tg.i-c-a.su), a free
// open-source service that is itself logged in to Telegram and exposes the
// media of public channels over plain HTTPS:
//
//	GET <base>/media/<channel>/<post id>    -> the mp4, Range supported
//
// Its rules, which this file is built around:
//   - 15 requests a minute       -> bridge requests are spaced out;
//   - "no media hosting", "do not fetch the same media from many clients,
//     cache on your backend"     -> every video is downloaded ONCE to our own
//     disk and all viewers are served from that copy;
//   - "temporary ban on any error" -> after an error we stay away for a
//     while; a FLOOD_WAIT_n answer blocks that channel for n seconds.
//
// Playback does not wait for the whole download: the file is served while it
// grows (growReader), so a normal streamable mp4 starts within a second or two.

const defaultBridgeBase = "https://tg.i-c-a.su"

var (
	reBridgeFlood   = regexp.MustCompile(`FLOOD_WAIT_(\d+)`)
	reBridgeChannel = regexp.MustCompile(`^[A-Za-z0-9_]{3,64}$`)
)

// bridgeBusyError: the bridge asked us to stay away until a given time.
type bridgeBusyError struct{ until time.Time }

func (e *bridgeBusyError) Error() string {
	return "שירות הסרטונים הארוכים עמוס כרגע — נסה שוב בעוד " + humanWait(time.Until(e.until))
}

func humanWait(d time.Duration) string {
	switch {
	case d <= 90*time.Second:
		return "כדקה"
	case d < 100*time.Minute:
		return "כ-" + strconv.Itoa(int(d.Minutes()+0.5)) + " דקות"
	default:
		return "כ-" + strconv.Itoa(int(d.Hours()+0.5)) + " שעות"
	}
}

var errBridgeNotVideo = errors.New("הסרטון לא נמצא בשירות הסרטונים הארוכים")

type bridgeDL struct {
	path      string
	started   chan struct{} // closed once the size is known, or the download failed
	startOnce sync.Once

	mu       sync.Mutex
	size     int64 // total bytes (0 until the bridge answered)
	written  int64
	done     bool
	err      error
	failedAt time.Time
}

func (d *bridgeDL) snapshot() (size, written int64, done bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.size, d.written, d.done, d.err
}

type bridgeStore struct {
	base     string
	dir      string
	maxFile  int64
	maxTotal int64
	spacing  time.Duration
	client   *http.Client
	logf     func(format string, args ...any)

	mu         sync.Mutex
	dls        map[string]*bridgeDL
	nextStart  time.Time            // earliest moment the next bridge request may leave
	chanBlock  map[string]time.Time // per channel: FLOOD_WAIT
	globalWait time.Time            // after any other error
	sem        chan struct{}        // at most two downloads at once
}

// newBridgeStore: base "" = the public default. Returns nil when disabled
// ("off") or when base is not a usable URL.
func newBridgeStore(base, dir string, maxTotal int64, logf func(string, ...any)) *bridgeStore {
	base = strings.TrimSpace(base)
	switch strings.ToLower(base) {
	case "off", "0", "false", "no":
		return nil
	case "":
		base = defaultBridgeBase
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil
	}
	local := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return nil
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if maxTotal <= 0 {
		maxTotal = 700 << 20
	}
	host := u.Host
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 60 * time.Second
	b := &bridgeStore{
		base:      strings.TrimRight(base, "/"),
		dir:       dir,
		maxFile:   400 << 20,
		maxTotal:  maxTotal,
		spacing:   5 * time.Second,
		logf:      logf,
		dls:       make(map[string]*bridgeDL),
		chanBlock: make(map[string]time.Time),
		sem:       make(chan struct{}, 2),
		client: &http.Client{
			Transport: tr,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// The bridge is the only host we agreed to talk to.
				if req.URL.Host != host || len(via) > 3 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
	_ = os.MkdirAll(dir, 0o755)
	b.sweepPartial()
	return b
}

func (b *bridgeStore) key(channel string, id int) string {
	return strings.ToLower(channel) + "_" + strconv.Itoa(id)
}

func (b *bridgeStore) filePath(key string) string { return filepath.Join(b.dir, key+".mp4") }

// sweepPartial removes downloads a previous run did not finish.
func (b *bridgeStore) sweepPartial() {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".mp4") {
			continue
		}
		if _, err := os.Stat(filepath.Join(b.dir, name+".ok")); err != nil {
			_ = os.Remove(filepath.Join(b.dir, name))
		}
	}
}

// cached returns a finished download from disk, if there is one.
func (b *bridgeStore) cached(key string) *bridgeDL {
	path := b.filePath(key)
	if _, err := os.Stat(path + ".ok"); err != nil {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return nil
	}
	now := time.Now()
	_ = os.Chtimes(path+".ok", now, now) // LRU clock; the mp4's own mtime stays put for Range/If-Range
	d := &bridgeDL{path: path, started: make(chan struct{}), size: info.Size(), written: info.Size(), done: true}
	d.startOnce.Do(func() { close(d.started) })
	return d
}

// active: is this video already downloaded or on its way? (no network)
func (b *bridgeStore) active(channel string, id int) bool {
	if b == nil {
		return false
	}
	key := b.key(channel, id)
	b.mu.Lock()
	d := b.dls[key]
	b.mu.Unlock()
	if d != nil {
		_, _, _, err := d.snapshot()
		return err == nil
	}
	_, err := os.Stat(b.filePath(key) + ".ok")
	return err == nil
}

// get returns the download for a post, starting it when needed.
func (b *bridgeStore) get(channel string, id int) (*bridgeDL, error) {
	if b == nil {
		return nil, errBridgeNotVideo
	}
	if !reBridgeChannel.MatchString(channel) || id < 1 {
		return nil, errBridgeNotVideo
	}
	key := b.key(channel, id)

	b.mu.Lock()
	defer b.mu.Unlock()
	if d := b.dls[key]; d != nil {
		_, _, done, err := d.snapshot()
		switch {
		case err == nil && !done:
			return d, nil // in flight
		case err == nil && done:
			if _, sErr := os.Stat(d.path + ".ok"); sErr == nil {
				return d, nil
			}
			delete(b.dls, key) // evicted from disk since
		default:
			d.mu.Lock()
			fresh := time.Since(d.failedAt) < 30*time.Second
			d.mu.Unlock()
			if fresh {
				return nil, err
			}
			delete(b.dls, key)
		}
	}
	if d := b.cached(key); d != nil {
		b.dls[key] = d
		return d, nil
	}
	now := time.Now()
	if until := b.chanBlock[strings.ToLower(channel)]; until.After(now) {
		return nil, &bridgeBusyError{until}
	}
	if b.globalWait.After(now) {
		return nil, &bridgeBusyError{b.globalWait}
	}
	// Reserve a departure slot so requests leave at least `spacing` apart.
	depart := now
	if b.nextStart.After(depart) {
		depart = b.nextStart
	}
	b.nextStart = depart.Add(b.spacing)

	d := &bridgeDL{path: b.filePath(key), started: make(chan struct{})}
	b.dls[key] = d
	go b.run(d, channel, id, depart)
	return d, nil
}

func (b *bridgeStore) fail(d *bridgeDL, err error) {
	d.mu.Lock()
	d.err = err
	d.done = true
	d.failedAt = time.Now()
	d.mu.Unlock()
	d.startOnce.Do(func() { close(d.started) })
	_ = os.Remove(d.path)
}

func (b *bridgeStore) run(d *bridgeDL, channel string, id int, depart time.Time) {
	b.sem <- struct{}{}
	defer func() { <-b.sem }()
	if wait := time.Until(depart); wait > 0 {
		time.Sleep(wait)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	target := b.base + "/media/" + url.PathEscape(channel) + "/" + strconv.Itoa(id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	resp, err := b.client.Do(req)
	if err != nil {
		b.backoff(2 * time.Minute)
		b.logf("סרטון ארוך @%s/%d: אין תשובה משירות הגשר: %v", channel, id, err)
		b.fail(d, &bridgeBusyError{time.Now().Add(2 * time.Minute)})
		return
	}
	defer resp.Body.Close()

	ctype := strings.ToLower(resp.Header.Get("Content-Type"))
	isMedia := strings.HasPrefix(ctype, "video/") || strings.HasPrefix(ctype, "application/octet-stream")
	if resp.StatusCode != http.StatusOK || !isMedia {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if m := reBridgeFlood.FindSubmatch(body); m != nil {
			secs, _ := strconv.Atoi(string(m[1]))
			if secs < 30 {
				secs = 30
			}
			if secs > 12*3600 {
				secs = 12 * 3600
			}
			until := time.Now().Add(time.Duration(secs) * time.Second)
			b.mu.Lock()
			b.chanBlock[strings.ToLower(channel)] = until
			b.mu.Unlock()
			b.logf("סרטון ארוך @%s/%d: שירות הגשר ביקש להמתין %s לערוץ הזה", channel, id, humanWait(time.Until(until)))
			b.fail(d, &bridgeBusyError{until})
			return
		}
		// "Temporary ban on any error" — do not come back for more right away.
		b.backoff(90 * time.Second)
		b.logf("סרטון ארוך @%s/%d: שירות הגשר ענה %d (%s)", channel, id, resp.StatusCode, firstLine(body))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			b.fail(d, &bridgeBusyError{time.Now().Add(90 * time.Second)})
		} else {
			b.fail(d, errBridgeNotVideo)
		}
		return
	}
	if resp.ContentLength <= 0 || resp.ContentLength > b.maxFile {
		b.logf("סרטון ארוך @%s/%d: גודל לא מתאים (%d)", channel, id, resp.ContentLength)
		b.fail(d, errors.New("הסרטון גדול מדי להצגה כאן"))
		return
	}

	out, err := os.Create(d.path)
	if err != nil {
		b.fail(d, fmt.Errorf("אין מקום לשמור את הסרטון: %w", err))
		return
	}
	d.mu.Lock()
	d.size = resp.ContentLength
	d.mu.Unlock()
	d.startOnce.Do(func() { close(d.started) })

	t0 := time.Now()
	buf := make([]byte, 128<<10)
	var total int64
	for {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				out.Close()
				b.fail(d, fmt.Errorf("שמירת הסרטון נכשלה: %w", wErr))
				return
			}
			total += int64(n)
			d.mu.Lock()
			d.written = total
			d.mu.Unlock()
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			out.Close()
			b.logf("סרטון ארוך @%s/%d: ההורדה נקטעה אחרי %d בתים: %v", channel, id, total, rErr)
			b.fail(d, &bridgeBusyError{time.Now().Add(time.Minute)})
			return
		}
	}
	out.Close()
	if total != resp.ContentLength {
		b.fail(d, &bridgeBusyError{time.Now().Add(time.Minute)})
		return
	}
	_ = os.WriteFile(d.path+".ok", []byte(strconv.FormatInt(total, 10)), 0o644)
	d.mu.Lock()
	d.done = true
	d.mu.Unlock()
	b.logf("סרטון ארוך @%s/%d מוכן: %.1f MB ב-%.0f שניות", channel, id, float64(total)/(1<<20), time.Since(t0).Seconds())
	b.evict(filepath.Base(d.path))
}

func firstLine(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func (b *bridgeStore) backoff(d time.Duration) {
	b.mu.Lock()
	if until := time.Now().Add(d); until.After(b.globalWait) {
		b.globalWait = until
	}
	b.mu.Unlock()
}

// evict keeps the cache under maxTotal, oldest-watched first.
func (b *bridgeStore) evict(keep string) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return
	}
	type item struct {
		name string
		size int64
		used time.Time
	}
	var items []item
	var total int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".mp4") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		ok, err := os.Stat(filepath.Join(b.dir, name+".ok"))
		if err != nil {
			continue // still downloading
		}
		items = append(items, item{name, info.Size(), ok.ModTime()})
		total += info.Size()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].used.Before(items[j].used) })
	for _, it := range items {
		if total <= b.maxTotal {
			break
		}
		if it.name == keep {
			continue
		}
		// Marker first: a file without it is never served from cache.
		_ = os.Remove(filepath.Join(b.dir, it.name+".ok"))
		if os.Remove(filepath.Join(b.dir, it.name)) == nil {
			total -= it.size
		}
		b.mu.Lock()
		delete(b.dls, strings.TrimSuffix(it.name, ".mp4"))
		b.mu.Unlock()
	}
}

// waitStarted blocks until the bridge answered (size known) or failed.
func (d *bridgeDL) waitStarted(ctx context.Context, max time.Duration) error {
	t := time.NewTimer(max)
	defer t.Stop()
	select {
	case <-d.started:
		_, _, _, err := d.snapshot()
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil // still queued; the media request will keep waiting
	}
}

// serve streams the video to the browser, also while it is still arriving.
// Returns false when nothing was written (the caller answers instead).
func (b *bridgeStore) serve(w http.ResponseWriter, r *http.Request, channel string, id int) (bool, error) {
	d, err := b.get(channel, id)
	if err != nil {
		return false, err
	}
	select {
	case <-d.started:
	case <-r.Context().Done():
		return false, r.Context().Err()
	case <-time.After(3 * time.Minute):
		return false, &bridgeBusyError{time.Now().Add(time.Minute)}
	}
	_, _, done, dErr := d.snapshot()
	if dErr != nil {
		return false, dErr
	}
	f, err := os.Open(d.path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if done {
		info, sErr := f.Stat()
		if sErr != nil {
			return false, sErr
		}
		http.ServeContent(w, r, "video.mp4", info.ModTime(), f)
		return true, nil
	}
	http.ServeContent(w, r, "video.mp4", time.Time{}, &growReader{d: d, f: f, ctx: r.Context()})
	return true, nil
}

// growReader reads a file that is still being written: Read blocks until the
// wanted bytes have arrived. Seek works against the final size, which is
// known from the first response header.
type growReader struct {
	d   *bridgeDL
	f   *os.File
	off int64
	ctx context.Context
}

func (g *growReader) Seek(offset int64, whence int) (int64, error) {
	size, _, _, _ := g.d.snapshot()
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = g.off + offset
	case io.SeekEnd:
		abs = size + offset
	default:
		return 0, errors.New("bad whence")
	}
	if abs < 0 {
		return 0, errors.New("negative position")
	}
	g.off = abs
	return abs, nil
}

func (g *growReader) Read(p []byte) (int, error) {
	for {
		size, written, done, err := g.d.snapshot()
		if g.off >= size {
			return 0, io.EOF
		}
		if g.off < written {
			max := written - g.off
			if int64(len(p)) > max {
				p = p[:max]
			}
			n, rErr := g.f.ReadAt(p, g.off)
			g.off += int64(n)
			if n > 0 {
				return n, nil
			}
			if rErr != nil && rErr != io.EOF {
				return 0, rErr
			}
		}
		if err != nil {
			return 0, err
		}
		if done && g.off >= written {
			return 0, io.ErrUnexpectedEOF
		}
		select {
		case <-g.ctx.Done():
			return 0, g.ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
}

// ----- HTTP ------------------------------------------------------------------

// followed: the bridge only ever fetches channels this app follows, so the
// page cannot be used to pull arbitrary media through our server.
func (f *Feed) followed(channel string) bool {
	if f.ListChannels == nil {
		return true
	}
	for _, c := range f.ListChannels() {
		if strings.EqualFold(c, channel) {
			return true
		}
	}
	return false
}

// handleBigVideo: the page asks to prepare a long video. Starts (or joins)
// the download and answers once the bridge has accepted — the player then
// streams it from /api/media.
func (f *Feed) handleBigVideo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if f.bridge == nil || err != nil || channel == "" || id < 1 || !f.followed(channel) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "הסרטון הזה זמין לצפייה רק בתוך טלגרם"})
		return
	}
	d, err := f.bridge.get(channel, id)
	if err == nil {
		err = d.waitStarted(r.Context(), 40*time.Second)
	}
	if err != nil {
		out := map[string]any{"error": err.Error()}
		var busy *bridgeBusyError
		if errors.As(err, &busy) {
			out["retry"] = int(time.Until(busy.until).Seconds())
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	size, _, _, _ := d.snapshot()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"size": size,
		"src":  "/api/media?channel=" + url.QueryEscape(channel) + "&id=" + strconv.Itoa(id),
	})
}
