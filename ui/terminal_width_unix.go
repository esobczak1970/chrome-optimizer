//go:build darwin || linux || freebsd

package ui

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// terminalColumns intentionally ignores environment variables for redirected
// output: CI logs and shell pipelines must not be silently truncated.
func terminalColumns(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	var size struct{ rows, cols, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 || size.cols == 0 {
		return 0
	}
	return int(size.cols)
}
