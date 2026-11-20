//go:build windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetRect(t *testing.T) {
	fullHD := rect{0, 0, 1920, 1080}
	fullHDWork := rect{0, 0, 1920, 1032} // 48px taskbar at the bottom

	tests := []struct {
		name          string
		monitor, work rect
		percent       int
		want          rect
	}{
		{"60% centred in work area", fullHD, fullHDWork, 60, rect{384, 192, 384 + 1152, 192 + 648}},
		{"100% is clamped to the work area", fullHD, fullHDWork, 100, rect{0, 0, 1920, 1032}},
		{"secondary monitor left of primary", rect{-2560, 0, 0, 1440}, rect{-2560, 0, 0, 1440}, 50, rect{-1920, 360, -640, 1080}},
		{"taskbar on top", fullHD, rect{0, 48, 1920, 1080}, 50, rect{480, 294, 1440, 834}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := targetRect(tt.monitor, tt.work, tt.percent); got != tt.want {
				t.Errorf("targetRect = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMenuLabel(t *testing.T) {
	tests := []struct {
		w    windowInfo
		want string
	}{
		{windowInfo{title: "Notes", exe: "notepad.exe"}, "Notes\tnotepad.exe"},
		{windowInfo{title: "Q&A notes"}, "Q&&A notes"},
		{windowInfo{title: "a\tb\nc"}, "a b c"},
		{windowInfo{title: strings.Repeat("x", 20)}, strings.Repeat("x", 9) + "…"},
	}
	for _, tt := range tests {
		if got := menuLabel(tt.w, 10); got != tt.want {
			t.Errorf("menuLabel(%q) = %q, want %q", tt.w.title, got, tt.want)
		}
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")

	c, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig on missing file: %v", err)
	}
	if c.Percent != defaultPercent {
		t.Errorf("default percent = %d, want %d", c.Percent, defaultPercent)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}

	c.Percent = 75
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	if c2, err := loadConfig(path); err != nil || c2.Percent != 75 {
		t.Errorf("reloaded percent = %d (err %v), want 75", c2.Percent, err)
	}

	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for content, want := range map[string]int{
		`{"percent": 250}`: maxPercent,
		`{"percent": 1}`:   minPercent,
		`{}`:               defaultPercent,
		`{"percent": 65}`:  65,
	} {
		write(content)
		if err := c.reload(); err != nil || c.Percent != want {
			t.Errorf("%s: percent = %d (err %v), want %d", content, c.Percent, err, want)
		}
	}

	write(`{"percent": `)
	c.Percent = 65
	if err := c.reload(); err == nil {
		t.Error("expected an error for invalid JSON")
	}
	if c.Percent != 65 {
		t.Errorf("invalid JSON changed percent to %d", c.Percent)
	}

	os.Remove(path)
	if err := c.reload(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("reload of deleted file: err = %v, want ErrNotExist", err)
	}
}

func TestDrawIcon(t *testing.T) {
	for _, size := range []int{16, 20, 24, 32} {
		img := drawIcon(size)
		corner := img.NRGBAAt(0, 0)
		centre := img.NRGBAAt(size/2, size/2)
		if corner.A != 0 {
			t.Errorf("size %d: corner should be transparent, alpha = %d", size, corner.A)
		}
		if centre.R != 0xFF || centre.G != 0xFF || centre.B != 0xFF || centre.A != 0xFF {
			t.Errorf("size %d: centre should be the white window, got %+v", size, centre)
		}
	}
}
