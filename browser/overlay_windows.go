//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// The vote overlay is a small always-on-top window of our own, shown over the
// game while a playlist vote is open. Nothing is injected into Halo: like
// Discord's current overlay it relies on the game running borderless, which
// is the only full-screen mode Halo Infinite has. So it does not matter
// whether OpenLink or the game starts first.
//
// Passive: the panel shows itself, never takes focus or the mouse, and each
// choice has a global hotkey. Interactive: a small hint shows itself; the
// open hotkey brings up the panel with focus (keys 1-4, arrows, Enter, mouse;
// Esc returns to the game). Hotkeys are held only while a vote is open.
//
// The window and its hotkeys live on one locked OS thread, started the first
// time a vote opens with the overlay enabled. It draws with GDI: no second
// WebView.

var (
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	xinput   = loadXInput()

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procRegisterHotKey             = user32.NewProc("RegisterHotKey")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procUnregisterHotKey           = user32.NewProc("UnregisterHotKey")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procFillRect                   = user32.NewProc("FillRect")
	procDrawTextW                  = user32.NewProc("DrawTextW")
	procGetClientRect              = user32.NewProc("GetClientRect")
	procGetDC                      = user32.NewProc("GetDC")
	procReleaseDC                  = user32.NewProc("ReleaseDC")
	procMonitorFromWindow          = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procIsWindow                   = user32.NewProc("IsWindow")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procRoundRect              = gdi32.NewProc("RoundRect")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procStretchDIBits          = gdi32.NewProc("StretchDIBits")
	procSetStretchBltMode      = gdi32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx          = gdi32.NewProc("SetBrushOrgEx")
	procGetTextExtentPoint32W  = gdi32.NewProc("GetTextExtentPoint32W")
	procGetStockObject         = gdi32.NewProc("GetStockObject")

	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")

	procGetDpiForMonitor      = shcore.NewProc("GetDpiForMonitor")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wsPopup         = 0x80000000
	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000
	gwlExStyle      = ^uintptr(19) // -20
	hwndTopmost     = ^uintptr(0)  // -1

	swHide          = 0
	swpNoActivate   = 0x0010
	swpShowWindow   = 0x0040
	lwaAlpha        = 0x2
	modNoRepeat     = 0x4000
	maNoActivate    = 3
	waInactive      = 0
	idcArrow        = 32512
	nullPen         = 8
	psInsideFrame   = 6
	bkTransparent   = 1
	srcCopy         = 0x00CC0020
	stretchHalftone = 4
	dibRGBColors    = 0
	cleartype       = 5

	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtNoPrefix    = 0x0800
	dtEndEllipsis = 0x8000
	dtWordBreak   = 0x0010
	dtCalcRect    = 0x0400
	dtEditControl = 0x2000 // with dtWordBreak: no partly visible last line

	maxThumbSide = 4096 // larger map thumbnails are not decoded

	panelWidth = 400 // DIP (overlay_scale.go)
	rowShort   = 60  // DIP: option row with a one-line name
	rowTall    = 78  // DIP: option row when a name needs two lines

	wmDestroy       = 0x0002
	wmActivate      = 0x0006
	wmClose         = 0x0010
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmMouseActivate = 0x0021
	wmKeyDown       = 0x0100
	wmHotkey        = 0x0312
	wmTimer         = 0x0113
	wmLButtonDown   = 0x0201
	wmOverlayUpdate = 0x8000 + 1 // WM_APP+1: state changed, re-evaluate

	vkReturn  = 0x0D
	vkEscape  = 0x1B
	vkUp      = 0x26
	vkDown    = 0x28
	vkNumpad0 = 0x60

	processQueryLimited = 0x1000
	monitorToPrimary    = 1
	dwmCornerPreference = 33
	dwmCornerRound      = 2

	hotkeyOpenID = 100 // interactive open key; vote keys are 1..MaxOptions
	padTimerID   = 200 // controller polling while a vote is open

	// defaultOverlayMode is the mode of a new install (user decision 2026-10-05).
	defaultOverlayMode = overlayPassive

	gameExe        = "HaloInfinite.exe"
	resultLinger   = 8 * time.Second // the result stays up this long after the vote closes
	overlayAlpha   = 250             // whole-window opacity, of 255
	thumbW, thumbH = 192, 108        // stored thumbnail size (drawn at 96x54 DIP)
)

