package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBridge mimics tg.i-c-a.su/media/<channel>/<id>.
type fakeBridge struct {
	srv   *httptest.Server
	hits  atomic.Int32
	data  []byte
	slow  time.Duration // pause between chunks
	flood map[string]int
}

func newFakeBridge(t *testing.T, size int) *fakeBridge {
	fb := &fakeBridge{data: make([]byte, size), flood: map[string]int{}}
	for i := range fb.data {
		fb.data[i] = byte(i*7 + i/251)
	}
	fb.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fb.hits.Add(1)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "media" {
			http.Error(w, `{"success":false,"errors":["bad"]}`, 400)
			return
		}
		if n := fb.flood[parts[1]]; n > 0 {
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(429)
			io.WriteString(w, `{"success": false, "errors": ["FLOOD_WAIT_`+strconv.Itoa(n)+`"]}`)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.Itoa(len(fb.data)))
		fl, _ := w.(http.Flusher)
		for off := 0; off < len(fb.data); off += 4096 {
			end := off + 4096
			if end > len(fb.data) {
				end = len(fb.data)
			}
			w.Write(fb.data[off:end])
			if fl != nil {
				fl.Flush()
			}
			if fb.slow > 0 {
				time.Sleep(fb.slow)
			}
		}
	}))
	t.Cleanup(fb.srv.Close)
	return fb
}

func newTestBridge(t *testing.T, fb *fakeBridge) *bridgeStore {
	b := newBridgeStore(fb.srv.URL, t.TempDir(), 0, nil)
	if b == nil {
		t.Fatal("bridge store not created")
	}
	b.spacing = 0
	return b
}

func TestBridgeDisabledAndBadBase(t *testing.T) {
	if newBridgeStore("off", t.TempDir(), 0, nil) != nil {
		t.Fatal("off must disable the bridge")
	}
	if newBridgeStore("http://example.com", t.TempDir(), 0, nil) != nil {
		t.Fatal("plain http to a remote host must be refused")
	}
	if b := newBridgeStore("", t.TempDir(), 0, nil); b == nil || b.base != defaultBridgeBase {
		t.Fatal("empty value must mean the public default")
	}
}

