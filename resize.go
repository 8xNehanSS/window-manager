//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// targetRect returns where a window's visible frame should go: percent% of the
// monitor's width and height, centred in the work area (the monitor minus the
// taskbar), and never larger than the work area.
func targetRect(monitor, work rect, percent int) rect {
	w := monitor.width() * int32(percent) / 100
	h := monitor.height() * int32(percent) / 100
	w = min(w, work.width())
	h = min(h, work.height())
	x := work.Left + (work.width()-w)/2
	y := work.Top + (work.height()-h)/2
	return rect{Left: x, Top: y, Right: x + w, Bottom: y + h}
}

// resizeWindow restores hwnd if it is minimised or maximised, then resizes it
// to percent% of the screen it is on and centres it there.
func resizeWindow(hwnd uintptr, percent int) error {
	if !isWindow(hwnd) {
		return errors.New("the window has been closed")
	}
	// A window minimised from the maximised state restores to maximised first,
	// so it can take two restores to get a normal, sizeable window.
	for i := 0; i < 2 && (isIconic(hwnd) || isZoomed(hwnd)); i++ {
		showWindow(hwnd, swRestore)
	}

	mi, err := monitorInfoForWindow(hwnd)
	if err != nil {
		return err
	}
	visible := targetRect(mi.RcMonitor, mi.RcWork, percent)

	// SetWindowPos works on the outer rect, which on Windows 10/11 includes
	// invisible resize borders. Grow the target by those borders so the part
	// you can see lands exactly on `visible`.
	var left, top, right, bottom int32
	if outer, err := getWindowRect(hwnd); err == nil {
		if frame, err := extendedFrameBounds(hwnd); err == nil {
			left, top = frame.Left-outer.Left, frame.Top-outer.Top
			right, bottom = outer.Right-frame.Right, outer.Bottom-frame.Bottom
		}
	}
	if !plausibleBorder(left, top, right, bottom) {
		left, top, right, bottom = 0, 0, 0, 0
	}

	err = setWindowPos(hwnd, 0,
		visible.Left-left, visible.Top-top,
		visible.width()+left+right, visible.height()+top+bottom,
		swpNoZOrder|swpNoActivate)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return errors.New("it is running as administrator; run Window Manager as administrator to resize it")
	}
	return err
}

// resizeOrMaximize is the hotkey action. It maximises hwnd if the window is
// already about percent% of its screen, and otherwise resizes it to that size
// (restoring it first if it is maximised). Pressing the hotkey repeatedly
// therefore switches between the chosen size and maximised.
func resizeOrMaximize(hwnd uintptr, percent int) (maximized bool, err error) {
	if isWindow(hwnd) && !isZoomed(hwnd) && !isIconic(hwnd) && canMaximize(hwnd) {
		if near, err := nearTargetSize(hwnd, percent); err == nil && near {
			showWindow(hwnd, swMaximize)
			if !isZoomed(hwnd) {
				return false, errors.New("Windows didn't let it be maximised; it may be running as administrator")
			}
			return true, nil
		}
	}
	return false, resizeWindow(hwnd, percent)
}

// canMaximize reports whether the window has a maximise button; apps without
// one usually don't expect to be maximised.
func canMaximize(hwnd uintptr) bool {
	return getWindowLong(hwnd, gwlStyle)&wsMaximizeBox != 0
}

// nearTargetSize reports whether hwnd's visible frame is already about the
// size resizeWindow would give it, wherever it is on the screen.
func nearTargetSize(hwnd uintptr, percent int) (bool, error) {
	mi, err := monitorInfoForWindow(hwnd)
	if err != nil {
		return false, err
	}
	frame, err := extendedFrameBounds(hwnd)
	if err != nil {
		if frame, err = getWindowRect(hwnd); err != nil {
			return false, err
		}
	}
	return sizeNear(frame, targetRect(mi.RcMonitor, mi.RcWork, percent), mi.RcMonitor), nil
}

// sizeNear compares only width and height, allowing 2% of the monitor (at
// least 16 px) either way, because some apps round their size, e.g. terminals
// that size themselves in whole characters.
func sizeNear(got, want, monitor rect) bool {
	tolW := max(16, monitor.width()/50)
	tolH := max(16, monitor.height()/50)
	return abs32(got.width()-want.width()) <= tolW && abs32(got.height()-want.height()) <= tolH
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func plausibleBorder(sides ...int32) bool {
	for _, s := range sides {
		if s < 0 || s > 64 {
			return false
		}
	}
	return true
}