// Colours (0x00BBGGRR), from frontend/src/style.css and VotePanel.svelte.
var (
	colBg     = rgb(0x23, 0x2f, 0x32) // vote panel background
	colRow    = rgb(0x1b, 0x21, 0x22) // --panel
	colRowSel = rgb(0x2a, 0x3a, 0x3e)
	colLine   = rgb(0x33, 0x39, 0x3a)
	colText   = rgb(0xde, 0xe3, 0xe5)
	colMuted  = rgb(0xbf, 0xc8, 0xcb)
	colFaint  = rgb(0x7c, 0x84, 0x86)
	colAccent = rgb(0xce, 0xe7, 0xee)
	colMeter  = rgb(0x33, 0x4a, 0x50) // --button-bg
	colOK     = rgb(0x3d, 0xdc, 0x84)
	colWarn   = rgb(0xff, 0xb5, 0x47)
)

func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

type rect struct{ left, top, right, bottom int32 }

type point struct{ x, y int32 }

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type paintStruct struct {
	hdc         uintptr
	erase       int32
	paint       rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}

type monitorInfo struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
}

type bitmapInfo struct {
	size          uint32
	width, height int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPPM, yPPM    int32
	clrUsed       uint32
	clrImportant  uint32
	colors        [4]byte
}

type textSize struct{ cx, cy int32 }

// thumbImage is a map thumbnail as a top-down 32-bit BGRA bitmap.
type thumbImage struct {
	w, h int
	bits []byte
}

type hotkeyReg struct {
	id  int
	key string
}

type voteOverlay struct {
	vote func(round uint64, choice int) error

	mu         sync.Mutex
	hwnd       uintptr // 0 until the window thread runs
	failed     bool    // the window could not be created; don't retry every tick
	ballot     *BallotView
	cfg        Settings
	pendRound  uint64 // a vote sent from the overlay, shown until the host confirms it
	pendChoice int
	voteErr    string
	errRound   uint64 // the round voteErr belongs to
	keyErr     string
	registered []string               // hotkeys held now
	thumbs     map[string]*thumbImage // by first thumbnail URL; nil = none
	fetching   map[string]bool

	// Window thread only.
	held        []hotkeyReg
	wantKeys    []hotkeyReg
	visible     bool
	open        bool    // interactive panel shown with focus
	prevFg      uintptr // where focus returns when the panel closes
	sel         int
	closedRound uint64
	closedAt    time.Time
	fgHwnd      uintptr
	fgGame      bool
	scale       int // device pixels per 1000 DIP (overlayScale), 0 before the first placement
	fontTitle   uintptr
	fontName    uintptr
	fontSmall   uintptr
	rows        []rect       // option rows from the last paint, for clicks
	rowH        int32        // option row height in pixels, set by place
	padTimer    bool         // controller polling runs
	padSeen     bool         // a controller is connected (labels and full panel)
	padPrev     [4]uint16    // last buttons per controller slot
	padConn     [4]bool      // a controller answered in this slot
	padNext     [4]time.Time // when to look at an empty slot again
}

var (
	theOverlay  *voteOverlay
	overlayProc = syscall.NewCallback(overlayWndProc)
)

func newVoteOverlay(vote func(round uint64, choice int) error) *voteOverlay {
	theOverlay = &voteOverlay{vote: vote, pendChoice: -1, thumbs: map[string]*thumbImage{}, fetching: map[string]bool{}}
	return theOverlay
}

// update is called by watchVotes about twice a second with the current ballot.
func (o *voteOverlay) update(b *BallotView, s Settings) {
	o.mu.Lock()
	o.ballot, o.cfg = b, s
	if b != nil && o.pendRound != 0 && (b.Round != o.pendRound || b.Mine == o.pendChoice) {
		o.pendRound, o.pendChoice = 0, -1 // confirmed by the host, or a new round
	}
	if b != nil && b.Round != o.errRound {
		o.voteErr = ""
	}
	hwnd := o.hwnd
	start := hwnd == 0 && !o.failed && b != nil && s.OverlayMode != overlayOff
	o.mu.Unlock()
	if start {
		hwnd = o.start()
	}
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmOverlayUpdate, 0, 0)
	}
	if b != nil && s.OverlayMode != overlayOff {
		o.fetchThumbs(b)
	}
}

