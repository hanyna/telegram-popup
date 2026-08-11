//go:build !telegram

package main

import "fmt"

// This is the DEFAULT build: no Telegram account client, no gogram dependency.
// The binary stays lean (~7 MB, pure standard library) so Windows SmartScreen
// and antivirus treat it like any small utility. Large "too big" videos are
// unavailable in this build; everything else works exactly the same.
//
// To enable large-video downloads, build with:  go build -tags telegram

type TGClient struct{}

// NewTGClient always returns nil here — the feature is compiled out.
func NewTGClient(appID int, appHash, sessionPath, cacheDir string) *TGClient {
	return nil
}

func (t *TGClient) HasSession() bool { return false }

func (t *TGClient) DownloadVideo(channel string, id int) (string, error) {
	return "", fmt.Errorf("הורדת סרטונים ארוכים אינה זמינה בגרסה זו")
}

// runLogin explains that login lives in the separate Telegram-enabled helper.
func runLogin(dir string, cfg Config) {
	alert("Telegram Popup",
		"התחברות לטלגרם (לסרטונים ארוכים) זמינה בקובץ העזר הנפרד.\n"+
			"הגרסה הרגילה עובדת בלי זה — סרטונים קצרים ובינוניים מתנגנים כרגיל.")
}
