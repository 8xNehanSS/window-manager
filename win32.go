//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 constants, from the Windows SDK headers.
const (
	wmNull      = 0x0000
	wmDestroy   = 0x0002
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	wmHotkey    = 0x0312
	wmApp       = 0x8000
	wmTrayIcon  = wmApp + 1

	nimAdd    = 0x0
	nimModify = 0x1
	nimDelete = 0x2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10

	niifInfo    = 0x1
	niifWarning = 0x2

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	wsThickFrame   = 0x00040000
	wsMaximizeBox  = 0x00010000
	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
	wsExNoActivate = 0x08000000

	gwOwner = 4

	swShowNormal = 1
	swRestore    = 9
	swMaximize   = 3

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	monitorDefaultToNearest = 2

	smCxSmIcon = 49

	dwmwaExtendedFrameBounds = 9
	dwmwaCloaked             = 14

	idiApplication = 32512

	mbIconError = 0x10

	msgfltAllow = 1
)

const (
	gwlStyle   int32 = -16
	gwlExStyle int32 = -20
)

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is ((DPI_AWARENESS_CONTEXT)-4).
const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	procChangeWindowMessageFilterEx   = user32.NewProc("ChangeWindowMessageFilterEx")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procRegisterHotKey                = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey              = user32.NewProc("UnregisterHotKey")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procEnumWindows                   = user32.NewProc("EnumWindows")
	procIsWindow                      = user32.NewProc("IsWindow")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsIconic                      = user32.NewProc("IsIconic")
	procIsZoomed                      = user32.NewProc("IsZoomed")
	procIsHungAppWindow               = user32.NewProc("IsHungAppWindow")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procGetClassNameW                 = user32.NewProc("GetClassNameW")
	procGetWindowLongW                = user32.NewProc("GetWindowLongW")
	procGetWindow                     = user32.NewProc("GetWindow")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procLoadIconW                     = user32.NewProc("LoadIconW")
	procCreateIconFromResourceEx      = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon                   = user32.NewProc("DestroyIcon")
	procMessageBoxW                   = user32.NewProc("MessageBoxW")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) width() int32  { return r.Right - r.Left }
func (r rect) height() int32 { return r.Bottom - r.Top }

type winMsg struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

// notifyIconData mirrors NOTIFYICONDATAW (Vista+ layout).
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32 // union with uTimeout
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

// win32Error turns the lastErr of a failed LazyProc.Call into a useful error.
func win32Error(name string, err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 0 {
		return fmt.Errorf("%s failed", name)
	}
	return fmt.Errorf("%s: %w", name, err)
}

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	if err != nil {
		panic(err) // unreachable: NULs are stripped above
	}
	return p
}

// copyUTF16 copies s into a fixed-size, NUL-terminated buffer, truncating if needed.
func copyUTF16(dst []uint16, s string) {
	src := utf16.Encode([]rune(s))
	n := copy(dst[:len(dst)-1], src)
	dst[n] = 0
}

func enableDPIAwareness() {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		if r, _, _ := procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2); r != 0 {
			return
		}
	}
	procSetProcessDPIAware.Call()
}

func getModuleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func defWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func registerWindowMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16Ptr(name))))
	return uint32(r)
}

// allowMessage lets msg through UIPI, so TaskbarCreated still arrives when we run elevated.
func allowMessage(hwnd uintptr, msg uint32) {
	if procChangeWindowMessageFilterEx.Find() == nil {
		procChangeWindowMessageFilterEx.Call(hwnd, uintptr(msg), msgfltAllow, 0)
	}
}

func postMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) {
	procPostMessageW.Call(hwnd, uintptr(msg), wparam, lparam)
}

func destroyWindow(hwnd uintptr) {
	procDestroyWindow.Call(hwnd)
}

func setForegroundWindow(hwnd uintptr) {
	procSetForegroundWindow.Call(hwnd)
}

func getForegroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

func registerHotKey(hwnd uintptr, id int32, mods, vk uint32) error {
	r, _, err := procRegisterHotKey.Call(hwnd, uintptr(id), uintptr(mods), uintptr(vk))
	if r == 0 {
		return win32Error("RegisterHotKey", err)
	}
	return nil
}

