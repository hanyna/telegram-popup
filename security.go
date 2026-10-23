package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// הגנות על השרת — במחשב ובענן.
//
//   1. guardRequests: חוסם בקשות שמגיעות מאתר זר (CSRF) ו-DNS rebinding.
//      במחשב השרת מאזין רק ב-127.0.0.1, אבל כל דף אינטרנט פתוח יכול לשלוח
//      POST ל-localhost:8420 ולשנות הגדרות או להפעיל עדכון. לכן:
//        - Host חייב להיות הכתובת שלנו (במחשב: localhost/127.0.0.1)
//        - Origin, אם קיים, חייב להתאים ל-Host
//        - Sec-Fetch-Site: cross-site נדחה
//        - בקשות שמשנות מצב חייבות Content-Type: application/json
//          (טופס HTML לא יכול לשלוח כזה בלי CORS — וזה בדיוק מה שחוסם)
//   2. allowedMediaHost: הפרוקסי של הסרטונים (/api/media) מוריד כתובות
//      שמופיעות בתוך דפי טלגרם. בלי סינון, פוסט שמכיל קישור ל-mp4 בכתובת
//      פנימית היה גורם לשרת (בענן!) לגשת לרשת הפנימית שלו (SSRF).
//      מותרים רק דומייני טלגרם.
//   3. loginLimiter: מגביל ניסיונות ניחוש של מפתח הגישה בענן.
// ---------------------------------------------------------------------------

// telegramHostSuffixes are the only hosts the media/avatar proxies will fetch.
var telegramHostSuffixes = []string{
	"telesco.pe", "telegram.org", "t.me", "telegram.me",
	"cdn-telegram.org", "telegram-cdn.org", "tg.dev",
}

// extraMediaHosts: TGPOPUP_MEDIA_HOSTS (comma-separated) — for tests and for
// anyone fronting Telegram through their own proxy. Plain http is allowed
// only for these.
func extraMediaHosts() []string {
	raw := os.Getenv("TGPOPUP_MEDIA_HOSTS")
	if raw == "" {
		return nil
	}
	var out []string
	for _, h := range strings.Split(raw, ",") {
		if h = strings.TrimSpace(strings.ToLower(h)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// allowedMediaHost reports whether the proxy may fetch this URL.
func allowedMediaHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, extra := range extraMediaHosts() {
		if host == extra {
			return u.Scheme == "https" || u.Scheme == "http"
		}
	}
	if u.Scheme != "https" {
		return false
	}
	// Never follow a URL that names an IP address — the allowlist is by name.
	if net.ParseIP(host) != nil {
		return false
	}
	for _, suf := range telegramHostSuffixes {
		if host == suf || strings.HasSuffix(host, "."+suf) {
			return true
		}
	}
	return false
}

// hostOnly strips the port from a Host header value.
func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.Trim(h, "[]"))
}

func isLoopbackHost(h string) bool {
	switch hostOnly(h) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// guardRequests wraps the mux with the CSRF / rebinding checks described
// above. cloud=false means the desktop server, where Host must be loopback.
func guardRequests(next http.Handler, cloud bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cloud && !isLoopbackHost(r.Host) {
			// DNS rebinding: a hostile page resolving its own domain to
			// 127.0.0.1 arrives with its own Host header — refuse it.
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			// Origin / Sec-Fetch-Site are checked ONLY on state-changing
			// requests. A plain GET arriving from another site is just
			// someone clicking a link to the page (it carries
			// Sec-Fetch-Site: cross-site too), and reads of /api/ from a
			// foreign page are already stopped by the browser (no CORS).
			if o := r.Header.Get("Origin"); o != "" && o != "null" {
				if ou, err := url.Parse(o); err != nil || hostOnly(ou.Host) != hostOnly(r.Host) {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
			if sfs := r.Header.Get("Sec-Fetch-Site"); sfs == "cross-site" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			// The login form is the one legitimate non-JSON POST.
			if r.URL.Path != loginPath {
				ct := strings.ToLower(r.Header.Get("Content-Type"))
				if !strings.HasPrefix(ct, "application/json") && r.ContentLength != 0 {
					http.Error(w, `{"error":"content-type must be application/json"}`, http.StatusUnsupportedMediaType)
					return
				}
				if r.ContentLength == 0 && ct != "" && !strings.HasPrefix(ct, "application/json") {
					http.Error(w, `{"error":"content-type must be application/json"}`, http.StatusUnsupportedMediaType)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// loginLimiter counts failed key attempts per client address. After
// loginMaxFails failures inside loginWindow the client waits loginCooldown.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
	block map[string]time.Time
}

const (
	loginMaxFails = 6
	loginWindow   = 10 * time.Minute
	loginCooldown = 2 * time.Minute
)

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{fails: map[string][]time.Time{}, block: map[string]time.Time{}}
}

// clientIP: Render (and every reverse proxy) puts the visitor in
// X-Forwarded-For; the first entry is the real client.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// blocked reports whether the client must wait, and for how long.
func (l *loginLimiter) blocked(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until, ok := l.block[ip]; ok {
		if d := time.Until(until); d > 0 {
			return true, d
		}
		delete(l.block, ip)
	}
	return false, 0
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if now.Sub(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	l.fails[ip] = kept
	if len(kept) >= loginMaxFails {
		l.block[ip] = now.Add(loginCooldown)
		delete(l.fails, ip)
	}
	// Keep the maps from growing forever under a scan.
	if len(l.fails) > 5000 {
		for k, ts := range l.fails {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > loginWindow {
				delete(l.fails, k)
			}
		}
	}
	if len(l.block) > 5000 {
		for k, until := range l.block {
			if now.After(until) {
				delete(l.block, k)
			}
		}
	}
}

func (l *loginLimiter) success(ip string) {
	l.mu.Lock()
	delete(l.fails, ip)
	delete(l.block, ip)
	l.mu.Unlock()
}

// securityHeaders adds the browser-side protections every response gets.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
