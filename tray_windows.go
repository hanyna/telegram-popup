//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// System tray icon, in raw Win32 — no external dependencies. A hidden window
// owns the icon; its message loop runs on a locked OS thread. Every entry
// point is wrapped in recover: if anything here misbehaves on some Windows
// build, the tray silently disappears and the app itself keeps running.
// ---------------------------------------------------------------------------

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassEx  = user32.NewProc("RegisterClassExW")
	pCreateWindowEx   = user32.NewProc("CreateWindowExW")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pGetMessage       = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenu       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pLoadImage        = user32.NewProc("LoadImageW")
	pLoadIcon         = user32.NewProc("LoadIconW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmApp         = 0x8000
	wmTrayMsg     = wmApp + 1
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	nimAdd        = 0
	nimDelete     = 2
	nifMessage    = 0x1
	nifIcon       = 0x2
	nifTip        = 0x4
	tpmReturnCmd  = 0x0100
	tpmRightAlign = 0x0008
	mfString      = 0x0
	mfSeparator   = 0x800
	mfChecked     = 0x8
	idiApp        = 32512

	cmdOpen   = 101
	cmdMute   = 102
	cmdExit   = 103
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	// Trailing fields of the modern struct, zeroed.
	DwState, DwStateMask uint32
	SzInfo               [256]uint16
	UVersion             uint32
	SzInfoTitle          [64]uint16
	DwInfoFlags          uint32
	GuidItem             [16]byte
	HBalloonIcon         uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type point struct{ X, Y int32 }

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

// TrayCallbacks is what the menu items do — wired by main.
type TrayCallbacks struct {
	OpenPage   func()
	PopupsOn   func() bool
	TogglePops func()
	Quit       func()
}

var trayCb TrayCallbacks
var trayHwnd uintptr

// StartTray launches the tray icon. Never panics outward.
func StartTray(dir string, cb TrayCallbacks) {
	trayCb = cb
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logLine("אייקון המגש לא עלה (%v) — התוכנה ממשיכה כרגיל", r)
			}
		}()
		runtime.LockOSThread()
		runTray(dir)
	}()
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func trayWndProc(hwnd uintptr, msgID uint32, wParam, lParam uintptr) uintptr {
	switch msgID {
	case wmTrayMsg:
		switch lParam {
		case wmLButtonUp:
			if trayCb.OpenPage != nil {
				trayCb.OpenPage()
			}
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		switch wParam & 0xffff {
		case cmdOpen:
			if trayCb.OpenPage != nil {
				trayCb.OpenPage()
			}
		case cmdMute:
			if trayCb.TogglePops != nil {
				trayCb.TogglePops()
			}
		case cmdExit:
			removeTrayIcon(hwnd)
			pPostQuitMessage.Call(0)
			if trayCb.Quit != nil {
				trayCb.Quit()
			}
		}
		return 0
	}
	ret, _, _ := pDefWindowProc.Call(hwnd, uintptr(msgID), wParam, lParam)
	return ret
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)

	pAppendMenu.Call(menu, mfString, cmdOpen, uintptr(unsafe.Pointer(utf16Ptr("פתח את דף הערוץ"))))
	muteFlags := uintptr(mfString)
	label := "השתק חלוניות קופצות"
	if trayCb.PopupsOn != nil && !trayCb.PopupsOn() {
		muteFlags |= mfChecked
		label = "חלוניות מושתקות — הפעל"
	}
	pAppendMenu.Call(menu, muteFlags, cmdMute, uintptr(unsafe.Pointer(utf16Ptr(label))))
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	pAppendMenu.Call(menu, mfString, cmdExit, uintptr(unsafe.Pointer(utf16Ptr("יציאה"))))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWnd.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	if cmd != 0 {
		trayWndProc(hwnd, wmCommand, cmd, 0)
	}
}

