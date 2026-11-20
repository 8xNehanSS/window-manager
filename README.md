# Window Manager

A small Windows notification-area (system tray) app written in Go. Right-click
(or left-click) the tray icon to see the open windows that can be resized; pick
one and it is resized to **60%** of its screen (configurable) and centred.

- Pure Go + Win32 API (`golang.org/x/sys/windows`), no cgo, single ~2.7 MB exe.
- Works per monitor: a window is resized on whichever screen it is on.
- Restores minimised/maximised windows first, and accounts for the invisible
  Windows 10/11 resize borders, so the visible frame lands exactly on target.
- DPI-aware (per-monitor v2), so sizes are correct on scaled displays.
- Only one copy runs at a time; the icon comes back if Explorer restarts.

## Build

Requires Go 1.26+ on Windows.

```powershell
go build -ldflags "-H windowsgui -s -w" -o window-manager.exe .
```

`-H windowsgui` stops a console window opening alongside the app.

## Use

Run `window-manager.exe`. On Windows 11 new tray icons start out in the
overflow area (the `^` arrow); drag the icon onto the taskbar to keep it visible.

The menu shows:

- **Window list**: visible, resizable app windows, most recently used first,
  with the program name on the right. Click one to resize it.
- **Size**: choose 50 / 60 / 70 / 75 / 80 / 90%, or **Custom size…** to open
  the config file in Notepad.
- **Exit**

### Configuration

Settings live in `%APPDATA%\window-manager\config.json`:

```json
{
  "percent": 60
}
```

`percent` is a percentage of the screen's width and height (10–100). The window
is centred in the area not covered by the taskbar and never made larger than it.
The file is re-read every time the menu opens, so edits apply without a restart.

### Start with Windows

Press `Win+R`, type `shell:startup`, and put a shortcut to `window-manager.exe`
in the folder that opens.

## Notes

- Windows belonging to programs running as administrator can't be resized by a
  normal app (Windows blocks it). Run Window Manager as administrator if you
  need that; otherwise you'll get a notification explaining why it failed.
- Fixed-size windows (no sizing border), tool windows, dialogs, and hidden or
  cloaked windows (e.g. on other virtual desktops) are not listed.

## Tests

```powershell
go test ./...
```

Some tests create real windows briefly on the desktop to check that listing and
resizing work against the actual Win32 APIs.
