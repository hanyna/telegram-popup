package main

// חיסכון ברוחב פס (Render החינמי מוגבל ל-5GB בחודש):
//
//   - דחיסה (gzip): רשימת ההודעות היא JSON עם HTML — נדחסת פי 6-10.
//     Go (הגשר לימות) ודפדפנים מבקשים gzip מעצמם, בלי שום שינוי אצלם.
//   - ETag: לקוח שכבר קיבל את אותה רשימה מקבל "304 — לא השתנה" בלי גוף.
//     הגשר בודק כל 20 שניות, והרשימה משתנה בערך פעם בדקה — רוב הבדיקות
//     עולות כמה מאות בתים במקום את כל הרשימה.
//
// לא עוטפים את /api/stream (SSE — חייב לזרום) ולא מדיה/תמונות (כבר דחוסות).

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// bufferedWriter אוסף את כל התשובה כדי לחשב ETag ולדחוס לפני השליחה.
type bufferedWriter struct {
	h      http.Header
	status int
	buf    bytes.Buffer
}

func (b *bufferedWriter) Header() http.Header { return b.h }
func (b *bufferedWriter) WriteHeader(s int) {
	if b.status == 0 {
		b.status = s
	}
}
func (b *bufferedWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.buf.Write(p)
}

// gzipMinBytes: תשובה קטנה מזה — לא שווה לדחוס.
const gzipMinBytes = 1024

// lean עוטף handler: ETag + 304 לבקשות GET, ודחיסת gzip כשהלקוח מקבל אותה.
func lean(next http.HandlerFunc) http.HandlerFunc {
	// One-entry cache per wrapped handler: the same body (same ETag) is
	// gzipped once, not once per client. /api/messages barely changes
	// between the page's and the bridge's polls, so most hits are reused.
	var (
		cacheMu   sync.Mutex
		cacheTag  string
		cacheGzip []byte
	)
	return func(w http.ResponseWriter, r *http.Request) {
		bw := &bufferedWriter{h: http.Header{}}
		next(bw, r)
		if bw.status == 0 {
			bw.status = http.StatusOK
		}
		for k, v := range bw.h {
			w.Header()[k] = v
		}
		body := bw.buf.Bytes()
		w.Header().Add("Vary", "Accept-Encoding")

		if r.Method == http.MethodGet && bw.status == http.StatusOK {
			sum := sha1.Sum(body)
			etag := `"` + hex.EncodeToString(sum[:10]) + `"`
			w.Header().Set("ETag", etag)
			if etagMatch(r.Header.Get("If-None-Match"), etag) {
				w.Header().Del("Content-Length")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		if len(body) >= gzipMinBytes && acceptsGzip(r) && w.Header().Get("Content-Encoding") == "" {
			etag := w.Header().Get("ETag")
			cacheMu.Lock()
			if etag != "" && etag == cacheTag && cacheGzip != nil {
				body = cacheGzip
				cacheMu.Unlock()
			} else {
				cacheMu.Unlock()
				var zb bytes.Buffer
				zw, _ := gzip.NewWriterLevel(&zb, gzip.BestSpeed)
				_, _ = zw.Write(body)
				_ = zw.Close()
				body = zb.Bytes()
				if etag != "" {
					cacheMu.Lock()
					cacheTag, cacheGzip = etag, body
					cacheMu.Unlock()
				}
			}
			w.Header().Set("Content-Encoding", "gzip")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(bw.status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.SplitN(part, ";", 2)
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		if len(fields) == 2 {
			q := strings.TrimSpace(fields[1])
			if strings.HasPrefix(q, "q=") {
				if v, err := strconv.ParseFloat(q[2:], 64); err == nil && v == 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

func etagMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "W/"))
		if t == etag || t == "*" {
			return true
		}
	}
	return false
}
