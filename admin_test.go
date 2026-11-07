package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatsAdminGate(t *testing.T) {
	// Cloud feed with an admin key: stats require the admin cookie.
	f := NewFeed()
	f.BindAddr = "0.0.0.0"
	f.AdminKey = "sekret"
	f.stats = newStatsStore("")

	// no admin cookie -> 403
	rec := httptest.NewRecorder()
	f.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	if rec.Code != 403 {
		t.Fatalf("non-admin should be forbidden, got %d", rec.Code)
	}

	// correct admin cookie -> 200
	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.AddCookie(&http.Cookie{Name: "tgpopup_admin", Value: adminToken("sekret")})
	rec = httptest.NewRecorder()
	f.handleStats(rec, req)
	if rec.Code != 200 {
		t.Fatalf("admin cookie should pass, got %d", rec.Code)
	}

	// wrong admin cookie -> 403
	req = httptest.NewRequest("GET", "/api/stats", nil)
	req.AddCookie(&http.Cookie{Name: "tgpopup_admin", Value: "nope"})
	rec = httptest.NewRecorder()
	f.handleStats(rec, req)
	if rec.Code != 403 {
		t.Fatalf("wrong admin cookie should be forbidden, got %d", rec.Code)
	}

	// Desktop (no BindAddr) is always admin.
	d := NewFeed()
	d.stats = newStatsStore("")
	rec = httptest.NewRecorder()
	d.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("desktop should be admin, got %d", rec.Code)
	}

	// Cloud with no admin key -> nobody is admin.
	n := NewFeed()
	n.BindAddr = "0.0.0.0"
	n.stats = newStatsStore("")
	rec = httptest.NewRecorder()
	n.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	if rec.Code != 403 {
		t.Fatalf("cloud without admin key should forbid stats, got %d", rec.Code)
	}
}

func TestAdminUnlockViaHandlePage(t *testing.T) {
	f := NewFeed()
	f.BindAddr = "0.0.0.0"
	f.AdminKey = "sekret"
	// ?admin=correct sets the cookie and redirects
	req := httptest.NewRequest("GET", "/?admin=sekret", nil)
	rec := httptest.NewRecorder()
	f.handlePage(rec, req)
	if rec.Code != 303 {
		t.Fatalf("admin unlock should redirect, got %d", rec.Code)
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tgpopup_admin" {
			ck = c
		}
	}
	if ck == nil || ck.Value != adminToken("sekret") {
		t.Fatalf("admin cookie not set correctly: %+v", ck)
	}
	// wrong admin just redirects without setting the cookie
	rec = httptest.NewRecorder()
	f.handlePage(rec, httptest.NewRequest("GET", "/?admin=wrong", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tgpopup_admin" {
			t.Fatal("wrong admin key must not set a cookie")
		}
	}
}

func TestAdminClaimFlow(t *testing.T) {
	dir := t.TempDir()
	f := NewFeed()
	f.BindAddr = "0.0.0.0" // cloud
	f.AdminKey = ""        // no env key -> claim mode
	f.admin = newAdminStore(dir + "/admin.json")
	f.stats = newStatsStore("")

	// before claiming, nobody is admin
	rec := httptest.NewRecorder()
	f.handleStats(rec, httptest.NewRequest("GET", "/api/stats", nil))
	if rec.Code != 403 {
		t.Fatalf("pre-claim should forbid, got %d", rec.Code)
	}

	// first "?admin=claim" claims and sets the cookie
	rec = httptest.NewRecorder()
	if !f.handleAdminUnlock(rec, httptest.NewRequest("GET", "/?admin=claim", nil)) {
		t.Fatal("unlock should handle claim")
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tgpopup_admin" {
			ck = c
		}
	}
	if ck == nil || ck.Value == "" {
		t.Fatal("claim must set an admin cookie")
	}
	tok := f.admin.token()
	if tok == "" || ck.Value != tok {
		t.Fatalf("cookie should equal the stored token")
	}

	// that cookie now grants stats
	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.AddCookie(ck)
	rec = httptest.NewRecorder()
	f.handleStats(rec, req)
	if rec.Code != 200 {
		t.Fatalf("claimed admin should see stats, got %d", rec.Code)
	}

	// a reload of the store keeps the same token (persisted)
	f2 := NewFeed()
	f2.BindAddr = "0.0.0.0"
	f2.admin = newAdminStore(dir + "/admin.json")
	if f2.admin.token() != tok {
		t.Fatal("token should survive reload")
	}

	// unlocking another device with the token link works
	rec = httptest.NewRecorder()
	f.handleAdminUnlock(rec, httptest.NewRequest("GET", "/?admin="+tok, nil))
	got := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tgpopup_admin" {
			got = c.Value
		}
	}
	if got != tok {
		t.Fatal("token link should set the admin cookie on another device")
	}

	// a wrong token does not
	rec = httptest.NewRecorder()
	f.handleAdminUnlock(rec, httptest.NewRequest("GET", "/?admin=wrongtoken", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tgpopup_admin" {
			t.Fatal("wrong token must not unlock")
		}
	}
}
