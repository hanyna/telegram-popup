//go:build !windows

package main

// TrayCallbacks mirrors the Windows tray contract on other platforms.
type TrayCallbacks struct {
	OpenPage   func()
	PopupsOn   func() bool
	TogglePops func()
	Quit       func()
}

// StartTray is a no-op outside Windows.
func StartTray(dir string, cb TrayCallbacks) {}
