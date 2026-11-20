//go:build windows

// Command window-manager is a Windows notification-area (system tray) utility.
// Clicking its icon lists the open, resizable application windows; picking one
// resizes it to a configurable percentage of its screen and centres it there.
//
// Build without a console window:
//
//	go build -ldflags "-H windowsgui -s -w" -o window-manager.exe .
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	appName       = "Window Manager"
	windowClass   = "WindowManagerTrayWindow"
	singletonName = `Local\window-manager-tray`
	trayIconID    = 1
	hotkeyID      = 1
	maxTitleRunes = 60

	// Menu command IDs.
	cmdExit       = 1
	cmdEditConfig = 2
	cmdPresetBase = 100  // cmdPresetBase + percent
	cmdWindowBase = 1000 // cmdWindowBase + index into the listed windows
)

// Sizes offered in the "Size" submenu. Any other value can be set in config.json.
var presets = []int{50, 60, 70, 75, 80, 90}

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

type app struct {
	hwnd              uintptr
	icon              uintptr
	cfg               *config
	taskbarCreatedMsg uint32
	menuOpen          bool
	lastConfigErr     string

	hotkeyApplied bool   // applyHotkey has run at least once
	hotkeySpec    string // config value it last applied
	hotkeyName    string // canonical name while registered, e.g. "Win+Alt+Z"
	hotkeyStatus  string // menu text describing the hotkey state
}

var (
	// The window procedure needs package-level access to the app.
	theApp          *app
	wndProcCallback = windows.NewCallback(wndProc)
)

func main() {
	// All Win32 window and menu calls must stay on the thread that owns the window.
	runtime.LockOSThread()

	if _, err := windows.CreateMutex(nil, false, utf16Ptr(singletonName)); errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return // already running
	}

	enableDPIAwareness()

	a, err := newApp()
	if err != nil {
		messageBox(err.Error(), appName, mbIconError)
		return
	}
	a.run()
}

func newApp() (*app, error) {
	path, err := defaultConfigPath()
	if err != nil {
		return nil, fmt.Errorf("finding config folder: %w", err)
	}
	cfg, cfgErr := loadConfig(path)

	a := &app{cfg: cfg}
	theApp = a

	hInstance := getModuleHandle()
	wc := wndClassEx{
		LpfnWndProc:   wndProcCallback,
		HInstance:     hInstance,
		LpszClassName: utf16Ptr(windowClass),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return nil, win32Error("RegisterClassExW", err)
	}

	// A hidden top-level window (not a message-only one) so it can become the
	// foreground window, which TrackPopupMenu needs to close the menu properly.
	hwnd, _, err := procCreateWindowExW.Call(
		wsExToolWindow,
		uintptr(unsafe.Pointer(utf16Ptr(windowClass))),
		uintptr(unsafe.Pointer(utf16Ptr(appName))),
		0, 0, 0, 0, 0, 0, 0, hInstance, 0)
	if hwnd == 0 {
		return nil, win32Error("CreateWindowExW", err)
	}
	a.hwnd = hwnd

	// Explorer broadcasts TaskbarCreated when it restarts; re-add the icon then.
	a.taskbarCreatedMsg = registerWindowMessage("TaskbarCreated")
	allowMessage(hwnd, a.taskbarCreatedMsg)

	a.icon = createTrayIcon()

	// At sign-in the app can start before the taskbar is ready, so retry.
	for attempt := 1; ; attempt++ {
		if err = a.addTrayIcon(); err == nil {
			break
		}
		if attempt == 30 {
			return nil, fmt.Errorf("adding the notification area icon: %w", err)
		}
		time.Sleep(time.Second)
	}

	if cfgErr != nil {
		a.reportConfigError(cfgErr)
	}
	a.applyHotkey()
	a.updateTooltip()
	return a, nil
}

func (a *app) run() {
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	if a == nil {
		return defWindowProc(hwnd, msg, wparam, lparam)
	}
	switch uint32(msg) {
	case wmTrayIcon:
		// With the default NOTIFYICON version, lParam is the mouse message.
		switch uint32(lparam) {
		case wmRButtonUp, wmLButtonUp:
			a.showMenu()
		}
		return 0
	case wmHotkey:
		if wparam == hotkeyID {
			a.resizeActiveWindow()
		}
		return 0
	case wmDestroy:
		if a.hotkeyName != "" {
			unregisterHotKey(a.hwnd, hotkeyID)
		}
		a.removeTrayIcon()
		procPostQuitMessage.Call(0)
		return 0
	}
	if a.taskbarCreatedMsg != 0 && uint32(msg) == a.taskbarCreatedMsg {
		a.addTrayIcon()
		return 0
	}
	return defWindowProc(hwnd, msg, wparam, lparam)
}

