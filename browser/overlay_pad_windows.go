package main

import (
	"syscall"
	"time"
	"unsafe"
)

// loadXInput finds XInput: xinput1_4 (Windows 8 and later), else the
// redistributable-free xinput9_1_0. Nil if neither loads.
func loadXInput() *syscall.LazyProc {
	for _, name := range []string{"xinput1_4.dll", "xinput9_1_0.dll"} {
		p := syscall.NewLazyDLL(name).NewProc("XInputGetState")
		if p.Find() == nil {
			return p
		}
	}
	return nil
}

// padButtons returns the buttons held on controller slot i, or false when no
// controller is connected there.
func padButtons(i int) (uint16, bool) {
	if xinput == nil {
		return 0, false
	}
	var state [16]byte // XINPUT_STATE: packet number, then XINPUT_GAMEPAD (buttons first)
	if r, _, _ := xinput.Call(uintptr(i), uintptr(unsafe.Pointer(&state[0]))); r != 0 {
		return 0, false
	}
	return uint16(state[4]) | uint16(state[5])<<8, true
}

// setPadTimer starts or stops polling controllers (window thread).
func (o *voteOverlay) setPadTimer(hwnd uintptr, on bool) {
	on = on && xinput != nil
	if on == o.padTimer {
		return
	}
	o.padTimer = on
	if on {
		procSetTimer.Call(hwnd, padTimerID, 33, 0)
		return
	}
	procKillTimer.Call(hwnd, padTimerID)
	o.padPrev = [4]uint16{}
}

// pollPads votes on View + D-pad from any controller while Halo (or the open
// panel) is in front. Empty slots are checked once a second, since asking an
// empty slot is slow.
func (o *voteOverlay) pollPads(hwnd uintptr) {
	b, _ := o.snapshot()
	now := time.Now()
	for i := range o.padPrev {
		if now.Before(o.padNext[i]) {
			continue // empty slot, checked again later
		}
		buttons, ok := padButtons(i)
		if o.padConn[i] = ok; !ok {
			o.padPrev[i], o.padNext[i] = 0, now.Add(time.Second)
			continue
		}
		prev := o.padPrev[i]
		o.padPrev[i] = buttons
		if b == nil || b.Closed {
			continue
		}
		if c := padChoice(prev, buttons, len(b.Options)); c >= 0 && (o.open || o.gameInFront(hwnd)) {
			o.choose(hwnd, c)
		}
	}
	seen := false
	for _, c := range o.padConn {
		seen = seen || c
	}
	if seen != o.padSeen {
		o.padSeen = seen
		o.refresh(hwnd)
	}
}
