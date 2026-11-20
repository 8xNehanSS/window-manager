//go:build windows

package main

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// These tests create real (briefly visible) windows on the desktop.

const (
	wsOverlappedWindow = 0x00CF0000
	wsFixedCaption     = 0x00C80000 // WS_CAPTION | WS_SYSMENU, no sizing border
	swShowNoActivate   = 4
	swMinimize         = 6
)

var testClassRegistered bool

func newTestWindow(t *testing.T, title string, style uint32) uintptr {
	t.Helper()
	if testing.Short() {
		t.Skip("opens real windows; skipped with -short")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	enableDPIAwareness()

	const class = "WindowManagerTestWindow"
	if !testClassRegistered {
		wc := wndClassEx{
			LpfnWndProc:   procDefWindowProcW.Addr(),
			HInstance:     getModuleHandle(),
			LpszClassName: utf16Ptr(class),
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			t.Fatal(win32Error("RegisterClassExW", err))
		}
		testClassRegistered = true
	}
	hwnd, _, err := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr(class))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		uintptr(style), 100, 100, 400, 300, 0, 0, getModuleHandle(), 0)
	if hwnd == 0 {
		t.Fatal(win32Error("CreateWindowExW", err))
	}
	t.Cleanup(func() { destroyWindow(hwnd) })
	showWindow(hwnd, swShowNoActivate)
	return hwnd
}

func listed(hwnd uintptr) (windowInfo, bool) {
	for _, w := range listResizableWindows(0) {
		if w.hwnd == hwnd {
			return w, true
		}
	}
	return windowInfo{}, false
}

func TestListResizableWindows(t *testing.T) {
	resizable := newTestWindow(t, "window-manager test: resizable", wsOverlappedWindow)
	fixed := newTestWindow(t, "window-manager test: fixed", wsFixedCaption)

	w, ok := listed(resizable)
	if !ok {
		t.Fatal("resizable window not listed")
	}
	if w.title != "window-manager test: resizable" || !strings.HasSuffix(strings.ToLower(w.exe), ".exe") {
		t.Errorf("unexpected entry %+v", w)
	}
	if _, ok := listed(fixed); ok {
		t.Error("fixed-size window should not be listed")
	}
}

func TestResizeWindow(t *testing.T) {
	hwnd := newTestWindow(t, "window-manager test: resize", wsOverlappedWindow)

	check := func(percent int) {
		t.Helper()
		if err := resizeWindow(hwnd, percent); err != nil {
			t.Fatalf("resizeWindow: %v", err)
		}
		if isZoomed(hwnd) || isIconic(hwnd) {
			t.Fatal("window is still maximised or minimised")
		}
		mi, err := monitorInfoForWindow(hwnd)
		if err != nil {
			t.Fatal(err)
		}
		want := targetRect(mi.RcMonitor, mi.RcWork, percent)
		got, err := extendedFrameBounds(hwnd)
		if err != nil {
			t.Fatal(err)
		}
		if !near(got, want, 1) {
			t.Errorf("%d%%: visible frame = %+v, want %+v (monitor %+v, work %+v)", percent, got, want, mi.RcMonitor, mi.RcWork)
		}
	}

	check(60)
	check(80)

	showWindow(hwnd, swMaximize)
	check(50)

	showWindow(hwnd, swMaximize)
	showWindow(hwnd, swMinimize)
	check(70)

	destroyWindow(hwnd)
	if err := resizeWindow(hwnd, 60); err == nil {
		t.Error("expected an error resizing a destroyed window")
	}
}

func TestCreateIconFromPNG(t *testing.T) {
	for _, size := range []int{16, 24, 32} {
		h, err := createIconFromPNG(size)
		if err != nil || h == 0 {
			t.Fatalf("size %d: %v", size, err)
		}
		procDestroyIcon.Call(h)
	}
}

func near(a, b rect, tol int32) bool {
	d := func(x, y int32) bool { return x-y <= tol && y-x <= tol }
	return d(a.Left, b.Left) && d(a.Top, b.Top) && d(a.Right, b.Right) && d(a.Bottom, b.Bottom)
}

func TestCanResize(t *testing.T) {
	owner := newTestWindow(t, "window-manager test: owner", wsOverlappedWindow)
	fixed := newTestWindow(t, "window-manager test: fixed", wsFixedCaption)

	// A resizable window owned by another one: the hotkey may resize it, but
	// the tray menu leaves owned windows out.
	owned, _, err := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("WindowManagerTestWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr("window-manager test: owned"))),
		wsOverlappedWindow, 150, 150, 300, 200, owner, 0, getModuleHandle(), 0)
	if owned == 0 {
		t.Fatal(win32Error("CreateWindowExW", err))
	}
	t.Cleanup(func() { destroyWindow(owned) })
	showWindow(owned, swShowNoActivate)

	if !canResize(owned) {
		t.Error("canResize(owned resizable window) = false")
	}
	if isResizableAppWindow(owned) {
		t.Error("owned window should not be listed in the menu")
	}
	if canResize(fixed) {
		t.Error("canResize(fixed-size window) = true")
	}
}

func TestRegisterHotkey(t *testing.T) {
	hwnd := newTestWindow(t, "window-manager test: hotkey", wsOverlappedWindow)
	// An unusual combination, so it can't clash with the running app.
	hk, err := parseHotkey("Win+Ctrl+Alt+Shift+F23")
	if err != nil {
		t.Fatal(err)
	}
	if err := registerHotKey(hwnd, 7, hk.mods|modNoRepeat, hk.vk); err != nil {
		t.Fatalf("registerHotKey(%s): %v", hk, err)
	}
	if err := registerHotKey(hwnd, 8, hk.mods|modNoRepeat, hk.vk); err == nil {
		t.Error("registering the same hotkey twice should fail")
	}
	unregisterHotKey(hwnd, 7)
}

func TestResizeOrMaximize(t *testing.T) {
	hwnd := newTestWindow(t, "window-manager test: toggle", wsOverlappedWindow)

	press := func(wantMaximized bool) {
		t.Helper()
		maximized, err := resizeOrMaximize(hwnd, 60)
		if err != nil {
			t.Fatalf("resizeOrMaximize: %v", err)
		}
		if maximized != wantMaximized || isZoomed(hwnd) != wantMaximized {
			t.Fatalf("maximized = %v, IsZoomed = %v, want %v", maximized, isZoomed(hwnd), wantMaximized)
		}
		if !wantMaximized {
			mi, _ := monitorInfoForWindow(hwnd)
			got, _ := extendedFrameBounds(hwnd)
			if want := targetRect(mi.RcMonitor, mi.RcWork, 60); !near(got, want, 1) {
				t.Fatalf("frame = %+v, want %+v", got, want)
			}
		}
	}
	press(false) // 400x300 → 60%
	press(true)  // already 60% → maximised
	press(false) // maximised → back to 60%
	press(true)

	// A window with no maximise button is only ever resized.
	noMax := newTestWindow(t, "window-manager test: no maximise box", wsOverlappedWindow&^wsMaximizeBox)
	for i := 0; i < 2; i++ {
		if maximized, err := resizeOrMaximize(noMax, 60); err != nil || maximized || isZoomed(noMax) {
			t.Fatalf("press %d: maximized = %v, err = %v; want a plain resize", i+1, maximized, err)
		}
	}
}
