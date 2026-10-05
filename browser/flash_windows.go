package main

import (
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procFindWindowW = user32.NewProc("FindWindowW")
	procFlashWindow = user32.NewProc("FlashWindowEx")
)

// flashWINFO mirrors FLASHWINFO.
type flashWINFO struct {
	size    uint32
	hwnd    uintptr
	flags   uint32
	count   uint32
	timeout uint32
}

const (
	flashwAll       = 0x3 // caption and taskbar button
	flashwTimerNoFG = 0xC // until the window comes to the foreground
)

// flashWindow flashes the app's taskbar button until the player switches to it.
func flashWindow(title string) {
	name, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(name)))
	if hwnd == 0 {
		return
	}
	info := flashWINFO{hwnd: hwnd, flags: flashwAll | flashwTimerNoFG}
	info.size = uint32(unsafe.Sizeof(info))
	procFlashWindow.Call(uintptr(unsafe.Pointer(&info)))
}

var (
	winmm         = syscall.NewLazyDLL("winmm.dll")
	procPlaySound = winmm.NewProc("PlaySoundW")
	chime         = chimeWAV() // kept alive: PlaySound reads it asynchronously
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndMemory    = 0x0004
)

// playChime plays the vote chime without waiting for it to finish.
func playChime() {
	procPlaySound.Call(uintptr(unsafe.Pointer(&chime[0])), 0, sndMemory|sndAsync|sndNoDefault)
}
