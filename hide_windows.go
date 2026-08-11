//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow stops each PowerShell invocation from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
