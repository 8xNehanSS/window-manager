//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// windowInfo is one entry in the tray menu.
type windowInfo struct {
	hwnd  uintptr
	title string
	exe   string // e.g. "chrome.exe"; empty if the process can't be queried
}

// Shell windows that have a resizable frame but must never be touched.
var shellClasses = map[string]bool{
	"Progman":                true,
	"WorkerW":                true,
	"Shell_TrayWnd":          true,
	"Shell_SecondaryTrayWnd": true,
}

// EnumWindows needs a callback, and windows.NewCallback allocates from a small
// fixed pool that is never freed, so the callback is created once and reports
// into package state guarded by enumMu.
var (
	enumMu       sync.Mutex
	enumExclude  uintptr
	enumFound    []windowInfo
	enumCallback = windows.NewCallback(enumWindowsProc)
)

// listResizableWindows returns the visible, resizable top-level application
// windows in Z-order (most recently active first), skipping exclude.
func listResizableWindows(exclude uintptr) []windowInfo {
	enumMu.Lock()
	defer enumMu.Unlock()

	enumExclude = exclude
	enumFound = nil
	procEnumWindows.Call(enumCallback, 0)

	found := enumFound
	enumFound = nil
	return found
}

func enumWindowsProc(hwnd, _ uintptr) uintptr {
	if hwnd != enumExclude && isResizableAppWindow(hwnd) {
		enumFound = append(enumFound, windowInfo{
			hwnd:  hwnd,
			title: windowText(hwnd),
			exe:   processName(hwnd),
		})
	}
	return 1 // keep enumerating
}

// isResizableAppWindow reports whether hwnd is a window a user would think of
// as an application window (the kind that shows in Alt+Tab) with a sizing border.
func isResizableAppWindow(hwnd uintptr) bool {
	if !isWindowVisible(hwnd) {
		return false
	}
	style := getWindowLong(hwnd, gwlStyle)
	exStyle := getWindowLong(hwnd, gwlExStyle)
	switch {
	case style&wsThickFrame == 0: // fixed-size window
		return false
	case exStyle&wsExToolWindow != 0, exStyle&wsExNoActivate != 0:
		return false
	case getWindow(hwnd, gwOwner) != 0 && exStyle&wsExAppWindow == 0: // dialogs and popups
		return false
	case isCloaked(hwnd), isHungAppWindow(hwnd):
		return false
	case shellClasses[className(hwnd)]:
		return false
	}
	return strings.TrimSpace(windowText(hwnd)) != ""
}

func processName(hwnd uintptr) string {
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil || pid == 0 {
		return ""
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(proc)

	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(proc, 0, &buf[0], &n); err != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(buf[:n]))
}

// menuLabel formats a window for the menu: "Title<TAB>app.exe". The tab makes
// Windows right-align the exe name; "&" is doubled so it isn't read as a mnemonic.
func menuLabel(w windowInfo, maxRunes int) string {
	clean := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	title := truncate(strings.TrimSpace(clean.Replace(w.title)), maxRunes)
	label := strings.ReplaceAll(title, "&", "&&")
	if w.exe != "" {
		label += "\t" + strings.ReplaceAll(w.exe, "&", "&&")
	}
	return label
}

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(r[:maxRunes-1])) + "…"
}
