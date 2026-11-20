//go:build windows

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"unsafe"
)

// drawIcon draws the tray icon at size×size pixels: a blue rounded square with
// a white "screen" outline and a smaller window centred inside it.
func drawIcon(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)

	margin := int(math.Round(s * 3 / 16))
	stroke := max(1, int(math.Round(s/16)))
	gap := max(2, int(math.Round(s*2/16)))

	screen := image.Rect(margin, margin, size-margin, size-margin)
	screenInner := screen.Inset(stroke)
	window := screenInner.Inset(gap)

	blue := color.NRGBA{R: 0x25, G: 0x63, B: 0xEB, A: 0xFF}
	white := color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			p := image.Pt(x, y)
			if (p.In(screen) && !p.In(screenInner)) || p.In(window) {
				img.SetNRGBA(x, y, white)
				continue
			}
			if a := roundedSquareCoverage(float64(x), float64(y), s, s*0.22); a > 0 {
				c := blue
				c.A = uint8(math.Round(a * 255))
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img
}

// roundedSquareCoverage returns how much of pixel (x, y) lies inside a rounded
// square of side s and corner radius r, using 4×4 supersampling.
func roundedSquareCoverage(x, y, s, r float64) float64 {
	const n = 4
	hits := 0
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			px := x + (float64(i)+0.5)/n
			py := y + (float64(j)+0.5)/n
			dx := math.Max(math.Max(r-px, px-(s-r)), 0)
			dy := math.Max(math.Max(r-py, py-(s-r)), 0)
			if dx*dx+dy*dy <= r*r {
				hits++
			}
		}
	}
	return float64(hits) / (n * n)
}

// createIconFromPNG builds an HICON from the drawn image. Windows accepts PNG
// data directly in CreateIconFromResourceEx.
func createIconFromPNG(size int) (uintptr, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, drawIcon(size)); err != nil {
		return 0, err
	}
	data := buf.Bytes()
	const iconVersion = 0x00030000
	h, _, err := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		1, iconVersion, uintptr(size), uintptr(size), 0)
	if h == 0 {
		return 0, win32Error("CreateIconFromResourceEx", err)
	}
	return h, nil
}

// createTrayIcon returns the app icon sized for the notification area, falling
// back to the stock application icon.
func createTrayIcon() uintptr {
	size := int(getSystemMetrics(smCxSmIcon))
	if size <= 0 {
		size = 16
	}
	if h, err := createIconFromPNG(size); err == nil {
		return h
	}
	h, _, _ := procLoadIconW.Call(0, idiApplication)
	return h
}