func (o *voteOverlay) close() {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
}

// conflicts tries each hotkey and reports the ones another app holds.
func (o *voteOverlay) conflicts(keys []string) []string {
	o.mu.Lock()
	held := slices.Clone(o.registered)
	o.mu.Unlock()
	runtime.LockOSThread() // a thread hotkey must be released by the same thread
	defer runtime.UnlockOSThread()
	var taken []string
	for i, k := range keys {
		h, norm, err := parseHotkey(k)
		if err != nil || slices.Contains(held, norm) {
			continue
		}
		id := uintptr(0xB000 + i)
		if r, _, _ := procRegisterHotKey.Call(0, id, uintptr(h.mods|modNoRepeat), uintptr(h.vk)); r == 0 {
			taken = append(taken, k)
		} else {
			procUnregisterHotKey.Call(0, id)
		}
	}
	return taken
}

// start runs the window thread and returns its window, or 0.
func (o *voteOverlay) start() uintptr {
	ready := make(chan uintptr)
	go func() {
		runtime.LockOSThread() // the window and its hotkeys belong to this thread
		hwnd := createOverlayWindow()
		o.mu.Lock()
		o.hwnd, o.failed = hwnd, hwnd == 0
		o.mu.Unlock()
		ready <- hwnd
		if hwnd == 0 {
			return
		}
		var m winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		o.mu.Lock()
		o.hwnd = 0
		o.mu.Unlock()
	}()
	return <-ready
}

func createOverlayWindow() uintptr {
	inst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	cls, _ := syscall.UTF16PtrFromString("OpenLinkVoteOverlay")
	title, _ := syscall.UTF16PtrFromString("OpenLink vote")
	wc := wndClassEx{wndProc: overlayProc, instance: inst, cursor: cursor, className: cls}
	wc.size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))) // fails harmlessly if already registered
	hwnd, _, _ := procCreateWindowExW.Call(passiveExStyle, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		wsPopup, 0, 0, 1, 1, 0, 0, inst, 0)
	if hwnd == 0 {
		return 0
	}
	procSetLayeredWindowAttributes.Call(hwnd, 0, overlayAlpha, lwaAlpha)
	corner := uint32(dwmCornerRound)
	procDwmSetWindowAttribute.Call(hwnd, dwmCornerPreference, uintptr(unsafe.Pointer(&corner)), 4)
	return hwnd
}

// Not activated by clicks, clicks pass through to the game, no taskbar button.
const passiveExStyle = wsExTopmost | wsExToolWindow | wsExLayered | wsExNoActivate | wsExTransparent

func overlayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	o := theOverlay
	switch msg {
	case wmOverlayUpdate:
		o.refresh(hwnd)
		return 0
	case wmPaint:
		o.paint(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmHotkey:
		o.hotkey(hwnd, int(wParam))
		return 0
	case wmTimer:
		if wParam == padTimerID {
			o.pollPads(hwnd)
		}
		return 0
	case wmMouseActivate:
		if !o.open {
			return maNoActivate
		}
	case wmActivate:
		if wParam&0xFFFF == waInactive && o.open {
			o.closePanel(hwnd, false) // the player clicked or tabbed elsewhere
		}
	case wmKeyDown:
		if o.open {
			o.key(hwnd, wParam)
			return 0
		}
	case wmLButtonDown:
		if o.open {
			x, y := int32(int16(lParam&0xFFFF)), int32(int16(lParam>>16&0xFFFF))
			for i, r := range o.rows {
				if x >= r.left && x < r.right && y >= r.top && y < r.bottom {
					o.choose(hwnd, i)
				}
			}
			return 0
		}
	case wmDestroy:
		o.setKeys(hwnd, nil)
		o.setPadTimer(hwnd, false)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func (o *voteOverlay) snapshot() (*BallotView, Settings) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ballot, o.cfg
}

// refresh decides what to show and which hotkeys to hold.
func (o *voteOverlay) refresh(hwnd uintptr) {
	b, cfg := o.snapshot()
	if b == nil || cfg.OverlayMode == overlayOff || len(b.Options) == 0 {
		o.closePanel(hwnd, false)
		o.setKeys(hwnd, nil)
		o.setPadTimer(hwnd, false)
		o.hide(hwnd)
		return
	}
	var want []hotkeyReg
	if !b.Closed {
		if cfg.OverlayMode == overlayPassive {
			for i := range b.Options {
				want = append(want, hotkeyReg{i + 1, cfg.OverlayVoteKeys[i]})
			}
		} else {
			want = []hotkeyReg{{hotkeyOpenID, cfg.OverlayOpenKey}}
		}
	}
	o.setKeys(hwnd, want)
	o.setPadTimer(hwnd, !b.Closed && !cfg.OverlayNoController)

	if b.Closed {
		if o.closedRound != b.Round {
			o.closedRound, o.closedAt = b.Round, time.Now()
		}
		if o.open {
			o.closePanel(hwnd, true)
		}
		if time.Since(o.closedAt) > resultLinger {
			o.hide(hwnd)
			return
		}
	}
	if !o.open && !o.gameInFront(hwnd) {
		o.hide(hwnd)
		return
	}
	o.place(hwnd, b, cfg)
}

// gameInFront reports whether the foreground window is Halo (in the demo
// build: any window but OpenLink's own).
func (o *voteOverlay) gameInFront(hwnd uintptr) bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == hwnd {
		return true
	}
	if fg != o.fgHwnd {
		o.fgHwnd, o.fgGame = fg, isGameWindow(fg)
	}
	return o.fgGame
}

func isGameWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if demo != nil {
		return pid != 0 && int(pid) != os.Getpid()
	}
	h, _, _ := procOpenProcess.Call(processQueryLimited, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return false
	}
	return strings.EqualFold(filepath.Base(syscall.UTF16ToString(buf[:n])), gameExe)
}

func (o *voteOverlay) hide(hwnd uintptr) {
	if o.visible {
		procShowWindow.Call(hwnd, swHide)
		o.visible = false
	}
}

// setKeys holds exactly the wanted hotkeys, noting any another app has taken.
func (o *voteOverlay) setKeys(hwnd uintptr, want []hotkeyReg) {
	if slices.Equal(want, o.wantKeys) {
		return
	}
	o.wantKeys = want
	for _, k := range o.held {
		procUnregisterHotKey.Call(hwnd, uintptr(k.id))
	}
	o.held = nil
	var names, taken []string
	for _, k := range want {
		h, norm, err := parseHotkey(k.key)
		if err != nil {
			continue
		}
		if r, _, _ := procRegisterHotKey.Call(hwnd, uintptr(k.id), uintptr(h.mods|modNoRepeat), uintptr(h.vk)); r == 0 {
			taken = append(taken, norm)
			continue
		}
		o.held = append(o.held, k)
		names = append(names, norm)
	}
	o.mu.Lock()
	o.registered = names
	o.keyErr = ""
	if len(taken) > 0 {
		o.keyErr = strings.Join(taken, ", ") + " is in use by another app; change it in OpenLink settings"
	}
	o.mu.Unlock()
}

func (o *voteOverlay) hotkey(hwnd uintptr, id int) {
	b, cfg := o.snapshot()
	if b == nil || b.Closed {
		return
	}
	switch {
	case id == hotkeyOpenID && cfg.OverlayMode == overlayInteractive:
		if o.open {
			o.closePanel(hwnd, true)
		} else {
			o.openPanel(hwnd)
		}
	case id >= 1 && id <= len(b.Options) && cfg.OverlayMode == overlayPassive:
		o.choose(hwnd, id-1)
	}
}

func (o *voteOverlay) openPanel(hwnd uintptr) {
	fg, _, _ := procGetForegroundWindow.Call()
	o.prevFg, o.open = fg, true
	o.sel = max(0, o.mine())
	setExStyle(hwnd, wsExTopmost|wsExToolWindow|wsExLayered)
	o.refresh(hwnd)
	// A hotkey press lets this process take the foreground.
	procSetForegroundWindow.Call(hwnd)
}

func (o *voteOverlay) closePanel(hwnd uintptr, refocus bool) {
	if !o.open {
		return
	}
	o.open = false
	setExStyle(hwnd, passiveExStyle)
	if prev := o.prevFg; refocus && prev != 0 {
		if ok, _, _ := procIsWindow.Call(prev); ok != 0 {
			procSetForegroundWindow.Call(prev)
		}
	}
	o.prevFg = 0
	o.refresh(hwnd)
}

func setExStyle(hwnd, style uintptr) {
	procSetWindowLongPtrW.Call(hwnd, gwlExStyle, style)
}