// showMenu builds the menu from the windows open right now, shows it at the
// cursor and acts on the choice.
func (a *app) showMenu() {
	if a.menuOpen {
		return
	}
	a.menuOpen = true
	defer func() { a.menuOpen = false }()

	a.refreshConfig()
	percent := a.cfg.Percent
	targets := listResizableWindows(a.hwnd)

	menu, err := a.buildMenu(percent, targets)
	if err != nil {
		a.notify("Couldn't open the menu", err.Error(), niifWarning)
		return
	}
	defer destroyMenu(menu) // also destroys the attached submenu

	pt := getCursorPos()
	setForegroundWindow(a.hwnd)
	cmd := int(trackPopupMenu(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify, pt.X, pt.Y, a.hwnd))
	postMessage(a.hwnd, wmNull, 0, 0)

	switch {
	case cmd == cmdExit:
		destroyWindow(a.hwnd)
	case cmd == cmdEditConfig:
		a.editConfig()
	case cmd > cmdPresetBase && cmd <= cmdPresetBase+maxPercent:
		a.setPercent(cmd - cmdPresetBase)
	case cmd >= cmdWindowBase && cmd < cmdWindowBase+len(targets):
		a.resize(targets[cmd-cmdWindowBase], percent)
	}
}

func (a *app) buildMenu(percent int, targets []windowInfo) (uintptr, error) {
	menu, err := createPopupMenu()
	if err != nil {
		return 0, err
	}
	sizeMenu, err := createPopupMenu()
	if err != nil {
		destroyMenu(menu)
		return 0, err
	}

	type item struct {
		menu  uintptr
		flags uint32
		id    uintptr
		text  string
	}
	items := []item{
		{menu, mfString | mfGrayed, 0, fmt.Sprintf("Resize to %d%% of the screen:", percent)},
		{menu, mfSeparator, 0, ""},
	}
	if len(targets) == 0 {
		items = append(items, item{menu, mfString | mfGrayed, 0, "No resizable windows open"})
	}
	for i, w := range targets {
		items = append(items, item{menu, mfString, uintptr(cmdWindowBase + i), menuLabel(w, maxTitleRunes)})
	}

	isPreset := false
	for _, p := range presets {
		isPreset = isPreset || p == percent
	}
	if !isPreset {
		items = append(items,
			item{sizeMenu, mfString | mfChecked, uintptr(cmdPresetBase + percent), fmt.Sprintf("%d%% (custom)", percent)},
			item{sizeMenu, mfSeparator, 0, ""})
	}
	for _, p := range presets {
		flags := uint32(mfString)
		if p == percent {
			flags |= mfChecked
		}
		items = append(items, item{sizeMenu, flags, uintptr(cmdPresetBase + p), fmt.Sprintf("%d%%", p)})
	}
	items = append(items,
		item{sizeMenu, mfSeparator, 0, ""},
		item{sizeMenu, mfString, cmdEditConfig, "Custom size… (edit config.json)"},
		item{menu, mfSeparator, 0, ""},
		item{menu, mfString | mfPopup, sizeMenu, fmt.Sprintf("Size: %d%%", percent)},
		item{menu, mfString | mfGrayed, 0, a.hotkeyStatus},
		item{menu, mfSeparator, 0, ""},
		item{menu, mfString, cmdExit, "Exit"},
	)

	attached := false // once attached, sizeMenu is destroyed along with menu
	for _, it := range items {
		if err := appendMenu(it.menu, it.flags, it.id, it.text); err != nil {
			destroyMenu(menu)
			if !attached {
				destroyMenu(sizeMenu)
			}
			return 0, err
		}
		attached = attached || it.flags&mfPopup != 0
	}
	return menu, nil
}

func (a *app) resize(w windowInfo, percent int) {
	if err := resizeWindow(w.hwnd, percent); err != nil {
		a.notify("Couldn't resize "+truncate(w.title, 40), err.Error(), niifWarning)
		return
	}
	setForegroundWindow(w.hwnd)
}

