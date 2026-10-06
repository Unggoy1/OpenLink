package main

import (
	"syscall"
	"unsafe"
)

// textWrap draws s in r on up to as many lines as fit, breaking at words and
// ending the last line with "…" if text is left over.
func (g canvas) textWrap(s string, r rect, font uintptr, col uint32) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	old, _, _ := procSelectObject.Call(g.dc, font)
	procSetTextColor.Call(g.dc, uintptr(col))
	procDrawTextW.Call(g.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)),
		dtWordBreak|dtEditControl|dtEndEllipsis|dtNoPrefix)
	procSelectObject.Call(g.dc, old)
}

// textLines is how many lines s needs at the given width, word-wrapped.
func (o *voteOverlay) textLines(hwnd uintptr, s string, width int32, font uintptr) int {
	u, err := syscall.UTF16FromString(s)
	if err != nil || len(u) < 2 || width <= 0 {
		return 1
	}
	dc, _, _ := procGetDC.Call(hwnd)
	defer procReleaseDC.Call(hwnd, dc)
	old, _, _ := procSelectObject.Call(dc, font)
	defer procSelectObject.Call(dc, old)
	one, all := rect{0, 0, width, 0}, rect{0, 0, width, 0}
	m := []uint16{'A', 'g', 0}
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&m[0])), 2, uintptr(unsafe.Pointer(&one)), dtCalcRect|dtSingleLine|dtNoPrefix)
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&all)), dtCalcRect|dtWordBreak|dtNoPrefix)
	if one.bottom <= 0 {
		return 1
	}
	return int((all.bottom + one.bottom - 1) / one.bottom)
}

// rowHeight is the option row height for this ballot: taller when any name
// needs a second line at panel width w.
func (o *voteOverlay) rowHeight(hwnd uintptr, b *BallotView, w int32) int32 {
	nameW := w - o.px(12)*2 - o.px(3) - o.px(96) - o.px(10) - o.px(10)
	for _, opt := range b.Options {
		if o.textLines(hwnd, opt.Name, nameW, o.fontName) > 1 {
			return o.px(rowTall)
		}
	}
	return o.px(rowShort)
}
