package main

// ---------------------------------------------------------------------------
// אפליקציית טלפון (PWA) — "התקן כאפליקציה" במסך הבית, אייקון, מסך פתיחה,
// ודף "מעיר את השרת" כשהשרת בענן ישן (Render החינמי נרדם אחרי רבע שעה).
//
// הכל נוצר מתוך הבינארי: האייקונים מצוירים בקוד (image/png), ה-manifest
// וה-service worker הם מחרוזות. אין קבצים חיצוניים, אין תלויות.
//
// ה-service worker מכוון שלא להפריע לדף החי:
//   - שומר במטמון רק את המעטפת: אייקונים, manifest, דף ההמתנה.
//   - ניווט לדף הראשי → תמיד מהרשת; אם הרשת נכשלת (שרת ישן/אין אינטרנט)
//     מוצג דף ההמתנה שמנסה שוב לבד ומרענן כשהשרת עונה.
//   - /api/ לא נוגע בו בכלל (במיוחד /api/stream — SSE חייב לזרום).
// ---------------------------------------------------------------------------

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"sync"
)

const manifestJSON = `{
  "name": "ערוץ חי",
  "short_name": "ערוץ חי",
  "description": "מעקב ערוצי טלגרם ציבוריים — דף חי מכל מקום",
  "lang": "he",
  "dir": "rtl",
  "start_url": "/?source=pwa",
  "scope": "/",
  "display": "standalone",
  "orientation": "portrait",
  "background_color": "#0b0e18",
  "theme_color": "#0f1320",
  "icons": [
    { "src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any" },
    { "src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any" },
    { "src": "/icon-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}`

// serviceWorkerJS — the %%VERSION%% stamp makes every release a new worker,
// which drops the old cache (and the old offline page) on activation.
const serviceWorkerJS = `// ערוץ חי — service worker v%%VERSION%%
var CACHE = 'tgpopup-shell-%%VERSION%%';
var SHELL = ['/offline', '/manifest.webmanifest', '/icon-192.png', '/icon-512.png', '/icon-maskable.png'];

self.addEventListener('install', function (ev) {
  ev.waitUntil(caches.open(CACHE).then(function (c) { return c.addAll(SHELL); }).then(function () { return self.skipWaiting(); }));
});

self.addEventListener('activate', function (ev) {
  ev.waitUntil(caches.keys().then(function (keys) {
    return Promise.all(keys.filter(function (k) { return k !== CACHE; }).map(function (k) { return caches.delete(k); }));
  }).then(function () { return self.clients.claim(); }));
});

self.addEventListener('fetch', function (ev) {
  var req = ev.request;
  if (req.method !== 'GET') return;
  var url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  // Live data is never cached — the stream, the APIs, the media proxy.
  if (url.pathname.indexOf('/api/') === 0) return;

  if (req.mode === 'navigate') {
    // The page itself: network first; the wake page when the network fails.
    ev.respondWith(fetch(req).catch(function () {
      return caches.match('/offline');
    }));
    return;
  }
  // Shell assets: cache first, refresh in the background.
  if (SHELL.indexOf(url.pathname) >= 0) {
    ev.respondWith(caches.match(req).then(function (hit) {
      var net = fetch(req).then(function (res) {
        if (res && res.ok) caches.open(CACHE).then(function (c) { c.put(req, res.clone()); });
        return res;
      }).catch(function () { return hit; });
      return hit || net;
    }));
  }
});

// Tapping a notification focuses the open tab (or opens one) on the message.
self.addEventListener('notificationclick', function (ev) {
  ev.notification.close();
  var target = (ev.notification.data && ev.notification.data.url) || '/';
  ev.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
    for (var i = 0; i < list.length; i++) {
      if ('focus' in list[i]) { list[i].navigate ? list[i].navigate(target) : null; return list[i].focus(); }
    }
    return self.clients.openWindow(target);
  }));
});
`

// offlinePage: shown by the service worker when the server does not answer.
// On Render's free plan that is almost always "asleep, waking up" — the
// first request wakes it and the page reloads itself the moment it answers.
const offlinePage = `<!doctype html>
<html lang="he" dir="rtl"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#0b0e18">
<title>ערוץ חי — מתחבר</title>
<style>
  body { margin:0; font-family:"Segoe UI",system-ui,-apple-system,sans-serif; background:#0b0e18; color:#eceff8;
         display:flex; align-items:center; justify-content:center; min-height:100vh; min-height:100dvh; text-align:center; }
  .card { padding:32px 28px; width:min(90vw,380px); }
  .logo { width:72px; height:72px; border-radius:22px; margin:0 auto 18px;
          background:linear-gradient(135deg,#37aee2,#1e88d2); display:flex; align-items:center; justify-content:center;
          box-shadow:0 8px 30px rgba(30,136,210,0.45); }
  .logo svg { width:38px; height:38px; }
  h1 { font-size:19px; margin:0 0 8px; }
  p { font-size:13.5px; color:#8b93a9; line-height:1.6; margin:0 0 20px; }
  .spin { display:inline-block; width:22px; height:22px; border:3px solid rgba(255,255,255,0.15); border-top-color:#37aee2;
          border-radius:50%; animation:spin 0.8s linear infinite; }
  @keyframes spin { to { transform:rotate(360deg); } }
  button { margin-top:18px; padding:10px 22px; border:none; border-radius:10px; font-size:14px; font-weight:600;
           background:rgba(255,255,255,0.08); color:#eceff8; cursor:pointer; }
  #sec { color:#5a6378; font-size:12px; margin-top:12px; }
</style></head><body>
<div class="card">
  <div class="logo"><svg viewBox="0 0 64 64"><path d="M14 14h36a6 6 0 016 6v18a6 6 0 01-6 6H32l-11 9v-9h-7a6 6 0 01-6-6V20a6 6 0 016-6z" fill="#fff"/></svg></div>
  <h1>מעיר את השרת…</h1>
  <p id="msg">השרת ישן אחרי זמן ללא מבקרים ומתעורר עכשיו.<br>זה לוקח בדרך כלל עד דקה. הדף ייפתח לבד.</p>
  <div class="spin"></div>
  <div id="sec"></div>
  <button onclick="location.reload()">נסה שוב עכשיו</button>
</div>
<script>
  var started = Date.now();
  function tick() {
    var s = Math.floor((Date.now() - started) / 1000);
    document.getElementById('sec').textContent = s ? s + ' שנ׳' : '';
    if (!navigator.onLine) document.getElementById('msg').textContent = 'אין חיבור לאינטרנט. הדף ייפתח ברגע שהחיבור יחזור.';
  }
  setInterval(tick, 1000);
  function probe() {
    fetch('/manifest.webmanifest', { cache: 'no-store' }).then(function (r) {
      if (r.ok) location.replace('/'); else setTimeout(probe, 4000);
    }).catch(function () { setTimeout(probe, 4000); });
  }
  setTimeout(probe, 2500);
  window.addEventListener('online', probe);
</script>
</body></html>`

