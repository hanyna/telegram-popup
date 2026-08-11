//go:build telegram

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/amarnathcjd/gogram/telegram"
)

// ---------------------------------------------------------------------------
// Optional Telegram account client — the ONLY way to fetch large ("too big")
// videos that Telegram never exposes to anonymous web visitors. Entirely
// opt-in: without api_id/api_hash in the config and a saved session, none of
// this runs and the app behaves exactly as the anonymous build.
//
// All gogram usage is isolated in this file. The login flow runs once from a
// console (Login.bat); afterwards the main windowless app reuses the saved
// session silently.
// ---------------------------------------------------------------------------

type TGClient struct {
	appID    int32
	appHash  string
	session  string // path to the session file
	cacheDir string

	mu     sync.Mutex
	client *telegram.Client
	ready  bool
}

// NewTGClient prepares the client but does not connect. Returns nil when the
// feature is not configured, so callers can treat "not set up" as "absent".
func NewTGClient(appID int, appHash, sessionPath, cacheDir string) *TGClient {
	if appID == 0 || appHash == "" {
		return nil
	}
	_ = os.MkdirAll(cacheDir, 0o755)
	return &TGClient{
		appID:    int32(appID),
		appHash:  appHash,
		session:  sessionPath,
		cacheDir: cacheDir,
	}
}

// HasSession reports whether a login was already completed.
func (t *TGClient) HasSession() bool {
	if t == nil {
		return false
	}
	info, err := os.Stat(t.session)
	return err == nil && info.Size() > 0
}

func (t *TGClient) newRawClient() (*telegram.Client, error) {
	return telegram.NewClient(telegram.ClientConfig{
		AppID:        t.appID,
		AppHash:      t.appHash,
		Session:      t.session,
		LogLevel:     telegram.LogError,
		DisableCache: true,
	})
}

// InteractiveLogin runs the phone → code → (2FA) flow on the console, using
// the supplied callbacks to read input. The session is saved to disk.
func (t *TGClient) InteractiveLogin(phone string, codeCb func() (string, error), passwordCb func() (string, error)) error {
	c, err := t.newRawClient()
	if err != nil {
		return err
	}
	if err := c.Connect(); err != nil {
		return err
	}
	ok, err := c.Login(phone, &telegram.LoginOptions{
		CodeCallback:     codeCb,
		PasswordCallback: passwordCb,
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("ההתחברות לא הושלמה")
	}
	return nil
}

// ensure lazily connects the shared client used for downloads.
func (t *TGClient) ensure() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ready && t.client != nil {
		return nil
	}
	if !t.HasSession() {
		return errors.New("אין סשן טלגרם — יש להתחבר קודם (Login.bat)")
	}
	c, err := t.newRawClient()
	if err != nil {
		return err
	}
	if err := c.Connect(); err != nil {
		return err
	}
	if au, _ := c.IsAuthorized(); !au {
		return errors.New("סשן הטלגרם אינו מאושר — יש להתחבר מחדש (Login.bat)")
	}
	t.client = c
	t.ready = true
	return nil
}

// DownloadVideo fetches one post's video via the Telegram API and returns the
// path of the saved mp4. Cached on disk so each video downloads once.
func (t *TGClient) DownloadVideo(channel string, id int) (string, error) {
	if t == nil {
		return "", errors.New("לקוח טלגרם לא מוגדר")
	}
	dest := filepath.Join(t.cacheDir, channel+"_"+strconv.Itoa(id)+".mp4")
	if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
		return dest, nil
	}
	if err := t.ensure(); err != nil {
		return "", err
	}

	t.mu.Lock()
	c := t.client
	t.mu.Unlock()

	msg, err := c.GetMessageByID(channel, int32(id))
	if err != nil {
		return "", err
	}
	if msg == nil || !msg.IsMedia() {
		return "", errors.New("אין מדיה בהודעה")
	}

	tmp := dest + ".part"
	_, err = c.DownloadMedia(msg.Media(), &telegram.DownloadOptions{
		FileName: tmp,
		Threads:  4,
	})
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}
