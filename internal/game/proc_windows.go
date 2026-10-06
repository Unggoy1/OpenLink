package game

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// setNewConsole gives the server its own console window instead of sharing ours.
func setNewConsole(cmd *exec.Cmd) {
	const createNewConsole = 0x00000010
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}

var (
	procOpenProcess                = syscall.NewLazyDLL("kernel32.dll").NewProc("OpenProcess")
	procQueryFullProcessImageNameW = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
)

// processImage returns a process's executable path, or "" if unreadable.
func processImage(pid int) string {
	const processQueryLimitedInformation = 0x1000
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}