// ---- icons, drawn in code -------------------------------------------------

var iconOnce sync.Once
var iconPNG map[string][]byte

// buildIcons renders the app icon: a rounded blue gradient square with a
// white chat bubble, at the sizes the manifest declares. "maskable" keeps
// the bubble inside the safe zone so Android's circle/squircle masks do not
// clip it.
func buildIcons() {
	iconPNG = map[string][]byte{
		"/icon-192.png":         drawIcon(192, false),
		"/icon-512.png":         drawIcon(512, false),
		"/icon-maskable.png":    drawIcon(512, true),
		"/apple-touch-icon.png": drawIcon(180, true),
		"/favicon.ico":          drawIcon(64, false), // PNG bytes; browsers accept them under this name
	}
}

func drawIcon(size int, maskable bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fs := float64(size)
	// Corner radius: a rounded square for "any", fully filled for maskable.
	radius := fs * 0.22
	if maskable {
		radius = 0
	}
	// Bubble geometry (fractions of the icon) — shrunk for maskable.
	pad := 0.22
	if maskable {
		pad = 0.30
	}
	bx0, by0 := fs*pad, fs*(pad+0.02)
	bx1, by1 := fs*(1-pad), fs*(1-pad-0.08)
	br := (by1 - by0) * 0.22
	// Tail of the bubble: a small triangle hanging off the bottom-right (RTL
	// side), starting inside the bubble so the rounded corner leaves no gap.
	tx, ty := bx1-br*1.9, by1-br*0.9
	tw, th := br*1.5, br*1.9
	// "Live" dot inside the bubble.
	dcx, dcy, dr := bx0+(bx1-bx0)*0.5, by0+(by1-by0)*0.5, (by1-by0)*0.13

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			// Background gradient, clipped to the rounded square.
			if !insideRoundRect(px, py, 0, 0, fs, fs, radius) {
				img.Set(x, y, color.NRGBA{0, 0, 0, 0})
				continue
			}
			t := (px + py) / (2 * fs)
			c := lerpColor(color.NRGBA{0x37, 0xae, 0xe2, 255}, color.NRGBA{0x1e, 0x88, 0xd2, 255}, t)
			// White bubble.
			if insideRoundRect(px, py, bx0, by0, bx1, by1, br) || insideTriangle(px, py, tx, ty, tx+tw, ty, tx+tw*0.62, ty+th) {
				c = color.NRGBA{255, 255, 255, 255}
				if (px-dcx)*(px-dcx)+(py-dcy)*(py-dcy) <= dr*dr {
					c = color.NRGBA{0x31, 0xc4, 0x8d, 255}
				}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func insideRoundRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	if r <= 0 {
		return true
	}
	// Corner tests.
	cx, cy := px, py
	if px < x0+r {
		cx = x0 + r
	} else if px > x1-r {
		cx = x1 - r
	}
	if py < y0+r {
		cy = y0 + r
	} else if py > y1-r {
		cy = y1 - r
	}
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

func insideTriangle(px, py, x1, y1, x2, y2, x3, y3 float64) bool {
	d1 := (px-x2)*(y1-y2) - (x1-x2)*(py-y2)
	d2 := (px-x3)*(y2-y3) - (x2-x3)*(py-y3)
	d3 := (px-x1)*(y3-y1) - (x3-x1)*(py-y1)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

// registerPWA mounts the PWA routes on the feed's mux.
func registerPWA(mux *http.ServeMux) {
	iconOnce.Do(buildIcons)
	mux.HandleFunc("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(manifestJSON))
	})
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // browsers re-check the worker on their own schedule
		w.Header().Set("Service-Worker-Allowed", "/")
		_, _ = w.Write([]byte(strings.ReplaceAll(serviceWorkerJS, "%%VERSION%%", version)))
	})
	mux.HandleFunc("/offline", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(offlinePage))
	})
	for path, data := range iconPNG {
		data := data
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=604800")
			_, _ = w.Write(data)
		})
	}
}
