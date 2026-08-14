package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The one-click self-update: upload to GitHub once → the desktop offers the
// new version. These tests cover the whole chain against a fake repo.

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"6.5", "6.4", 1}, {"6.4", "6.5", -1}, {"6.5", "6.5", 0},
		{"6.5", "6.4.1", 1}, {"6.4.1", "6.4", 1}, {"6.10", "6.9", 1},
		{"7.0", "6.9.9", 1}, {"6.4", "6.4.0", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// buildFakeRepo serves version.json and an Update.zip like raw.githubusercontent.
func buildFakeRepo(t *testing.T, latest string, exe []byte) *httptest.Server {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	fw, _ := zw.Create("TelegramPopup.new")
	_, _ = fw.Write(exe)
	_ = zw.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/version.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version": "` + latest + `"}`))
	})
	mux.HandleFunc("/downloads/TelegramPopup-Update.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zbuf.Bytes())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeExe(size int) []byte {
	b := make([]byte, size)
	b[0], b[1] = 'M', 'Z'
	return b
}

func TestFetchLatestVersion(t *testing.T) {
	srv := buildFakeRepo(t, "9.9", fakeExe(2<<20))
	v, err := fetchLatestVersion(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if v != "9.9" {
		t.Fatalf("got %q", v)
	}
}

func TestDownloadUpdateExtractsAndValidates(t *testing.T) {
	dir := t.TempDir()
	srv := buildFakeRepo(t, "9.9", fakeExe(2<<20))
	if err := downloadUpdate(srv.URL, dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "TelegramPopup.new"))
	if err != nil || info.Size() != 2<<20 {
		t.Fatalf("extracted binary wrong: %v / %v", err, info)
	}
}

func TestDownloadUpdateRejectsGarbage(t *testing.T) {
	dir := t.TempDir()

	// Not a PE binary.
	srv := buildFakeRepo(t, "9.9", bytes.Repeat([]byte("A"), 2<<20))
	if err := downloadUpdate(srv.URL, dir); err == nil {
		t.Fatal("a non-executable payload must be rejected")
	}
	// Too small to be the real app.
	srv2 := buildFakeRepo(t, "9.9", fakeExe(1024))
	if err := downloadUpdate(srv2.URL, dir); err == nil {
		t.Fatal("a suspiciously tiny binary must be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "TelegramPopup.new")); err == nil {
		t.Fatal("rejected payloads must never land on disk")
	}
}

func TestUpdateAPIReportsAvailability(t *testing.T) {
	srv := buildFakeRepo(t, "99.0", fakeExe(2<<20))

	f := NewFeed()
	f.CheckUpdate = func() (string, error) { return fetchLatestVersion(srv.URL) }
	url, _ := f.Start(0)

	resp, err := http.Get(url + "/api/update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Current   string `json:"current"`
		Latest    string `json:"latest"`
		Available bool   `json:"available"`
		Supported bool   `json:"supported"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if !got.Supported || !got.Available || got.Latest != "99.0" || got.Current != version {
		t.Fatalf("bad update report: %+v", got)
	}
}

func TestUpdateAPIQuietWhenCurrent(t *testing.T) {
	f := NewFeed()
	f.CheckUpdate = func() (string, error) { return version, nil }
	url, _ := f.Start(0)
	resp, err := http.Get(url + "/api/update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Available bool `json:"available"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Available {
		t.Fatal("same version must not offer an update")
	}
}

func TestUpdateAPICloudUnsupported(t *testing.T) {
	f := NewFeed() // no hooks wired = cloud
	url, _ := f.Start(0)
	resp, err := http.Get(url + "/api/update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Supported bool `json:"supported"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Supported {
		t.Fatal("cloud must report self-update unsupported")
	}
}

func TestPageHasUpdateButton(t *testing.T) {
	for _, want := range []string{"updrow", "updbtn", "checkUpdate", "/api/update"} {
		if !bytes.Contains([]byte(feedPage), []byte(want)) {
			t.Errorf("page missing update UI piece %q", want)
		}
	}
}