func (o *voteOverlay) key(hwnd, vk uintptr) {
	b, _ := o.snapshot()
	if b == nil {
		return
	}
	n := len(b.Options)
	switch {
	case vk == vkEscape:
		o.closePanel(hwnd, true)
	case vk >= '1' && vk < '1'+uintptr(n):
		o.choose(hwnd, int(vk-'1'))
	case vk >= vkNumpad0+1 && vk < vkNumpad0+1+uintptr(n):
		o.choose(hwnd, int(vk-vkNumpad0-1))
	case vk == vkUp:
		o.sel = (o.sel + n - 1) % n
		procInvalidateRect.Call(hwnd, 0, 0)
	case vk == vkDown:
		o.sel = (o.sel + 1) % n
		procInvalidateRect.Call(hwnd, 0, 0)
	case vk == vkReturn:
		o.choose(hwnd, o.sel)
	}
}

// choose sends a vote. In the interactive panel it also returns to the game.
func (o *voteOverlay) choose(hwnd uintptr, i int) {
	b, _ := o.snapshot()
	if b == nil || b.Closed || i < 0 || i >= len(b.Options) {
		return
	}
	o.mu.Lock()
	o.pendRound, o.pendChoice, o.voteErr = b.Round, i, ""
	o.mu.Unlock()
	go func() {
		if err := o.vote(b.Round, i); err != nil {
			o.mu.Lock()
			o.pendRound, o.pendChoice, o.voteErr, o.errRound = 0, -1, err.Error(), b.Round
			o.mu.Unlock()
			procPostMessageW.Call(hwnd, wmOverlayUpdate, 0, 0)
		}
	}()
	if o.open {
		o.closePanel(hwnd, true)
	} else {
		procInvalidateRect.Call(hwnd, 0, 0)
	}
}

// mine is the player's choice: an unconfirmed overlay vote, else the host's.
func (o *voteOverlay) mine() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ballot == nil {
		return -1
	}
	if o.pendRound == o.ballot.Round && o.pendChoice >= 0 {
		return o.pendChoice
	}
	return o.ballot.Mine
}

// ---- layout and drawing (window thread) ----

// px converts a design size to device pixels for the current monitor (overlayScale).
func (o *voteOverlay) px(v int) int32 { return int32(v * o.scale / 1000) }

// compact is the one-line hint used by the interactive mode until opened.
func (o *voteOverlay) compact(cfg Settings) bool {
	return cfg.OverlayMode == overlayInteractive && !o.open && !(o.padSeen && !cfg.OverlayNoController)
}

// place sizes and positions the window on the game's monitor and shows it.
func (o *voteOverlay) place(hwnd uintptr, b *BallotView, cfg Settings) {
	target := hwnd
	if !o.open {
		target, _, _ = procGetForegroundWindow.Call()
	}
	mon, _, _ := procMonitorFromWindow.Call(target, monitorToPrimary)
	mi := monitorInfo{}
	mi.size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	var dx, dy uint32
	if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r != 0 || dx == 0 {
		dx = 96
	}
	if s := overlayScale(mi.monitor.bottom-mi.monitor.top, dx); s != o.scale {
		o.setScale(s)
	}

	var w, h int32
	if o.compact(cfg) {
		w = min(o.textWidth(hwnd, o.hintText(b, cfg), o.fontName)+o.px(30), o.px(640))
		h = o.px(38)
	} else {
		w = o.px(panelWidth)
		o.rowH = o.rowHeight(hwnd, b, w)
		h = o.px(12+26+8) + int32(len(b.Options))*(o.rowH+o.px(6)) + o.px(12)
		if o.footer(b, cfg) != "" {
			h += o.px(22)
		}
	}
	m := mi.monitor
	margin := o.px(24)
	x, y := m.right-margin-w, m.top+margin
	if strings.HasSuffix(cfg.OverlayCorner, "left") {
		x = m.left + margin
	}
	if strings.HasPrefix(cfg.OverlayCorner, "bottom") {
		y = m.bottom - margin - h
	}
	flags := uintptr(swpNoActivate | swpShowWindow)
	procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(h), flags)
	o.visible = true
	procInvalidateRect.Call(hwnd, 0, 0)
}

