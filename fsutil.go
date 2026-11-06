package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// writeFileAtomic writes data to a temp file next to path, flushes it to
// disk and renames it into place. A crash, a `taskkill /F` from the updater
// or a power cut mid-write can no longer leave config.json / state.json
// empty or half-written (which made the app refuse to start).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, perm)
	if err := os.Rename(tmpName, path); err != nil {
		// Windows cannot rename over a file another process holds open;
		// retry via remove+rename, and fall back to a plain write.
		if rmErr := os.Remove(path); rmErr == nil || os.IsNotExist(rmErr) {
			if err2 := os.Rename(tmpName, path); err2 == nil {
				return nil
			}
		}
		_ = os.Remove(tmpName)
		return os.WriteFile(path, data, perm)
	}
	return nil
}

// isAddrInUse reports whether a listen error means the port is taken — by
// error code, not by English text, so it also works on a Hebrew Windows
// where the message is localised (the old string match missed it and the
// second copy kept running, showing duplicate popups).
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			var errno syscall.Errno
			if errors.As(sysErr.Err, &errno) {
				// 10048 = WSAEADDRINUSE on Windows; EADDRINUSE elsewhere.
				return errno == syscall.EADDRINUSE || uintptr(errno) == 10048
			}
		}
	}
	return false
}

// rotateLog keeps the previous run's log as app.prev.log so the evidence
// of a crash survives the next start (os.Create used to wipe it).
func rotateLog(dir string) {
	cur := filepath.Join(dir, "app.log")
	prev := filepath.Join(dir, "app.prev.log")
	if st, err := os.Stat(cur); err == nil && st.Size() > 0 {
		_ = os.Remove(prev)
		_ = os.Rename(cur, prev)
	}
}
