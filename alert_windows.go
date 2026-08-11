//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// alert shows a native message box — the app has no console window, so this
// is how setup problems reach the user.
func alert(title, text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	msgBox := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	const mbIconInformation = 0x40
	const mbTopMost = 0x40000
	_, _, _ = msgBox.Call(0,
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(c)),
		uintptr(mbIconInformation|mbTopMost))
}
