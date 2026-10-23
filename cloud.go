package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	// Embedded timezone database: the scratch-based cloud image has no
	// /usr/share/zoneinfo, and without this every timestamp would show UTC.
	// TZ=Asia/Jerusalem in the Dockerfile picks the zone; this makes it exist.
	_ "time/tzdata"
)

// ---------------------------------------------------------------------------
// Cloud mode: the same binary, running headless on a server (Render, Fly,
// any Docker host) so the live feed is reachable from anywhere — including a
// phone. Activated by the PORT environment variable, which every major host
// sets automatically. On a desktop nothing here runs.
//
// Differences from desktop mode, all applied in applyCloudEnv:
//   - listens on 0.0.0.0:$PORT instead of 127.0.0.1 (the page must be
//     reachable from outside the machine)
//   - popups, tray, sounds and browser-opening are off — there is no desktop
//   - channels can come from the TGPOPUP_CHANNELS env var (comma-separated),
//     because cloud filesystems are wiped on every deploy
//   - the page is protected by an access key (TGPOPUP_KEY): without it,
//     anyone who guessed the URL could read the feed and change the channel
//     list. The key is entered once; a cookie remembers it.
// ---------------------------------------------------------------------------

// inCloud reports whether we are running on a hosting platform. The PORT env
// var is the platform signal (Render, Fly, Heroku all set it) — but ONLY on
// non-Windows: developer machines sometimes carry a stray PORT variable, and
// flipping a desktop install into headless server mode because of it would
// look exactly like "the app opens nothing".
func inCloud() bool {
	return runtime.GOOS != "windows" && os.Getenv("PORT") != ""
}

// applyCloudEnv rewrites the config for headless hosting and returns the
// bind address and access key the feed should use.
func applyCloudEnv(cfg *Config) (bindAddr, accessKey string) {
	if !inCloud() {
		return "", ""
	}
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 && p < 65536 {
		cfg.FeedPort = p
	}
	// No desktop on a server: everything that pops, plays or opens is off.
	cfg.Popups = false
	cfg.Sound = false
	cfg.OpenFeedOnStart = false
	cfg.AppWindow = false
	cfg.Feed = true // the page IS the product online

	if chs := envChannels(); len(chs) > 0 {
		cfg.Channels = chs
	}
	return "0.0.0.0", os.Getenv("TGPOPUP_KEY")
}

// envChannels parses TGPOPUP_CHANNELS ("elisha_yered, hakolhayehudi, ...").
func envChannels() []string {
	raw := os.Getenv("TGPOPUP_CHANNELS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == ';'
	}) {
		ch := normalizeChannel(part)
		if ch != "" && !seen[ch] {
			seen[ch] = true
			out = append(out, ch)
		}
	}
	return out
}

const (
	authCookie = "tgpopup_key"
	loginPath  = "/login"
)