func (o *voteOverlay) setScale(scale int) {
	for _, f := range []uintptr{o.fontTitle, o.fontName, o.fontSmall} {
		if f != 0 {
			procDeleteObject.Call(f)
		}
	}
	o.scale = scale
	o.fontTitle = o.font(15, 600)
	o.fontName = o.font(14, 600)
	o.fontSmall = o.font(12, 400)
}

func (o *voteOverlay) font(size, weight int) uintptr {
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	f, _, _ := procCreateFontW.Call(uintptr(-o.px(size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0,
		cleartype, 0, uintptr(unsafe.Pointer(face)))
	return f
}

func (o *voteOverlay) textWidth(hwnd uintptr, s string, font uintptr) int32 {
	dc, _, _ := procGetDC.Call(hwnd)
	defer procReleaseDC.Call(hwnd, dc)
	old, _, _ := procSelectObject.Call(dc, font)
	defer procSelectObject.Call(dc, old)
	u, _ := syscall.UTF16FromString(s)
	var sz textSize
	procGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	return sz.cx
}

func secs(s float64) string { return strconv.Itoa(int(max(0, s+0.999))) + " s" }

func (o *voteOverlay) header(b *BallotView) (string, string) {
	if b.Closed {
		name := ""
		if b.Winner >= 0 && b.Winner < len(b.Options) {
			name = b.Options[b.Winner].Name
		}
		when := "starting soon"
		if b.StartsIn >= 0 {
			when = "starting in " + secs(b.StartsIn)
		}
		return "Next: " + name, when
	}
	return "Vote for the next match", secs(b.Remaining) + " left"
}

func (o *voteOverlay) hintText(b *BallotView, cfg Settings) string {
	title, timer := o.header(b)
	if b.Closed {
		return title + " · " + timer
	}
	if m := o.mine(); m >= 0 && m < len(b.Options) {
		return "Voted: " + b.Options[m].Name + " · " + cfg.OverlayOpenKey + " to change · " + timer
	}
	return title + " · press " + cfg.OverlayOpenKey + " · " + timer
}

// footer is the line under the choices, or "".
func (o *voteOverlay) footer(b *BallotView, cfg Settings) string {
	o.mu.Lock()
	keyErr, voteErr := o.keyErr, o.voteErr
	o.mu.Unlock()
	switch {
	case voteErr != "":
		return "Vote not sent: " + voteErr
	case keyErr != "":
		return keyErr
	case b.Closed:
		return ""
	case o.open:
		return fmt.Sprintf("1–%d or click to vote · Esc to go back", len(b.Options))
	case o.mine() < 0:
		return "No vote? The server picks one of these at random."
	}
	return ""
}

func (o *voteOverlay) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var cr rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	w, h := cr.right, cr.bottom
	if w <= 0 || h <= 0 {
		return
	}
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	oldBmp, _, _ := procSelectObject.Call(mem, bmp)
	procSetBkMode.Call(mem, bkTransparent)
	g := canvas{dc: mem}

	g.fill(cr, colBg)
	if b, cfg := o.snapshot(); b != nil && o.scale != 0 {
		if o.compact(cfg) {
			col := colText
			if b.Closed {
				col = colOK
			}
			g.text(o.hintText(b, cfg), rect{o.px(14), 0, w - o.px(14), h}, o.fontName, col, dtVCenter|dtEndEllipsis)
		} else {
			o.paintPanel(g, b, cfg, w)
		}
	}

	procBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, srcCopy)
	procSelectObject.Call(mem, oldBmp)
	procDeleteObject.Call(bmp)
	procDeleteDC.Call(mem)
}