// resizeActiveWindow handles the hotkey.
func (a *app) resizeActiveWindow() {
	a.refreshConfig()
	hwnd := getForegroundWindow()
	if hwnd == 0 || hwnd == a.hwnd {
		return
	}
	if !canResize(hwnd) {
		a.notify("Can't resize this window", "The active window has a fixed size or is part of Windows.", niifInfo)
		return
	}
	if err := resizeWindow(hwnd, a.cfg.Percent); err != nil {
		a.notify("Couldn't resize "+truncate(windowText(hwnd), 40), err.Error(), niifWarning)
	}
}

// refreshConfig re-reads config.json so hand edits apply without a restart.
func (a *app) refreshConfig() {
	if err := a.cfg.reload(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		a.reportConfigError(err)
	} else {
		a.lastConfigErr = ""
	}
	if a.applyHotkey() {
		a.updateTooltip()
	}
}

// applyHotkey (re)registers the configured hotkey if it changed since the last
// call, and reports whether it did anything.
func (a *app) applyHotkey() bool {
	spec := a.cfg.Hotkey
	if a.hotkeyApplied && spec == a.hotkeySpec {
		return false
	}
	a.hotkeyApplied, a.hotkeySpec = true, spec
	if a.hotkeyName != "" {
		unregisterHotKey(a.hwnd, hotkeyID)
		a.hotkeyName = ""
	}

	if spec == "" {
		a.hotkeyStatus = "Hotkey off (set one in config.json)"
		return true
	}
	hk, err := parseHotkey(spec)
	if err != nil {
		a.hotkeyStatus = "Hotkey in config.json is invalid"
		a.notify("Hotkey not set", "config.json: "+err.Error(), niifWarning)
		return true
	}
	if err := registerHotKey(a.hwnd, hotkeyID, hk.mods|modNoRepeat, hk.vk); err != nil {
		a.hotkeyStatus = "Hotkey " + hk.String() + " is used by another app"
		a.notify("Hotkey "+hk.String()+" is taken",
			"Another app already uses it. Choose a different one in config.json (Size → Custom size…).", niifWarning)
		return true
	}
	a.hotkeyName = hk.String()
	a.hotkeyStatus = "Resize active window\t" + a.hotkeyName
	return true
}

func (a *app) setPercent(p int) {
	a.cfg.Percent = clampPercent(p)
	if err := a.cfg.save(); err != nil {
		a.notify("Couldn't save settings", err.Error(), niifWarning)
	}
	a.updateTooltip()
}

func (a *app) editConfig() {
	if err := a.cfg.save(); err != nil { // make sure the file exists and is current
		a.notify("Couldn't save settings", err.Error(), niifWarning)
		return
	}
	err := windows.ShellExecute(windows.Handle(a.hwnd), utf16Ptr("open"), utf16Ptr("notepad.exe"),
		utf16Ptr(`"`+a.cfg.path+`"`), nil, swShowNormal)
	if err != nil {
		a.notify("Couldn't open config.json", a.cfg.path+": "+err.Error(), niifWarning)
	}
}

// reportConfigError shows a config problem once, not on every menu open.
func (a *app) reportConfigError(err error) {
	if err.Error() == a.lastConfigErr {
		return
	}
	a.lastConfigErr = err.Error()
	a.notify("Settings file problem", fmt.Sprintf("%v\nUsing %d%%.", err, a.cfg.Percent), niifWarning)
}

func (a *app) tooltip() string {
	tip := appName
	if version != "dev" {
		tip += " " + version
	}
	tip += fmt.Sprintf(" – resize to %d%%", a.cfg.Percent)
	if a.hotkeyName != "" {
		tip += " (" + a.hotkeyName + ")"
	}
	return tip
}

func (a *app) baseNotifyData() notifyIconData {
	nid := notifyIconData{HWnd: a.hwnd, UID: trayIconID}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	return nid
}

func (a *app) addTrayIcon() error {
	nid := a.baseNotifyData()
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTrayIcon
	nid.HIcon = a.icon
	copyUTF16(nid.SzTip[:], a.tooltip())
	return shellNotifyIcon(nimAdd, &nid)
}

func (a *app) updateTooltip() {
	nid := a.baseNotifyData()
	nid.UFlags = nifTip
	copyUTF16(nid.SzTip[:], a.tooltip())
	shellNotifyIcon(nimModify, &nid)
}

func (a *app) removeTrayIcon() {
	nid := a.baseNotifyData()
	shellNotifyIcon(nimDelete, &nid)
}

// notify shows a Windows notification from the tray icon.
func (a *app) notify(title, text string, flags uint32) {
	nid := a.baseNotifyData()
	nid.UFlags = nifInfo
	nid.DwInfoFlags = flags
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], text)
	shellNotifyIcon(nimModify, &nid)
}
