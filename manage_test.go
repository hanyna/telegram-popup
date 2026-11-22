package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newManagedFeed(t *testing.T) (*Feed, *http.Cookie) {
	t.Helper()
	f := NewFeed()
	f.BindAddr = "0.0.0.0" // online
	f.AdminKey = "owner-secret"
	f.manage = newManageStore(filepath.Join(t.TempDir(), "adminstate.json"))
	f.reacts = newReactStore("")
	return f, &http.Cookie{Name: "tgpopup_admin", Value: adminToken("owner-secret")}
}

func managePost(f *Feed, ck *http.Cookie, body string) (int, map[string]any) {
	r := httptest.NewRequest("POST", "/api/admin", strings.NewReader(body))
	r.Header.Set("X-Forwarded-For", "31.154.1.2")
	if ck != nil {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	f.handleManage(rec, r)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestManageAdminOnly(t *testing.T) {
	f, _ := newManagedFeed(t)
	if code, _ := managePost(f, nil, `{"action":"banner","text":"x"}`); code != 403 {
		t.Fatalf("visitor must not manage, got %d", code)
	}
	rec := httptest.NewRecorder()
	f.handleManage(rec, httptest.NewRequest("GET", "/api/admin", nil))
	if rec.Code != 403 {
		t.Fatalf("visitor must not read the panel, got %d", rec.Code)
	}
	if text, _ := f.manage.banner(); text != "" {
		t.Fatal("banner changed by a visitor")
	}
}

func TestHideMessageForEveryone(t *testing.T) {
	f, ck := newManagedFeed(t)
	f.Add([]Message{
		{ID: 1, Channel: "alpha", Text: "נשארת", Time: "2026-11-20T10:00:00+00:00"},
		{ID: 2, Channel: "alpha", Text: "מוסתרת", Time: "2026-11-20T11:00:00+00:00"},
	})
	if code, out := managePost(f, ck, `{"action":"hide","key":"alpha_2"}`); code != 200 || out["error"] != nil {
		t.Fatalf("hide failed: %d %v", code, out)
	}
	rec := httptest.NewRecorder()
	f.handleMessages(rec, httptest.NewRequest("GET", "/api/messages", nil))
	if strings.Contains(rec.Body.String(), "מוסתרת") || !strings.Contains(rec.Body.String(), "נשארת") {
		t.Fatalf("hidden post still served: %s", rec.Body.String())
	}
	// A later scan or the archive must not bring it back.
	f.Add([]Message{{ID: 2, Channel: "alpha", Text: "מוסתרת", Time: "2026-11-20T11:00:00+00:00"}})
	f.AddOlder([]Message{{ID: 2, Channel: "alpha", Text: "מוסתרת", Time: "2026-11-20T11:00:00+00:00"}})
	rec = httptest.NewRecorder()
	f.handleMessages(rec, httptest.NewRequest("GET", "/api/messages", nil))
	if strings.Contains(rec.Body.String(), "מוסתרת") {
		t.Fatal("hidden post came back")
	}
	// Every viewer is told which keys are hidden (to drop cached copies).
	rec = httptest.NewRecorder()
	f.handleSettings(rec, httptest.NewRequest("GET", "/api/settings", nil))
	if !strings.Contains(rec.Body.String(), `"alpha_2"`) {
		t.Fatalf("settings must list hidden keys: %s", rec.Body.String())
	}
	// It survives a restart (same file), and unhide lets it back in.
	f2 := NewFeed()
	f2.manage = newManageStore(f.manage.path)
	if !f2.manage.isHidden("alpha_2") {
		t.Fatal("hidden list did not persist")
	}
	managePost(f, ck, `{"action":"unhide","key":"alpha_2"}`)
	f.Add([]Message{{ID: 2, Channel: "alpha", Text: "מוסתרת", Time: "2026-11-20T11:00:00+00:00"}})
	rec = httptest.NewRecorder()
	f.handleMessages(rec, httptest.NewRequest("GET", "/api/messages", nil))
	if !strings.Contains(rec.Body.String(), "מוסתרת") {
		t.Fatal("unhidden post should return")
	}
}

func TestBannerForViewers(t *testing.T) {
	f, ck := newManagedFeed(t)
	managePost(f, ck, `{"action":"banner","text":"  האתר   בשיפוצים  "}`)
	rec := httptest.NewRecorder()
	f.handleSettings(rec, httptest.NewRequest("GET", "/api/settings", nil))
	if !strings.Contains(rec.Body.String(), "האתר בשיפוצים") {
		t.Fatalf("viewers should get the notice: %s", rec.Body.String())
	}
	managePost(f, ck, `{"action":"banner","text":""}`)
	if text, _ := f.manage.banner(); text != "" {
		t.Fatal("notice not cleared")
	}
}

func TestBlockAddress(t *testing.T) {
	f, ck := newManagedFeed(t)
	page := f.blockGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	visit := func(ip string, ck *http.Cookie, path string) int {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-Forwarded-For", ip)
		if ck != nil {
			r.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		page.ServeHTTP(rec, r)
		return rec.Code
	}
	if _, out := managePost(f, ck, `{"action":"block","ip":"not-an-ip"}`); out["error"] == nil {
		t.Fatal("junk address accepted")
	}
	if _, out := managePost(f, ck, `{"action":"block","ip":"31.154.1.2"}`); out["error"] == nil {
		t.Fatal("the owner must not be able to block their own address")
	}
	if _, out := managePost(f, ck, `{"action":"block","ip":"82.80.5.77"}`); out["error"] != nil {
		t.Fatalf("block failed: %v", out)
	}
	if visit("82.80.5.77", nil, "/") != 403 || visit("82.80.5.77", nil, "/api/messages") != 403 {
		t.Fatal("blocked address still gets in")
	}
	if visit("1.1.1.1", nil, "/") != 200 {
		t.Fatal("other addresses must be unaffected")
	}
	// The owner is never locked out, even from a blocked address.
	if visit("82.80.5.77", ck, "/") != 200 || visit("82.80.5.77", nil, "/?admin=owner-secret") != 200 {
		t.Fatal("owner locked out")
	}
	managePost(f, ck, `{"action":"unblock","ip":"82.80.5.77"}`)
	if visit("82.80.5.77", nil, "/") != 200 {
		t.Fatal("unblock failed")
	}
}

func TestLikesReset(t *testing.T) {
	f, ck := newManagedFeed(t)
	a := &http.Cookie{Name: "tgpopup_dev", Value: "device-aaaaaaaa"}
	reactPost(f, "alpha_1", "👍", a)
	reactPost(f, "alpha_2", "🔥", a)
	managePost(f, ck, `{"action":"likes_clear","key":"alpha_1"}`)
	got := reactGet(f, a)
	if len(got.Counts["alpha_1"]) != 0 || got.Counts["alpha_2"]["🔥"] != 1 {
		t.Fatalf("single reset wrong: %+v", got)
	}
	_, out := managePost(f, ck, `{"action":"likes_clear","key":""}`)
	if got := reactGet(f, a); len(got.Counts) != 0 || out["likes"].(float64) != 0 {
		t.Fatalf("full reset wrong: %+v", got)
	}
}

// Online only the owner may add / remove channels; the owner's list is saved.
func TestChannelsOwnerOnly(t *testing.T) {
	f, ck := newManagedFeed(t)
	list := []string{"alpha"}
	f.AdminChannels = true
	f.ListChannels = func() []string { return list }
	f.AddChannel = func(raw string) (string, error) {
		list = append(list, raw)
		f.manage.setChannels(list)
		return raw, nil
	}
	f.RemoveChannel = func(string) error { return nil }
	post := func(ck *http.Cookie) int {
		r := httptest.NewRequest("POST", "/api/channels", strings.NewReader(`{"channel":"beta"}`))
		if ck != nil {
			r.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		f.handleChannels(rec, r)
		return rec.Code
	}
	if post(nil) == 200 || len(list) != 1 {
		t.Fatal("a visitor added a channel")
	}
	rec := httptest.NewRecorder()
	f.handleChannels(rec, httptest.NewRequest("GET", "/api/channels", nil))
	if !strings.Contains(rec.Body.String(), `"locked":true`) {
		t.Fatalf("list must look locked to visitors: %s", rec.Body.String())
	}
	if post(ck) != 200 || len(list) != 2 {
		t.Fatal("the owner could not add a channel")
	}
	if own, ok := newManageStore(f.manage.path).channels(); !ok || len(own) != 2 {
		t.Fatalf("owner's channel list not saved: %v", own)
	}
}
