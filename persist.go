package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Permanent backup for the cloud.
//
// On Render's free plan the disk is thrown away on every restart — and the
// service restarts every time it wakes from sleep. Without a backup the
// owner's settings (channels, hidden posts, blocks, notice), the likes and
// the visit stats would all vanish several times a day.
//
// With TGPOPUP_GH_TOKEN set (a GitHub token that may only touch gists), the
// app keeps one private gist holding those files, encrypted with a key
// derived from the token (the list of visitor addresses is in there, so the
// gist's content must be unreadable even to someone who finds its link):
//
//   - on start: download, decrypt, write the files before anything reads them;
//   - while running: upload when a file changed — soon after an owner action,
//     at most every few minutes for likes / stats;
//   - on shutdown: one last upload.
//
// Without the token nothing changes: the files stay local and are lost on
// restart, and the owner's panel says so.

const (
	persistGistFile = "mivzakim-state.enc"
	persistGistDesc = "מבזקי ארץ ישראל — גיבוי נתוני האתר (מוצפן)"
	persistAPI      = "https://api.github.com"
)

type persister struct {
	token string
	api   string
	dir   string
	files []string // base names inside dir
	key   [32]byte
	hc    *http.Client
	logf  func(string, ...any)

	urgentEvery time.Duration // after an owner action
	calmEvery   time.Duration // likes / stats

	mu        sync.Mutex
	gistID    string
	lastHash  [32]byte // of what the gist holds
	lastSaved time.Time
	lastErr   string
	restored  bool
	urgent    bool
	stopCh    chan struct{}
}

// newPersister returns nil when no token is configured.
func newPersister(token, dir string, files []string, logf func(string, ...any)) *persister {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	api := persistAPI
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("TGPOPUP_GH_API")), "/"); v != "" {
		// Tests only: a stand-in server on this machine.
		if u, err := url.Parse(v); err == nil && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
			api = v
		}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	p := &persister{
		token: token, api: api, dir: dir, files: files, logf: logf,
		key:         sha256.Sum256([]byte("mivzakim-state\x00" + token)),
		hc:          &http.Client{Timeout: 25 * time.Second},
		urgentEvery: 8 * time.Second,
		calmEvery:   3 * time.Minute,
		stopCh:      make(chan struct{}),
	}
	return p
}

func (p *persister) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(data)
	}
	target := path
	if !strings.HasPrefix(path, "http") {
		target = p.api + path
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "mivzakim-state")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
	return resp.StatusCode, data, err
}

func ghError(status int, body []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("GitHub דחה את הטוקן (לא תקין או שפג תוקפו)")
	case http.StatusForbidden, http.StatusNotFound:
		return errors.New("לטוקן אין הרשאת gist")
	}
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	return fmt.Errorf("GitHub ענה %d %s", status, e.Message)
}

type gistFile struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	RawURL    string `json:"raw_url"`
}

type gistDoc struct {
	ID    string              `json:"id"`
	Files map[string]gistFile `json:"files"`
}

// findGist looks for our backup among the token owner's gists.
func (p *persister) findGist(ctx context.Context) (string, error) {
	for page := 1; page <= 5; page++ {
		status, body, err := p.do(ctx, http.MethodGet, fmt.Sprintf("/gists?per_page=100&page=%d", page), nil)
		if err != nil {
			return "", err
		}
		if status != http.StatusOK {
			return "", ghError(status, body)
		}
		var list []gistDoc
		if err := json.Unmarshal(body, &list); err != nil {
			return "", err
		}
		for _, g := range list {
			if _, ok := g.Files[persistGistFile]; ok {
				return g.ID, nil
			}
		}
		if len(list) < 100 {
			break
		}
	}
	return "", nil
}

