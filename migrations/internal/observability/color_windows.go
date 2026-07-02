//go:build windows

package observability

import (
	"os"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// prepareColorOutput enables ANSI escape-sequence processing for classic
// Windows consoles. Newer terminals usually already have it enabled.
func prepareColorOutput(file *os.File) bool {
	handle := syscall.Handle(file.Fd())
	var mode uint32

	ok, _, _ := getConsoleMode.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&mode)),
	)
	if ok == 0 {
		return false
	}

	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}

	ok, _, _ = setConsoleMode.Call(
		uintptr(handle),
		uintptr(mode|enableVirtualTerminalProcessing),
	)
	return ok != 0
}
