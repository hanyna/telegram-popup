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