func (o *voteOverlay) paintPanel(g canvas, b *BallotView, cfg Settings, w int32) {
	pad := o.px(12)
	title, timer := o.header(b)
	head := rect{pad, pad, w - pad, pad + o.px(26)}
	timerW := o.textWidth(0, timer, o.fontSmall) + o.px(10)
	g.text(title, rect{head.left, head.top, head.right - timerW, head.bottom}, o.fontTitle, colText, dtVCenter|dtEndEllipsis)
	g.text(timer, rect{head.right - timerW, head.top, head.right, head.bottom}, o.fontSmall, colAccent, dtVCenter|dtRight)

	o.mu.Lock()
	thumbs := make([]*thumbImage, len(b.Options))
	for i, opt := range b.Options {
		if len(opt.Thumbs) > 0 {
			thumbs[i] = o.thumbs[opt.Thumbs[0]]
		}
	}
	o.mu.Unlock()

	total := 0
	for _, opt := range b.Options {
		total += opt.Votes
	}
	mine := o.mine()
	o.rows = o.rows[:0]
	y := head.bottom + o.px(8)
	for i, opt := range b.Options {
		row := rect{pad, y, w - pad, y + o.rowH}
		o.rows = append(o.rows, row)
		y += o.rowH + o.px(6)

		fill, border, nameCol := colRow, colLine, colText
		if o.open && i == o.sel {
			fill = colRowSel
		}
		switch {
		case b.Closed && i == b.Winner:
			border = colOK
		case b.Closed:
			nameCol = colFaint
		case i == mine:
			border = colAccent
		}
		g.roundRect(row, fill, border, o.px(2), o.px(8))

		inset := o.px(3)
		// Full row height; in a two-line row the image is stretched taller (user choice).
		th := rect{row.left + inset, row.top + inset, row.left + inset + o.px(96), row.bottom - inset}
		g.fill(th, colBg)
		if t := thumbs[i]; t != nil {
			g.image(t, th)
		}
		if total > 0 && opt.Votes > 0 {
			mw := (row.right - row.left - 2*inset) * int32(opt.Votes) / int32(total)
			g.fill(rect{row.left + inset, row.bottom - inset - o.px(3), row.left + inset + mw, row.bottom - inset}, colMeter)
		}

		tx := th.right + o.px(10)
		mid := row.top + (row.bottom-row.top)/2
		if o.rowH > o.px(rowShort) {
			// Two lines for the name; the key and vote line keeps the bottom 26 DIP.
			mid = row.bottom - o.px(28)
			g.textWrap(opt.Name, rect{tx, row.top + o.px(5), row.right - o.px(10), mid + o.px(2)}, o.fontName, nameCol)
		} else {
			g.text(opt.Name, rect{tx, row.top + o.px(6), row.right - o.px(10), mid + o.px(2)}, o.fontName, nameCol, dtVCenter|dtEndEllipsis)
		}
		votes := strconv.Itoa(opt.Votes) + " votes"
		if opt.Votes == 1 {
			votes = "1 vote"
		}
		keyLabel := ""
		if !b.Closed {
			if o.open {
				keyLabel = strconv.Itoa(i + 1)
			} else if cfg.OverlayMode == overlayPassive {
				keyLabel = cfg.OverlayVoteKeys[i]
			}
			if o.padSeen && !cfg.OverlayNoController && i < len(padLabels) {
				if keyLabel != "" {
					keyLabel += " · "
				}
				keyLabel += padLabels[i]
			}
		}
		info := rect{tx, mid + o.px(2), row.right - o.px(10), row.bottom - o.px(8)}
		if keyLabel != "" {
			g.text(keyLabel, info, o.fontSmall, colAccent, dtVCenter|dtEndEllipsis)
			info.left += o.textWidth(0, keyLabel, o.fontSmall) + o.px(10)
		}
		g.text(votes, info, o.fontSmall, colMuted, dtVCenter|dtEndEllipsis)
	}

	if f := o.footer(b, cfg); f != "" {
		col := colMuted
		if strings.Contains(f, "in use") || strings.HasPrefix(f, "Vote not sent") {
			col = colWarn
		}
		g.text(f, rect{pad, y, w - pad, y + o.px(22)}, o.fontSmall, col, dtVCenter|dtEndEllipsis)
	}
}

// canvas wraps the GDI calls used for drawing.
type canvas struct{ dc uintptr }

func (g canvas) fill(r rect, col uint32) {
	br, _, _ := procCreateSolidBrush.Call(uintptr(col))
	procFillRect.Call(g.dc, uintptr(unsafe.Pointer(&r)), br)
	procDeleteObject.Call(br)
}

