package main

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPWAAssetsServe(t *testing.T) {
	mux := http.NewServeMux()
	registerPWA(mux)
	for _, path := range []string{"/manifest.webmanifest", "/sw.js", "/offline", "/icon-192.png", "/icon-512.png", "/icon-maskable.png", "/apple-touch-icon.png"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 || rec.Body.Len() == 0 {
			t.Errorf("%s: %d / %d bytes", path, rec.Code, rec.Body.Len())
		}
		if strings.HasSuffix(path, ".png") {
			img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
			if err != nil {
				t.Errorf("%s: not a PNG: %v", path, err)
			} else if img.Bounds().Dx() != img.Bounds().Dy() {
				t.Errorf("%s: not square", path)
			}
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/sw.js", nil))
	if !strings.Contains(rec.Body.String(), "tgpopup-shell-"+version) {
		t.Error("service worker must be stamped with the version")
	}
	if strings.Contains(rec.Body.String(), "%%VERSION%%") {
		t.Error("version placeholder left in service worker")
	}
}
