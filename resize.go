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

func plausibleBorder(sides ...int32) bool {
	for _, s := range sides {
		if s < 0 || s > 64 {
			return false
		}
	}
	return true
}