// The player must not wait for the whole file: a Range request in the middle
// of a still-running download gets exactly the right bytes.
func TestBridgeServesWhileDownloading(t *testing.T) {
	fb := newFakeBridge(t, 200_000)
	fb.slow = 2 * time.Millisecond
	b := newTestBridge(t, fb)

	req := httptest.NewRequest("GET", "/api/media?channel=alpha&id=7", nil)
	req.Header.Set("Range", "bytes=150000-150999")
	rec := httptest.NewRecorder()
	ok, err := b.serve(rec, req, "alpha", 7)
	if !ok || err != nil {
		t.Fatalf("serve: ok=%v err=%v", ok, err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("want 206, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 150000-150999/200000" {
		t.Fatalf("content-range %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), fb.data[150000:151000]) {
		t.Fatal("range bytes differ from the source")
	}

	// Full read, then a second viewer: served from our copy, the bridge is
	// not asked again.
	rec = httptest.NewRecorder()
	if ok, err := b.serve(rec, httptest.NewRequest("GET", "/x", nil), "alpha", 7); !ok || err != nil {
		t.Fatalf("full serve: %v %v", ok, err)
	}
	if !bytes.Equal(rec.Body.Bytes(), fb.data) {
		t.Fatal("full body differs")
	}
	rec = httptest.NewRecorder()
	b.serve(rec, httptest.NewRequest("GET", "/x", nil), "ALPHA", 7)
	if !bytes.Equal(rec.Body.Bytes(), fb.data) {
		t.Fatal("cached body differs")
	}
	if n := fb.hits.Load(); n != 1 {
		t.Fatalf("bridge was asked %d times, want 1", n)
	}

	// A fresh store on the same folder (restart) still has it.
	b2 := newBridgeStore(fb.srv.URL, b.dir, 0, nil)
	if !b2.active("alpha", 7) {
		t.Fatal("finished download should survive a restart")
	}
}

// FLOOD_WAIT blocks that channel only, and we do not knock again.
func TestBridgeFloodWait(t *testing.T) {
	fb := newFakeBridge(t, 5000)
	fb.flood["alpha"] = 600
	b := newTestBridge(t, fb)

	d, err := b.get("alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	<-d.started
	_, _, _, dErr := d.snapshot()
	var busy *bridgeBusyError
	if !errors.As(dErr, &busy) {
		t.Fatalf("want busy error, got %v", dErr)
	}
	if left := time.Until(busy.until); left < 9*time.Minute || left > 11*time.Minute {
		t.Fatalf("wait should be ~10 minutes, got %v", left)
	}
	if _, err := b.get("alpha", 2); !errors.As(err, &busy) {
		t.Fatalf("second post of a blocked channel: want busy, got %v", err)
	}
	if n := fb.hits.Load(); n != 1 {
		t.Fatalf("blocked channel was asked %d times, want 1", n)
	}
	// Another channel is unaffected.
	d2, err := b.get("beta", 1)
	if err != nil {
		t.Fatal(err)
	}
	<-d2.started
	if _, _, _, e := d2.snapshot(); e != nil {
		t.Fatalf("other channel should work: %v", e)
	}
}

func TestBridgeEvictsOldest(t *testing.T) {
	fb := newFakeBridge(t, 40_000)
	b := newTestBridge(t, fb)
	b.maxTotal = 100_000 // room for two
	for id := 1; id <= 3; id++ {
		rec := httptest.NewRecorder()
		if ok, err := b.serve(rec, httptest.NewRequest("GET", "/x", nil), "alpha", id); !ok || err != nil {
			t.Fatal(err)
		}
		d, _ := b.get("alpha", id)
		for i := 0; i < 400; i++ {
			if _, _, done, _ := d.snapshot(); done {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond) // distinct LRU stamps + let evict run
	}
	if b.active("alpha", 1) {
		t.Fatal("oldest video should have been evicted")
	}
	if !b.active("alpha", 3) {
		t.Fatal("newest video must stay")
	}
}

// End to end through the feed: Telegram says "too big", the page asks
// /api/bigvideo, then plays /api/media from our copy.
func TestBigVideoThroughFeed(t *testing.T) {
	fb := newFakeBridge(t, 60_000)
	f := NewFeed()
	f.bridge = newTestBridge(t, fb)
	f.ListChannels = func() []string { return []string{"alpha"} }
	f.ResolveVideo = func(string, int) (string, error) { return "", ErrMediaTooBig }

	rec := httptest.NewRecorder()
	f.handleBigVideo(rec, httptest.NewRequest("GET", "/api/bigvideo?channel=alpha&id=9", nil))
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("bigvideo: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	f.handleMedia(rec, httptest.NewRequest("GET", "/api/media?channel=alpha&id=9", nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), fb.data) {
		t.Fatalf("media: code %d, %d bytes", rec.Code, rec.Body.Len())
	}

	// A channel the app does not follow is never fetched through the bridge.
	rec = httptest.NewRecorder()
	f.handleBigVideo(rec, httptest.NewRequest("GET", "/api/bigvideo?channel=stranger&id=1", nil))
	if strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatal("unfollowed channel must be refused")
	}
	if n := fb.hits.Load(); n != 1 {
		t.Fatalf("bridge hits %d, want 1", n)
	}

	// Without a prepare call: /api/media alone also works for a too-big post.
	rec = httptest.NewRecorder()
	f.handleMedia(rec, httptest.NewRequest("GET", "/api/media?channel=alpha&id=10", nil))
	if rec.Code != 200 || rec.Body.Len() != len(fb.data) {
		t.Fatalf("direct media: code %d, %d bytes", rec.Code, rec.Body.Len())
	}
}
