//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// RegisterHotKey modifier flags.
const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000
)

// defaultHotkey resizes the active window. Win+Alt+R and Win+Alt+W are taken
// by Windows itself, Ctrl+Alt combos clash with AltGr on many keyboard layouts.
const defaultHotkey = "Win+Alt+Z"

type hotkey struct {
	mods uint32 // modAlt | modControl | modShift | modWin
	vk   uint32 // virtual-key code
}

// Non-alphanumeric keys accepted in a hotkey, with their display names.
var namedKeys = []struct {
	name string
	vk   uint32
}{
	{"Space", 0x20}, {"Enter", 0x0D}, {"Tab", 0x09},
	{"Left", 0x25}, {"Up", 0x26}, {"Right", 0x27}, {"Down", 0x28},
	{"Insert", 0x2D}, {"Delete", 0x2E}, {"Home", 0x24}, {"End", 0x23},
	{"PageUp", 0x21}, {"PageDown", 0x22},
}

// parseHotkey parses strings like "Win+Alt+Z" or "ctrl + shift + F5". It needs
// at least one modifier and exactly one key: A–Z, 0–9, F1–F24 or a named key.
func parseHotkey(s string) (hotkey, error) {
	var hk hotkey
	for _, part := range strings.Split(s, "+") {
		tok := strings.TrimSpace(part)
		switch strings.ToLower(tok) {
		case "":
			return hotkey{}, fmt.Errorf("%q has an empty key", s)
		case "ctrl", "control":
			hk.mods |= modControl
		case "alt":
			hk.mods |= modAlt
		case "shift":
			hk.mods |= modShift
		case "win", "windows":
			hk.mods |= modWin
		default:
			if hk.vk != 0 {
				return hotkey{}, fmt.Errorf("%q has more than one key", s)
			}
			vk, ok := keyCode(tok)
			if !ok {
				return hotkey{}, fmt.Errorf("%q: unknown key %q", s, tok)
			}
			hk.vk = vk
		}
	}
	if hk.vk == 0 {
		return hotkey{}, fmt.Errorf("%q has no key, only modifiers", s)
	}
	if hk.mods == 0 {
		return hotkey{}, fmt.Errorf("%q needs at least one of Ctrl, Alt, Shift or Win", s)
	}
	return hk, nil
}

func keyCode(tok string) (uint32, bool) {
	if len(tok) == 1 {
		c := strings.ToUpper(tok)[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint32(c), true // VK codes for letters and digits are their ASCII values
		}
	}
	if len(tok) > 1 && (tok[0] == 'F' || tok[0] == 'f') {
		if n, err := strconv.Atoi(tok[1:]); err == nil && n >= 1 && n <= 24 {
			return 0x70 + uint32(n-1), true // VK_F1..VK_F24
		}
	}
	for _, k := range namedKeys {
		if strings.EqualFold(tok, k.name) {
			return k.vk, true
		}
	}
	return 0, false
}

// String formats the hotkey in a canonical form, e.g. "Win+Ctrl+Alt+Shift+Z".
func (h hotkey) String() string {
	var parts []string
	for _, m := range []struct {
		flag uint32
		name string
	}{{modWin, "Win"}, {modControl, "Ctrl"}, {modAlt, "Alt"}, {modShift, "Shift"}} {
		if h.mods&m.flag != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, keyName(h.vk)), "+")
}

func keyName(vk uint32) string {
	switch {
	case (vk >= 'A' && vk <= 'Z') || (vk >= '0' && vk <= '9'):
		return string(rune(vk))
	case vk >= 0x70 && vk <= 0x87:
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	for _, k := range namedKeys {
		if k.vk == vk {
			return k.name
		}
	}
	return fmt.Sprintf("VK_%02X", vk)
}