func unregisterHotKey(hwnd uintptr, id int32) {
	procUnregisterHotKey.Call(hwnd, uintptr(id))
}

func getCursorPos() point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func createPopupMenu() (uintptr, error) {
	h, _, err := procCreatePopupMenu.Call()
	if h == 0 {
		return 0, win32Error("CreatePopupMenu", err)
	}
	return h, nil
}

func appendMenu(menu uintptr, flags uint32, id uintptr, text string) error {
	var textPtr uintptr
	if flags&mfSeparator == 0 {
		textPtr = uintptr(unsafe.Pointer(utf16Ptr(text)))
	}
	r, _, err := procAppendMenuW.Call(menu, uintptr(flags), id, textPtr)
	if r == 0 {
		return win32Error("AppendMenuW", err)
	}
	return nil
}

func destroyMenu(menu uintptr) {
	procDestroyMenu.Call(menu)
}

// trackPopupMenu shows menu at (x, y) and returns the chosen command ID (0 if dismissed).
func trackPopupMenu(menu uintptr, flags uint32, x, y int32, owner uintptr) uintptr {
	r, _, _ := procTrackPopupMenu.Call(menu, uintptr(flags), uintptr(x), uintptr(y), 0, owner, 0)
	return r
}

func shellNotifyIcon(message uint32, nid *notifyIconData) error {
	r, _, err := procShellNotifyIconW.Call(uintptr(message), uintptr(unsafe.Pointer(nid)))
	if r == 0 {
		return win32Error("Shell_NotifyIconW", err)
	}
	return nil
}

func isWindow(hwnd uintptr) bool {
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func isIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func isZoomed(hwnd uintptr) bool {
	r, _, _ := procIsZoomed.Call(hwnd)
	return r != 0
}

func isHungAppWindow(hwnd uintptr) bool {
	r, _, _ := procIsHungAppWindow.Call(hwnd)
	return r != 0
}

func getWindowLong(hwnd uintptr, index int32) uint32 {
	r, _, _ := procGetWindowLongW.Call(hwnd, uintptr(index))
	return uint32(r)
}

func getWindow(hwnd uintptr, cmd uint32) uintptr {
	r, _, _ := procGetWindow.Call(hwnd, uintptr(cmd))
	return r
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	r, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:r])
}

func className(hwnd uintptr) string {
	buf := make([]uint16, 256)
	r, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:r])
}

func showWindow(hwnd uintptr, cmd int32) {
	procShowWindow.Call(hwnd, uintptr(cmd))
}

func setWindowPos(hwnd, insertAfter uintptr, x, y, cx, cy int32, flags uint32) error {
	r, _, err := procSetWindowPos.Call(hwnd, insertAfter, uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), uintptr(flags))
	if r == 0 {
		return win32Error("SetWindowPos", err)
	}
	return nil
}

func getWindowRect(hwnd uintptr) (rect, error) {
	var r rect
	ok, _, err := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return r, win32Error("GetWindowRect", err)
	}
	return r, nil
}

// extendedFrameBounds returns the visible window frame, excluding the invisible
// resize borders that GetWindowRect includes on Windows 10 and 11.
func extendedFrameBounds(hwnd uintptr) (rect, error) {
	var r rect
	hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	if hr != 0 {
		return r, fmt.Errorf("DwmGetWindowAttribute: HRESULT 0x%08X", uint32(hr))
	}
	return r, nil
}

// isCloaked reports whether DWM is hiding the window (suspended UWP apps,
// windows on other virtual desktops, and similar).
func isCloaked(hwnd uintptr) bool {
	var cloaked uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	return hr == 0 && cloaked != 0
}

func monitorInfoForWindow(hwnd uintptr) (monitorInfo, error) {
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	mi := monitorInfo{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	r, _, err := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if r == 0 {
		return mi, win32Error("GetMonitorInfoW", err)
	}
	return mi, nil
}

func getSystemMetrics(index int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r)
}

func messageBox(text, caption string, flags uint32) {
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(caption))), uintptr(flags))
}
