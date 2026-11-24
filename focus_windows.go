//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procSwitchToThisWindow       = user32.NewProc("SwitchToThisWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
)

const (
	hwndTopmost   = ^uintptr(0) // HWND_TOPMOST   = (HWND)-1
	hwndNoTopmost = ^uintptr(1) // HWND_NOTOPMOST = (HWND)-2

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	swRestore = 9

	// How long to keep the launcher window pinned above other topmost
	// windows (such as Playnite's fullscreen UI) before releasing it back to
	// normal z-order, so it doesn't stay stuck over everything afterwards.
	topmostNudgeDuration = 1500 * time.Millisecond
)

// bringToForeground makes hwnd the active, foreground window. Windows only
// lets a process change the foreground window if it is attached to the
// input queue of the thread that currently owns it, so we briefly attach to
// the current foreground window's thread first. SwitchToThisWindow is a
// belt-and-braces fallback that emulates an Alt+Tab switch for cases where
// SetForegroundWindow is still refused.
func bringToForeground(hwnd uintptr) {
	current := uintptr(windows.GetCurrentThreadId())
	foreground, _, _ := procGetForegroundWindow.Call()
	var attachedTo uintptr
	if foreground != 0 && foreground != hwnd {
		foregroundThread, _, _ := procGetWindowThreadProcessId.Call(foreground, 0)
		if foregroundThread != 0 && foregroundThread != current {
			if ok, _, _ := procAttachThreadInput.Call(foregroundThread, current, 1); ok != 0 {
				attachedTo = foregroundThread
			}
		}
	}

	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procSwitchToThisWindow.Call(hwnd, 1)

	if attachedTo != 0 {
		procAttachThreadInput.Call(attachedTo, current, 0)
	}
}

// focusWindow finds our main window by title and brings it to the
// foreground. It is also pinned topmost for a moment so it lands above
// topmost frontends (Playnite fullscreen mode), then released.
func focusWindow(title string) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}

	// The window may not exist or be visible yet when DOM-ready fires.
	var hwnd uintptr
	for attempt := 0; attempt < 30; attempt++ {
		hwnd, _, _ = procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd != 0 {
			if visible, _, _ := procIsWindowVisible.Call(hwnd); visible != 0 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if hwnd == 0 {
		return
	}

	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	bringToForeground(hwnd)
	time.Sleep(topmostNudgeDuration)
	procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

// EnumWindows needs a C-callable callback. syscall.NewCallback allocates a
// permanent trampoline each time it's called, so create it exactly once and
// pass state through package variables (only ever used from the single
// wait-for-game goroutine).
var (
	enumWantPIDs        map[uint32]bool
	enumFoundWindows    []uintptr
	enumWindowsCallback = syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1 // continue enumeration
		}
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if enumWantPIDs[pid] {
			enumFoundWindows = append(enumFoundWindows, hwnd)
		}
		return 1
	})
)

// visibleWindowsOf returns every visible top-level window owned by one of
// the given process IDs, in Z order (topmost first).
func visibleWindowsOf(pids []uint32) []uintptr {
	enumWantPIDs = make(map[uint32]bool, len(pids))
	for _, pid := range pids {
		enumWantPIDs[pid] = true
	}
	enumFoundWindows = nil
	procEnumWindows.Call(enumWindowsCallback, 0)
	return enumFoundWindows
}

// gameFocuser hands foreground focus to the game's windows as they appear.
// Steam launches MCC rather than the launcher, so when the launcher hides
// itself nothing in the foreground grants the game permission to activate
// its own window; without this it starts behind whatever else is open.
type gameFocuser struct {
	seen map[uintptr]bool
}

func newGameFocuser() *gameFocuser {
	return &gameFocuser{seen: map[uintptr]bool{}}
}

// focusNewWindows brings to the foreground any visible top-level window
// owned by one of pids that hasn't been seen before, and reports whether it
// focused anything. Each window is only ever focused once, so the user can
// still alt-tab away afterwards without being dragged back.
func (f *gameFocuser) focusNewWindows(pids []uint32) bool {
	focused := false
	for _, hwnd := range visibleWindowsOf(pids) {
		if f.seen[hwnd] {
			continue
		}
		f.seen[hwnd] = true
		bringToForeground(hwnd)
		focused = true
	}
	return focused
}