func (p *persister) seal(plain []byte) (string, error) {
	block, err := aes.NewCipher(p.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

func (p *persister) open(sealed string) ([]byte, error) {
	sealed = strings.TrimSpace(sealed)
	if !strings.HasPrefix(sealed, "v1:") {
		return nil, errors.New("unknown backup format")
	}
	raw, err := base64.StdEncoding.DecodeString(sealed[3:])
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(p.key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("backup too short")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}

// bundle reads the local files into one JSON document: name -> content.
func (p *persister) bundle() ([]byte, [32]byte) {
	doc := map[string]string{}
	for _, name := range p.files {
		if data, err := os.ReadFile(filepath.Join(p.dir, name)); err == nil && len(data) > 0 {
			doc[name] = string(data)
		}
	}
	data, _ := json.Marshal(doc) // map keys are sorted: stable hash
	return data, sha256.Sum256(data)
}

// restore downloads the backup and writes the files. Call it before the
// stores that read those files are created.
func (p *persister) restore() error {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	err := p.restoreCtx(ctx)
	p.mu.Lock()
	if err != nil {
		p.lastErr = err.Error()
	} else {
		p.lastErr = ""
		p.restored = true
	}
	p.mu.Unlock()
	return err
}

func (p *persister) restoreCtx(ctx context.Context) error {
	id, err := p.findGist(ctx)
	if err != nil {
		return err
	}
	if id == "" {
		return nil // first run: nothing saved yet
	}
	p.mu.Lock()
	p.gistID = id
	p.mu.Unlock()
	status, body, err := p.do(ctx, http.MethodGet, "/gists/"+id, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return ghError(status, body)
	}
	var g gistDoc
	if err := json.Unmarshal(body, &g); err != nil {
		return err
	}
	file := g.Files[persistGistFile]
	content := file.Content
	if file.Truncated && file.RawURL != "" {
		st, raw, rErr := p.do(ctx, http.MethodGet, file.RawURL, nil)
		if rErr != nil || st != http.StatusOK {
			return errors.New("הגיבוי גדול ולא נקרא במלואו")
		}
		content = string(raw)
	}
	plain, err := p.open(content)
	if err != nil {
		// A different token than the one that wrote it: start fresh rather
		// than refuse to run. The next save overwrites it.
		p.logf("גיבוי: לא ניתן לפענח את הגיבוי הקיים (טוקן אחר?) — מתחילים מחדש")
		return nil
	}
	var doc map[string]string
	if err := json.Unmarshal(plain, &doc); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, n := range p.files {
		allowed[n] = true
	}
	n := 0
	for name, data := range doc {
		if !allowed[name] || data == "" {
			continue
		}
		if writeFileAtomic(filepath.Join(p.dir, name), []byte(data), 0o600) == nil {
			n++
		}
	}
	p.mu.Lock()
	p.lastHash = sha256.Sum256(plain)
	p.lastSaved = time.Now()
	p.mu.Unlock()
	p.logf("גיבוי: שוחזרו %d קבצים מהגיבוי הקבוע", n)
	return nil
}

// save uploads the current files if they differ from what the gist holds.
func (p *persister) save(ctx context.Context) error {
	plain, hash := p.bundle()
	p.mu.Lock()
	same := hash == p.lastHash
	id := p.gistID
	p.mu.Unlock()
	if same {
		return nil
	}
	sealed, err := p.seal(plain)
	if err != nil {
		return err
	}
	files := map[string]any{persistGistFile: map[string]string{"content": sealed}}
	var status int
	var body []byte
	if id == "" {
		if id, err = p.findGist(ctx); err != nil {
			return err
		}
	}
	if id == "" {
		status, body, err = p.do(ctx, http.MethodPost, "/gists",
			map[string]any{"description": persistGistDesc, "public": false, "files": files})
		if err != nil {
			return err
		}
		if status != http.StatusCreated && status != http.StatusOK {
			return ghError(status, body)
		}
		var g gistDoc
		if err := json.Unmarshal(body, &g); err != nil || g.ID == "" {
			return errors.New("GitHub לא החזיר מזהה לגיבוי")
		}
		id = g.ID
	} else {
		status, body, err = p.do(ctx, http.MethodPatch, "/gists/"+id, map[string]any{"files": files})
		if err != nil {
			return err
		}
		if status == http.StatusNotFound {
			// The gist was deleted by hand: forget it, the next round makes
			// a new one.
			p.mu.Lock()
			p.gistID = ""
			p.mu.Unlock()
			return errors.New("הגיבוי נמחק מ-GitHub — ייווצר מחדש בשמירה הבאה")
		}
		if status != http.StatusOK {
			return ghError(status, body)
		}
	}
	p.mu.Lock()
	p.gistID = id
	p.lastHash = hash
	p.lastSaved = time.Now()
	p.mu.Unlock()
	return nil
}

func (p *persister) saveNow(timeout time.Duration) {
	if p == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := p.save(ctx)
	p.mu.Lock()
	if err != nil {
		if p.lastErr != err.Error() {
			p.logf("גיבוי: השמירה נכשלה: %v", err)
		}
		p.lastErr = err.Error()
	} else {
		p.lastErr = ""
	}
	p.mu.Unlock()
}

// kick: an owner action just changed something — save soon.
func (p *persister) kick() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.urgent = true
	p.mu.Unlock()
}

// run is the background loop. Stop it with stop().
func (p *persister) run() {
	if p == nil {
		return
	}
	tick := time.NewTicker(4 * time.Second)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-p.stopCh:
			return
		case <-tick.C:
			p.mu.Lock()
			urgent := p.urgent
			p.mu.Unlock()
			wait := p.calmEvery
			if urgent {
				wait = p.urgentEvery
			}
			if time.Since(last) < wait {
				continue
			}
			p.mu.Lock()
			p.urgent = false
			p.mu.Unlock()
			p.saveNow(30 * time.Second)
			last = time.Now()
		}
	}
}

// stop ends the loop and uploads one last time.
func (p *persister) stop() {
	if p == nil {
		return
	}
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
	p.saveNow(12 * time.Second)
}

// status for the owner's panel.
func (p *persister) status() map[string]any {
	if p == nil {
		return map[string]any{"on": false}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]any{"on": true, "ok": p.lastErr == "", "error": p.lastErr}
	if !p.lastSaved.IsZero() {
		out["lastSaved"] = p.lastSaved.Unix()
	}
	return out
}
