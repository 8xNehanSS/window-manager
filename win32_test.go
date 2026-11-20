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
	swMaximize         = 3
	swMinimize         = 6
)

var testClassRegistered bool

func newTestWindow(t *testing.T, title string, style uint32) uintptr {
	t.Helper()
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