func (g canvas) roundRect(r rect, fill, border uint32, width, radius int32) {
	br, _, _ := procCreateSolidBrush.Call(uintptr(fill))
	pen, _, _ := procCreatePen.Call(psInsideFrame, uintptr(width), uintptr(border))
	oldBr, _, _ := procSelectObject.Call(g.dc, br)
	oldPen, _, _ := procSelectObject.Call(g.dc, pen)
	procRoundRect.Call(g.dc, uintptr(r.left), uintptr(r.top), uintptr(r.right), uintptr(r.bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(g.dc, oldBr)
	procSelectObject.Call(g.dc, oldPen)
	procDeleteObject.Call(br)
	procDeleteObject.Call(pen)
}

func (g canvas) text(s string, r rect, font uintptr, col uint32, flags uintptr) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	old, _, _ := procSelectObject.Call(g.dc, font)
	procSetTextColor.Call(g.dc, uintptr(col))
	procDrawTextW.Call(g.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)),
		flags|dtSingleLine|dtNoPrefix)
	procSelectObject.Call(g.dc, old)
}

func (g canvas) image(t *thumbImage, r rect) {
	bi := bitmapInfo{width: int32(t.w), height: -int32(t.h), planes: 1, bitCount: 32}
	bi.size = 40
	procSetStretchBltMode.Call(g.dc, stretchHalftone)
	procSetBrushOrgEx.Call(g.dc, 0, 0, 0)
	procStretchDIBits.Call(g.dc, uintptr(r.left), uintptr(r.top), uintptr(r.right-r.left), uintptr(r.bottom-r.top),
		0, 0, uintptr(t.w), uintptr(t.h), uintptr(unsafe.Pointer(&t.bits[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors, srcCopy)
}

// ---- thumbnails ----

var thumbClient = &http.Client{Timeout: 8 * time.Second}

// fetchThumbs loads the ballot's map thumbnails in the background, once each.
// The URLs are built by the app (vote.ThumbURLs), never taken from the server.
func (o *voteOverlay) fetchThumbs(b *BallotView) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.thumbs) > 32 {
		o.thumbs = map[string]*thumbImage{}
	}
	for _, opt := range b.Options {
		if len(opt.Thumbs) == 0 {
			continue
		}
		key := opt.Thumbs[0]
		if _, done := o.thumbs[key]; done || o.fetching[key] {
			continue
		}
		o.fetching[key] = true
		go func(urls []string) {
			var t *thumbImage
			for _, u := range urls {
				if t = loadThumb(u); t != nil {
					break
				}
			}
			o.mu.Lock()
			o.thumbs[urls[0]] = t
			delete(o.fetching, urls[0])
			hwnd := o.hwnd
			o.mu.Unlock()
			if hwnd != 0 && t != nil {
				procPostMessageW.Call(hwnd, wmOverlayUpdate, 0, 0)
			}
		}(slices.Clone(opt.Thumbs))
	}
}

func loadThumb(url string) *thumbImage {
	resp, err := thumbClient.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil
	}
	// A small file can declare huge dimensions; check before decoding so it
	// cannot make the decoder allocate gigabytes.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxThumbSide || cfg.Height > maxThumbSide {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return shrinkThumb(img, thumbW, thumbH)
}

// shrinkThumb crops img to the target aspect (centred) and box-filters it
// down to at most tw x th, as BGRA.
func shrinkThumb(img image.Image, tw, th int) *thumbImage {
	b := img.Bounds()
	if sw, sh := b.Dx(), b.Dy(); sw*th > sh*tw {
		cw := sh * tw / th
		x0 := b.Min.X + (sw-cw)/2
		b = image.Rect(x0, b.Min.Y, x0+cw, b.Max.Y)
	} else {
		ch := sw * th / tw
		y0 := b.Min.Y + (sh-ch)/2
		b = image.Rect(b.Min.X, y0, b.Max.X, y0+ch)
	}
	if b.Dx() < tw || b.Dy() < th {
		tw, th = b.Dx(), b.Dy() // never enlarge; StretchDIBits scales when drawing
	}
	if tw <= 0 || th <= 0 {
		return nil
	}
	t := &thumbImage{w: tw, h: th, bits: make([]byte, tw*th*4)}
	for y := 0; y < th; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/th, b.Min.Y+(y+1)*b.Dy()/th
		for x := 0; x < tw; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/tw, b.Min.X+(x+1)*b.Dx()/tw
			var r, g, bl, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, bl, n = r+cr>>8, g+cg>>8, bl+cb>>8, n+1
				}
			}
			n = max(n, 1)
			i := (y*tw + x) * 4
			t.bits[i], t.bits[i+1], t.bits[i+2], t.bits[i+3] = byte(bl/n), byte(g/n), byte(r/n), 255
		}
	}
	return t
}