func loadTrayIcon(dir string) uintptr {
	// Prefer our generated icon file; fall back to the stock application icon.
	icoPath := filepath.Join(dir, "app.ico")
	if _, err := os.Stat(icoPath); err != nil {
		if data := buildIco(); data != nil {
			_ = os.WriteFile(icoPath, data, 0o644)
		}
	}
	const imageIcon = 1
	const lrLoadFromFile = 0x10
	if h, _, _ := pLoadImage.Call(0, uintptr(unsafe.Pointer(utf16Ptr(icoPath))),
		imageIcon, 32, 32, lrLoadFromFile); h != 0 {
		return h
	}
	h, _, _ := pLoadIcon.Call(0, uintptr(idiApp))
	return h
}

func runTray(dir string) {
	hInst, _, _ := pGetModuleHandle.Call(0)
	className := utf16Ptr("TgPopupTrayWnd")

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(trayWndProc),
		HInstance:     hInst,
		LpszClassName: className,
	}
	if atom, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		panic("RegisterClassEx failed")
	}

	hwnd, _, _ := pCreateWindowEx.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("Telegram Popup"))),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		panic("CreateWindowEx failed")
	}
	trayHwnd = hwnd

	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayMsg,
		HIcon:            loadTrayIcon(dir),
	}
	tip := syscall.StringToUTF16("ערוץ חי — לחיצה פותחת את הדף")
	copy(nid.SzTip[:], tip)
	if ok, _, _ := pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ok == 0 {
		panic("Shell_NotifyIcon failed")
	}
	logLine("אייקון המגש פעיל")

	var m msg
	for {
		ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func removeTrayIcon(hwnd uintptr) {
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   hwnd,
		UID:    1,
	}
	pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// buildIco draws the app icon (blue-green rounded square + white chat bubble)
// as a 32x32 32-bit ICO, entirely in code.
func buildIco() []byte {
	const size = 32
	pix := make([]byte, size*size*4) // BGRA, bottom-up rows

	inBubble := func(x, y int) bool {
		// Bubble body 6..26 x 8..20 (top-down coords), tail near bottom-right.
		if x >= 6 && x <= 26 && y >= 8 && y <= 20 {
			return true
		}
		if y > 20 && y <= 24 && x >= 8 && x <= 12 && (x-8) >= (y-21) {
			return true
		}
		return false
	}

	for ty := 0; ty < size; ty++ {
		for x := 0; x < size; x++ {
			// Rounded-square background.
			dx, dy := 0, 0
			if x < 4 {
				dx = 4 - x
			} else if x > size-5 {
				dx = x - (size - 5)
			}
			if ty < 4 {
				dy = 4 - ty
			} else if ty > size-5 {
				dy = ty - (size - 5)
			}
			var b, g, r, a byte
			if dx*dx+dy*dy <= 16 {
				// Vertical gradient: telegram-blue → green.
				t := float64(ty) / float64(size-1)
				r = byte(0x37 + t*(0x31-0x37))
				g = byte(0xae + t*(0xc4-0xae))
				b = byte(0xe2 + t*(0x8d-0xe2))
				a = 0xff
				if inBubble(x, ty) {
					r, g, b = 0xff, 0xff, 0xff
				}
			}
			row := size - 1 - ty // bottom-up
			off := (row*size + x) * 4
			pix[off], pix[off+1], pix[off+2], pix[off+3] = b, g, r, a
		}
	}

	// AND mask (all zero — alpha channel rules).
	mask := make([]byte, size*size/8)

	// BITMAPINFOHEADER: height doubled (XOR + AND).
	bih := make([]byte, 40)
	put32 := func(b []byte, off int, v uint32) {
		b[off] = byte(v)
		b[off+1] = byte(v >> 8)
		b[off+2] = byte(v >> 16)
		b[off+3] = byte(v >> 24)
	}
	put32(bih, 0, 40)
	put32(bih, 4, size)
	put32(bih, 8, size*2)
	bih[12] = 1              // planes
	bih[14] = 32             // bpp
	put32(bih, 20, uint32(len(pix)+len(mask)))

	img := append(append(bih, pix...), mask...)

	// ICONDIR + ICONDIRENTRY.
	out := make([]byte, 6+16)
	out[2] = 1 // type: icon
	out[4] = 1 // count
	out[6] = size
	out[7] = size
	out[10] = 1  // planes
	out[12] = 32 // bpp
	put32(out, 14, uint32(len(img)))
	put32(out, 18, uint32(len(out)))
	return append(out, img...)
}
