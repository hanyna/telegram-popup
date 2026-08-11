package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strconv"
	"strings"
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

// inCloud reports whether we are running on a hosting platform.
func inCloud() bool { return os.Getenv("PORT") != "" }

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

const authCookie = "tgpopup_key"

// withAccessKey wraps the whole site behind a shared key. The key arrives
// once as ?k=... (typed into a small Hebrew login page); a long-lived cookie
// carries it from then on, so the EventSource stream and every API call pass
// without extra work.
func withAccessKey(next http.Handler, key string) http.Handler {
	if key == "" {
		return next
	}
	keyBytes := []byte(key)
	okKey := func(candidate string) bool {
		return subtle.ConstantTimeCompare([]byte(candidate), keyBytes) == 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(authCookie); err == nil && okKey(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if k := r.URL.Query().Get("k"); k != "" && okKey(k) {
			http.SetCookie(w, &http.Cookie{
				Name: authCookie, Value: k, Path: "/",
				MaxAge: 365 * 24 * 3600, HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(loginPage))
	})
}

// loginPage is what a visitor without the key sees — a single field, Hebrew,
// styled to match the app.
const loginPage = `<!doctype html>
<html lang="he" dir="rtl"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ערוץ חי — כניסה</title>
<style>
  body { margin:0; font-family:"Segoe UI",-apple-system,sans-serif;
         background:#0b0e18; color:#eceff8; display:flex;
         align-items:center; justify-content:center; min-height:100vh; }
  .card { background:#12172a; border:1px solid rgba(255,255,255,0.08);
          border-radius:16px; padding:36px 32px; width:min(90vw,360px);
          text-align:center; box-shadow:0 20px 60px rgba(0,0,0,0.5); }
  .logo { font-size:34px; margin-bottom:10px; }
  h1 { font-size:19px; margin:0 0 6px; }
  p  { font-size:13px; color:#8b93a9; margin:0 0 22px; }
  input { width:100%; box-sizing:border-box; padding:12px 14px;
          border-radius:10px; border:1px solid rgba(255,255,255,0.14);
          background:#0b0e18; color:#eceff8; font-size:15px; text-align:center;
          outline:none; }
  input:focus { border-color:#37aee2; }
  button { width:100%; margin-top:14px; padding:12px; border:none;
           border-radius:10px; font-size:15px; font-weight:600; cursor:pointer;
           background:linear-gradient(135deg,#37aee2,#31c48d); color:#fff; }
  .err { color:#ff8a72; font-size:12px; margin-top:12px; min-height:15px; }
</style></head><body>
<div class="card">
  <div class="logo">📡</div>
  <h1>ערוץ חי</h1>
  <p>הזן את מפתח הגישה כדי להיכנס</p>
  <input id="k" type="password" placeholder="מפתח גישה" autofocus>
  <button onclick="go()">כניסה</button>
  <div class="err" id="e"></div>
</div>
<script>
  function go(){
    var v = document.getElementById('k').value.trim();
    if(!v){ document.getElementById('e').textContent='הזן מפתח'; return; }
    location.href = '/?k=' + encodeURIComponent(v);
  }
  document.getElementById('k').addEventListener('keydown', function(ev){
    if(ev.key==='Enter') go();
  });
</script>
</body></html>`
