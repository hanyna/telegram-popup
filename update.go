package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Self-update from the user's own GitHub repository.
//
// The flow the user asked for: they upload a new release to GitHub once —
// Render redeploys the online page by itself, and the DESKTOP app notices the
// new version and offers a one-click update button. Clicking it downloads
// downloads/TelegramPopup-Update.zip from the repo, extracts the new binary,
// and swaps it in with the same stop→replace→restart dance Update.bat does.
//
// version.json at the repo root is the source of truth:
//   {"version": "6.5"}
// ---------------------------------------------------------------------------

// updateClient is small-timeout: update checks must never hang the UI.
var updateClient = &http.Client{Timeout: 20 * time.Second}

// downloadClient allows the few-MB binary fetch more time.
var downloadClient = &http.Client{Timeout: 3 * time.Minute}

// fetchLatestVersion reads version.json from the repo and returns the version
// string, e.g. "6.5".
func fetchLatestVersion(baseURL string) (string, error) {
	resp, err := updateClient.Get(strings.TrimRight(baseURL, "/") + "/version.json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("version.json: status " + strconv.Itoa(resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	// Tolerant extraction — the file is trivial and hand-editable.
	s := string(raw)
	i := strings.Index(s, `"version"`)
	if i < 0 {
		return "", errors.New("version.json חסר שדה version")
	}
	rest := s[i+len(`"version"`):]
	q1 := strings.Index(rest, `"`)
	if q1 < 0 {
		return "", errors.New("version.json פגום")
	}
	q2 := strings.Index(rest[q1+1:], `"`)
	if q2 < 0 {
		return "", errors.New("version.json פגום")
	}
	v := rest[q1+1 : q1+1+q2]
	if v == "" {
		return "", errors.New("version.json ריק")
	}
	return v, nil
}

// compareVersions returns +1 if a is newer than b, -1 if older, 0 if equal.
// Handles "6.5" vs "6.4.1" style strings; unparseable parts compare as 0.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if na != nb {
			if na > nb {
				return 1
			}
			return -1
		}
	}
	return 0
}

// downloadUpdate fetches the Update package from the repo and extracts the
// new binary to <dir>/TelegramPopup.new. The binary is sanity-checked (PE
// header, minimum size) before it is allowed to land on disk.
func downloadUpdate(baseURL, dir string) error {
	url := strings.TrimRight(baseURL, "/") + "/downloads/TelegramPopup-Update.zip"
	resp, err := downloadClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("הורדת העדכון נכשלה: סטטוס " + strconv.Itoa(resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return errors.New("קובץ העדכון פגום (לא ZIP)")
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != "TelegramPopup.new" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		exe, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			return err
		}
		// A real Windows binary: MZ magic and a sane size.
		if len(exe) < 1<<20 || len(exe) < 2 || exe[0] != 'M' || exe[1] != 'Z' {
			return errors.New("הקובץ שהורד אינו תוכנה תקינה — העדכון בוטל")
		}
		return os.WriteFile(filepath.Join(dir, "TelegramPopup.new"), exe, 0o755)
	}
	return errors.New("קובץ העדכון לא מכיל את TelegramPopup.new")
}

// selfUpdateBat swaps the running exe for TelegramPopup.new and restarts.
// Written fresh on every use so old installs work too. Deletes itself.
const selfUpdateBat = `@echo off
cd /d "%~dp0"
timeout /t 1 >nul
taskkill /IM TelegramPopup.exe /F >nul 2>&1
timeout /t 2 >nul
if not exist TelegramPopup.new exit /b 1
if exist TelegramPopup.exe move /y TelegramPopup.exe TelegramPopup.old >nul
move /y TelegramPopup.new TelegramPopup.exe >nul
if exist TelegramPopup.exe (
  del TelegramPopup.old >nul 2>&1
) else (
  move /y TelegramPopup.old TelegramPopup.exe >nul
)
start "" TelegramPopup.exe
del "%~f0"
`

// applyUpdate launches the swap script and lets it kill this process. On
// non-Windows hosts it is a no-op (the cloud updates through Render).
func applyUpdate(dir string) error {
	if runtime.GOOS != "windows" {
		return errors.New("עדכון עצמי זמין רק בגרסת המחשב")
	}
	batPath := filepath.Join(dir, "SelfUpdate.bat")
	if err := os.WriteFile(batPath, []byte(selfUpdateBat), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", "start", "/min", "", batPath)
	cmd.Dir = dir
	hideWindow(cmd)
	return cmd.Start()
}
