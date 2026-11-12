package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegalPagesPublicAndComplete(t *testing.T) {
	f := NewFeed()
	// Behind the access key, like the cloud deployment.
	h := withAccessKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/terms":
			f.handleTerms(w, r)
		case "/accessibility":
			f.handleAccessibility(w, r)
		default:
			w.WriteHeader(200)
		}
	}), "secret")

	for path, must := range map[string][]string{
		"/terms":         {"תקנון", "פרטיות", "דיני מדינת ישראל", "ערוצי טלגרם"},
		"/accessibility": {"הצהרת נגישות", "5568", "WCAG 2.0", "רכז הנגישות", "הסתייגויות", "עודכן לאחרונה"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s must be reachable without the key, got %d", path, rec.Code)
		}
		body := rec.Body.String()
		for _, m := range must {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing %q", path, m)
			}
		}
		if !strings.Contains(body, `lang="he"`) || !strings.Contains(body, `dir="rtl"`) || !strings.Contains(body, "דלג לתוכן") {
			t.Errorf("%s lacks lang/dir/skip-link", path)
		}
	}
}

func TestContactLineFromEnv(t *testing.T) {
	t.Setenv("TGPOPUP_CONTACT", "טלפון 02-1234567 <x>")
	if got := contactLine(); !strings.Contains(got, "02-1234567") || strings.Contains(got, "<x>") {
		t.Fatalf("contact must come from env and be escaped, got %q", got)
	}
}