// sessionToken derives the cookie value from the key, so the raw key never
// sits in the browser's cookie jar. Same key → same token, so nothing has
// to be stored server-side and a redeploy does not log anyone out.
func sessionToken(key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("tgpopup-session-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// publicAsset: PWA plumbing the browser fetches before (or without) a cookie
// — manifest, service worker, icons, the offline/wake page. None of them
// carry feed data, so they sit outside the key.
func publicAsset(path string) bool {
	switch path {
	case "/manifest.webmanifest", "/sw.js", "/offline", "/favicon.ico", "/icon-192.png", "/icon-512.png", "/icon-maskable.png", "/apple-touch-icon.png":
		return true
	}
	return false
}

// requestIsHTTPS: Render terminates TLS and forwards plain HTTP with
// X-Forwarded-Proto. The Secure cookie flag must only be set when the
// browser really is on https, otherwise it would never be sent back.
func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// withAccessKey wraps the whole site behind a shared key.
//
//   - Browsers log in with a POST form on /login; a derived session cookie
//     carries the login from then on (a year, HttpOnly, Secure on https).
//   - Machine clients (the Yemot bridge) keep passing ?k=KEY on /api/ calls.
//   - A page opened with ?k=KEY (old bookmarks) gets the cookie and is sent
//     to the same address WITHOUT the key, so it leaves the history bar.
//   - Six wrong keys in ten minutes → two-minute cool-down per client.
func withAccessKey(next http.Handler, key string) http.Handler {
	if key == "" {
		return next
	}
	keyBytes := []byte(key)
	token := []byte(sessionToken(key))
	okKey := func(candidate string) bool {
		return subtle.ConstantTimeCompare([]byte(candidate), keyBytes) == 1
	}
	okCookie := func(candidate string) bool {
		// Old installs still hold the raw key as the cookie value.
		return subtle.ConstantTimeCompare([]byte(candidate), token) == 1 || okKey(candidate)
	}
	limiter := newLoginLimiter()

	setCookie := func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name: authCookie, Value: string(token), Path: "/",
			MaxAge: 365 * 24 * 3600, HttpOnly: true,
			Secure:   requestIsHTTPS(r),
			SameSite: http.SameSiteLaxMode,
		})
	}
	showLogin := func(w http.ResponseWriter, status int, msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(strings.Replace(loginPage, "%%ERR%%", html.EscapeString(msg), 1)))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicAsset(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == loginPath {
			if r.Method != http.MethodPost {
				showLogin(w, http.StatusOK, "")
				return
			}
			ip := clientIP(r)
			if blocked, wait := limiter.blocked(ip); blocked {
				showLogin(w, http.StatusTooManyRequests,
					"יותר מדי ניסיונות. נסה שוב בעוד "+strconv.Itoa(int(wait/time.Second)+1)+" שניות.")
				return
			}
			_ = r.ParseForm()
			k := strings.TrimSpace(r.PostFormValue("k"))
			if k == "" || !okKey(k) {
				limiter.fail(ip)
				showLogin(w, http.StatusUnauthorized, "מפתח גישה שגוי")
				return
			}
			limiter.success(ip)
			setCookie(w, r)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie(authCookie); err == nil && okCookie(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if k := r.URL.Query().Get("k"); k != "" {
			ip := clientIP(r)
			if blocked, _ := limiter.blocked(ip); blocked {
				http.Error(w, `{"error":"too many attempts"}`, http.StatusTooManyRequests)
				return
			}
			if okKey(k) {
				limiter.success(ip)
				if strings.HasPrefix(r.URL.Path, "/api/") {
					// Machine client: answer in place, no cookie dance.
					next.ServeHTTP(w, r)
					return
				}
				setCookie(w, r)
				q := r.URL.Query()
				q.Del("k")
				clean := r.URL.Path
				if enc := q.Encode(); enc != "" {
					clean += "?" + enc
				}
				http.Redirect(w, r, clean, http.StatusSeeOther)
				return
			}
			limiter.fail(ip)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		showLogin(w, http.StatusUnauthorized, "")
	})
}

// loginPage is what a visitor without the key sees — a single field, Hebrew,
// styled to match the app. %%ERR%% is replaced with an error line (or "").
const loginPage = `<!doctype html>
<html lang="he" dir="rtl"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#0b0e18">
<link rel="manifest" href="/manifest.webmanifest">
<link rel="icon" href="/icon-192.png">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<title>ערוץ חי — כניסה</title>
<style>
  body { margin:0; font-family:"Segoe UI",system-ui,-apple-system,sans-serif;
         background:#0b0e18; color:#eceff8; display:flex;
         align-items:center; justify-content:center; min-height:100vh; min-height:100dvh; }
  .card { background:#12172a; border:1px solid rgba(255,255,255,0.08);
          border-radius:16px; padding:36px 32px; width:min(90vw,360px);
          text-align:center; box-shadow:0 20px 60px rgba(0,0,0,0.5); }
  .logo { font-size:34px; margin-bottom:10px; }
  h1 { font-size:19px; margin:0 0 6px; }
  p  { font-size:13px; color:#8b93a9; margin:0 0 22px; }
  input { width:100%; box-sizing:border-box; padding:12px 14px;
          border-radius:10px; border:1px solid rgba(255,255,255,0.14);
          background:#0b0e18; color:#eceff8; font-size:16px; text-align:center;
          outline:none; }
  input:focus { border-color:#37aee2; }
  button { width:100%; margin-top:14px; padding:12px; border:none;
           border-radius:10px; font-size:16px; font-weight:600; cursor:pointer;
           background:linear-gradient(135deg,#37aee2,#31c48d); color:#fff; }
  .err { color:#ff8a72; font-size:12px; margin-top:12px; min-height:15px; }
</style></head><body>
<form class="card" method="post" action="/login" autocomplete="off">
  <div class="logo">📡</div>
  <h1>ערוץ חי</h1>
  <p>הזן את מפתח הגישה כדי להיכנס</p>
  <input id="k" name="k" type="password" placeholder="מפתח גישה" autofocus required>
  <button type="submit">כניסה</button>
  <div class="err" id="e">%%ERR%%</div>
</form>
</body></html>`
