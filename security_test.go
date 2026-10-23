package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAllowedMediaHost(t *testing.T) {
	ok := []string{
		"https://cdn4.telesco.pe/file/a.mp4?token=x",
		"https://cdn5.cdn-telegram.org/file/b.jpg",
		"https://t.me/s/x",
	}
	bad := []string{
		"http://cdn4.telesco.pe/file/a.mp4",   // plain http
		"https://evil.com/telesco.pe/a.mp4",   // suffix in path, not host
		"https://telesco.pe.evil.com/a.mp4",   // suffix in the middle
		"https://10.0.0.5/internal.mp4",       // raw IP
		"https://[::1]/x.mp4",
		"file:///etc/passwd",
		"",
	}
	for _, u := range ok {
		if !allowedMediaHost(u) {
			t.Errorf("should allow %s", u)
		}
	}
	for _, u := range bad {
		if allowedMediaHost(u) {
			t.Errorf("should refuse %s", u)
		}
	}
}

func TestGuardRefusesForeignHostAndOrigin(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := guardRequests(inner, false)

	req := httptest.NewRequest("POST", "http://evil.example:8420/api/update", nil)
	req.Host = "evil.example:8420"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("rebinding host should be refused, got %d", rec.Code)
	}

	req = httptest.NewRequest("POST", "http://127.0.0.1:8420/api/update", strings.NewReader("{}"))
	req.Host = "127.0.0.1:8420"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("foreign origin should be refused, got %d", rec.Code)
	}

	// A form POST (what a hostile page can send without CORS) is refused.
	req = httptest.NewRequest("POST", "http://127.0.0.1:8420/api/autostart", strings.NewReader("on=true"))
	req.Host = "127.0.0.1:8420"
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Fatalf("non-JSON POST should be refused, got %d", rec.Code)
	}

	// The page's own fetch passes.
	req = httptest.NewRequest("POST", "http://localhost:8420/api/settings", strings.NewReader("{}"))
	req.Host = "localhost:8420"
	req.Header.Set("Origin", "http://localhost:8420")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("same-origin JSON POST should pass, got %d", rec.Code)
	}
}

func TestLoginFlow(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("feed")) })
	h := withAccessKey(inner, "secret-key")

	// No cookie → login page, 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "/login") {
		t.Fatalf("expected login page, got %d", rec.Code)
	}

	// Wrong key → 401 with message.
	form := url.Values{"k": {"nope"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "שגוי") {
		t.Fatalf("wrong key: got %d", rec.Code)
	}

	// Right key → cookie + redirect to / (key never in the URL).
	form = url.Values{"k": {"secret-key"}}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("login should redirect, got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "secret-key" || !cookie.Secure || !cookie.HttpOnly {
		t.Fatalf("cookie must be derived, Secure and HttpOnly: %+v", cookie)
	}

	// Cookie admits.
	req = httptest.NewRequest("GET", "/api/messages", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "feed" {
		t.Fatalf("cookie should admit, got %d", rec.Code)
	}

	// Legacy raw-key cookie still admits.
	req = httptest.NewRequest("GET", "/api/messages", nil)
	req.AddCookie(&http.Cookie{Name: authCookie, Value: "secret-key"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("legacy cookie should admit, got %d", rec.Code)
	}

	// Machine client: ?k= on /api/ answers in place.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/messages?k=secret-key", nil))
	if rec.Code != 200 || rec.Body.String() != "feed" {
		t.Fatalf("?k= on api should answer in place, got %d", rec.Code)
	}

	// Old bookmark: ?k= on the page sets the cookie and strips the key.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/?k=secret-key", nil))
	if rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("page ?k= should redirect clean, got %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// Public PWA assets need no key.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	if rec.Code != 200 {
		t.Fatalf("manifest should be public, got %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := withAccessKey(inner, "secret-key")
	for i := 0; i < loginMaxFails; i++ {
		req := httptest.NewRequest("POST", "/login", strings.NewReader("k=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.9:1234"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader("k=secret-key"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.9:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 429 {
		t.Fatalf("after %d failures the client must wait, got %d", loginMaxFails, rec.Code)
	}
	// Another client is unaffected.
	req = httptest.NewRequest("GET", "/api/messages?k=secret-key", nil)
	req.RemoteAddr = "198.51.100.1:5"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("other client should pass, got %d", rec.Code)
	}
}
