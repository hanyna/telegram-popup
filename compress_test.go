package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLeanGzipAndETag(t *testing.T) {
	big := strings.Repeat(`{"text":"שלום עולם","html":"<div class=\"msgtext\">x</div>"},`, 200)
	h := lean(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, big)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	// לקוח Go רגיל: מבקש gzip לבד ומקבל את הגוף המקורי.
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != big || !resp.Uncompressed {
		t.Fatalf("gzip round trip failed (uncompressed=%v, len=%d)", resp.Uncompressed, len(body))
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	// גודל בפועל ברשת
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, _ = http.DefaultTransport.RoundTrip(req)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(raw)*4 > len(big) {
		t.Fatalf("not compressed enough: %d of %d", len(raw), len(big))
	}
	zr, _ := gzip.NewReader(strings.NewReader(string(raw)))
	if dec, _ := io.ReadAll(zr); string(dec) != big {
		t.Fatal("bad gzip")
	}

	// אותה רשימה שוב → 304 בלי גוף
	req, _ = http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("If-None-Match", etag)
	resp, _ = http.DefaultClient.Do(req)
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified || len(raw) != 0 {
		t.Fatalf("want 304 empty, got %d len %d", resp.StatusCode, len(raw))
	}

	// לקוח בלי gzip — מקבל רגיל
	req, _ = http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Accept-Encoding", "identity")
	resp, _ = http.DefaultTransport.RoundTrip(req)
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(raw) != big || resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("identity client got compressed body")
	}
}

func TestAcceptsGzip(t *testing.T) {
	for ae, want := range map[string]bool{"gzip": true, "br, gzip;q=0.5": true, "gzip;q=0": false, "identity": false, "": false} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", ae)
		if acceptsGzip(r) != want {
			t.Errorf("%q → %v", ae, !want)
		}
	}
}

func TestMediaRedirect(t *testing.T) {
	f := NewFeed()
	f.ResolveVideo = func(channel string, id int) (string, error) { return "https://cdn4.telesco.pe/file/v.mp4?token=abc", nil }
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(url + "/api/media?channel=mychan&id=42&redirect=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://cdn4.telesco.pe/file/v.mp4?token=abc" {
		t.Fatalf("status %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestMessagesLean(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(url + "/api/messages")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	req, _ := http.NewRequest("GET", url+"/api/messages", nil)
	req.Header.Set("If-None-Match", resp.Header.Get("ETag"))
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.Header.Get("ETag") == "" || resp.StatusCode != http.StatusNotModified {
		t.Fatalf("messages: status %d etag %q", resp.StatusCode, resp.Header.Get("ETag"))
	}
}
