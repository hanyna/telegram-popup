package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub: just enough of the gists API.
type fakeGitHub struct {
	mu     sync.Mutex
	srv    *httptest.Server
	gists  map[string]string // id -> content of our file
	writes int
}

func newFakeGitHub(t *testing.T, wantToken string) *fakeGitHub {
	g := &fakeGitHub{gists: map[string]string{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			w.WriteHeader(401)
			io.WriteString(w, `{"message":"Bad credentials"}`)
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		doc := func(id string) map[string]any {
			return map[string]any{"id": id, "files": map[string]any{persistGistFile: map[string]any{"content": g.gists[id]}}}
		}
		var body struct {
			Files map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/gists":
			list := []any{}
			for id := range g.gists {
				list = append(list, map[string]any{"id": id, "files": map[string]any{persistGistFile: map[string]any{}}})
			}
			json.NewEncoder(w).Encode(list)
		case r.Method == "POST" && r.URL.Path == "/gists":
			json.NewDecoder(r.Body).Decode(&body)
			g.gists["g1"] = body.Files[persistGistFile].Content
			g.writes++
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(doc("g1"))
		case strings.HasPrefix(r.URL.Path, "/gists/"):
			id := strings.TrimPrefix(r.URL.Path, "/gists/")
			if _, ok := g.gists[id]; !ok {
				w.WriteHeader(404)
				return
			}
			if r.Method == "PATCH" {
				json.NewDecoder(r.Body).Decode(&body)
				g.gists[id] = body.Files[persistGistFile].Content
				g.writes++
			}
			json.NewEncoder(w).Encode(doc(id))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// What the owner set, the likes and the stats survive a "new machine".
func TestBackupSurvivesRestart(t *testing.T) {
	gh := newFakeGitHub(t, "tok-1")
	t.Setenv("TGPOPUP_GH_API", gh.srv.URL)
	files := []string{"adminstate.json", "reactions.json", "stats.json"}

	dir1 := t.TempDir()
	p1 := newPersister("tok-1", dir1, files, nil)
	if p1 == nil {
		t.Fatal("persister not created")
	}
	if err := p1.restore(); err != nil { // nothing saved yet
		t.Fatal(err)
	}
	m := newManageStore(filepath.Join(dir1, "adminstate.json"))
	m.setBanner("שלום לכולם")
	m.block("82.80.5.77")
	m.setChannels([]string{"alpha", "beta"})
	rs := newReactStore(filepath.Join(dir1, "reactions.json"))
	rs.set("alpha_1", "d:dev", "👍")
	p1.saveNow(5e9)
	if st := p1.status(); st["ok"] != true || st["lastSaved"] == nil {
		t.Fatalf("save status: %+v", st)
	}
	// The stored blob must not expose the visitor address or the notice.
	gh.mu.Lock()
	blob := gh.gists["g1"]
	gh.mu.Unlock()
	if blob == "" || strings.Contains(blob, "82.80.5.77") || strings.Contains(blob, "שלום") {
		t.Fatalf("backup is not encrypted: %.80s", blob)
	}
	// Unchanged files are not uploaded again.
	before := gh.writes
	p1.saveNow(5e9)
	if gh.writes != before {
		t.Fatal("unchanged state was uploaded again")
	}

	// A brand-new disk, same token.
	dir2 := t.TempDir()
	p2 := newPersister("tok-1", dir2, files, nil)
	if err := p2.restore(); err != nil {
		t.Fatal(err)
	}
	m2 := newManageStore(filepath.Join(dir2, "adminstate.json"))
	if text, _ := m2.banner(); text != "שלום לכולם" || !m2.isBlocked("82.80.5.77") {
		t.Fatal("owner settings not restored")
	}
	if own, ok := m2.channels(); !ok || len(own) != 2 {
		t.Fatalf("channels not restored: %v", own)
	}
	if newReactStore(filepath.Join(dir2, "reactions.json")).total() != 1 {
		t.Fatal("likes not restored")
	}
	// A later change updates the same gist.
	m2.setBanner("עדכון")
	p2.saveNow(5e9)
	if len(gh.gists) != 1 {
		t.Fatalf("want one gist, got %d", len(gh.gists))
	}

	// A different token cannot read the old backup: start clean, no crash.
	gh2 := newFakeGitHub(t, "tok-2")
	gh2.gists["g9"] = blob
	t.Setenv("TGPOPUP_GH_API", gh2.srv.URL)
	dir3 := t.TempDir()
	p3 := newPersister("tok-2", dir3, files, nil)
	if err := p3.restore(); err != nil {
		t.Fatalf("foreign backup should be skipped quietly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir3, "adminstate.json")); err == nil {
		t.Fatal("undecryptable backup must not write files")
	}
}

func TestBackupBadTokenReported(t *testing.T) {
	gh := newFakeGitHub(t, "right")
	t.Setenv("TGPOPUP_GH_API", gh.srv.URL)
	p := newPersister("wrong", t.TempDir(), []string{"adminstate.json"}, nil)
	if err := p.restore(); err == nil {
		t.Fatal("a rejected token must be reported")
	}
	if st := p.status(); st["ok"] != false || st["error"] == "" {
		t.Fatalf("status should carry the error: %+v", st)
	}
	if newPersister("", t.TempDir(), nil, nil) != nil {
		t.Fatal("no token = no persister")
	}
	var none *persister
	none.kick()
	none.stop()
	if none.status()["on"] != false {
		t.Fatal("nil persister must report off")
	}
}
