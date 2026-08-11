//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func autostartLinkPath() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return ""
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "TelegramPopup.lnk")
}

// AutostartEnabled reports whether the startup shortcut exists.
func AutostartEnabled() bool {
	p := autostartLinkPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// SetAutostart creates or removes the startup shortcut for the current exe.
func SetAutostart(on bool) error {
	link := autostartLinkPath()
	if link == "" {
		return os.ErrNotExist
	}
	if !on {
		return os.Remove(link)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// A .lnk needs COM — one short hidden PowerShell call does it cleanly.
	script := "$s=(New-Object -COMObject WScript.Shell).CreateShortcut('" + link + "');" +
		"$s.TargetPath='" + exe + "';" +
		"$s.WorkingDirectory='" + filepath.Dir(exe) + "';" +
		"$s.Save()"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	return cmd.Run()
}
