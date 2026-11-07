package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

// adminStore holds the admin secret when it is NOT provided via the
// TGPOPUP_ADMIN_KEY env var. The first person to visit "/?admin=claim" on a
// fresh install becomes the admin: a random token is generated, saved, and
// set as their cookie. After that, claim does nothing — the owner unlocks
// extra devices with "/?admin=<token>" (the stats panel offers that link).
//
// This needs no Render dashboard change. On the free cloud plan the token
// file resets on a redeploy (like the archive), so the owner re-claims; set
// TGPOPUP_ADMIN_KEY for a permanent, non-resetting admin key instead.
type adminStore struct {
	mu    sync.Mutex
	path  string
	Token string `json:"token"`
}

func newAdminStore(path string) *adminStore {
	a := &adminStore{path: path}
	if path != "" {
		if data, err := readFileMaybe(path); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, a)
		}
	}
	return a
}

func (a *adminStore) token() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Token
}

// claim returns the existing token, or generates and persists one if unset.
func (a *adminStore) claim() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Token == "" {
		a.Token = newDeviceID() + newDeviceID() // 32 hex chars
		if a.path != "" {
			data, _ := json.Marshal(a)
			_ = writeFileAtomic(a.path, data, 0o600)
		}
	}
	return a.Token
}

// effectiveAdminToken is the cookie value that marks an admin, for whichever
// mode is active: env key (hashed) or the claimed token. "" means admin is
// impossible in the cloud (no env key and nothing claimed yet).
func (f *Feed) effectiveAdminToken() string {
	if f.AdminKey != "" {
		return adminToken(f.AdminKey)
	}
	if f.admin != nil {
		return f.admin.token()
	}
	return ""
}

// handleAdminUnlock processes a "?admin=..." query on the page. Returns true
// if it handled the request (the caller should stop after redirecting).
func (f *Feed) handleAdminUnlock(w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query().Get("admin")
	if q == "" {
		return false
	}
	setCookie := func(val string) {
		http.SetCookie(w, &http.Cookie{Name: "tgpopup_admin", Value: val, Path: "/",
			MaxAge: 365 * 24 * 3600, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode})
	}
	switch {
	case f.AdminKey != "":
		// Permanent env key: "/?admin=<key>" unlocks.
		if q == f.AdminKey {
			setCookie(adminToken(f.AdminKey))
		}
	case f.admin != nil:
		if q == "claim" {
			setCookie(f.admin.claim()) // first claim wins; later claims return the same token
		} else if tok := f.admin.token(); tok != "" && q == tok {
			setCookie(tok) // unlock an additional device with the token link
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther) // strip ?admin from the URL
	return true
}
