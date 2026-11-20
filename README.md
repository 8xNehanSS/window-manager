# Window Manager

A small Windows notification-area (system tray) app written in Go. Right-click
(or left-click) the tray icon to see the open windows that can be resized; pick
one and it is resized to **60%** of its screen (configurable) and centred. Or
press **Win+Alt+Z** to do the same to the active window.

- Pure Go + Win32 API (`golang.org/x/sys/windows`), no cgo, single ~2.7 MB exe.
- Works per monitor: a window is resized on whichever screen it is on.
- Restores minimised/maximised windows first, and accounts for the invisible
  Windows 10/11 resize borders, so the visible frame lands exactly on target.
- DPI-aware (per-monitor v2), so sizes are correct on scaled displays.
- Only one copy runs at a time; the icon comes back if Explorer restarts.

## Download

Prebuilt `window-manager.exe` files for x64 and ARM64 are attached to each
[GitHub release](../../releases) (see [Releases](#releases) below).

## Build

Requires Go 1.26+ on Windows.

```powershell
go build -ldflags "-H windowsgui -s -w" -o window-manager.exe .
```

`-H windowsgui` stops a console window opening alongside the app. Add
`-X main.version=v1.2.3` to the `-ldflags` to stamp a version into the tooltip.

## Use

Run `window-manager.exe`. On Windows 11 new tray icons start out in the
overflow area (the `^` arrow); drag the icon onto the taskbar to keep it visible.

The menu shows:

- **Window list**: visible, resizable app windows, most recently used first,
  with the program name on the right. Click one to resize it.
- **Size**: choose 50 / 60 / 70 / 75 / 80 / 90%, or **Custom size…** to open
  the config file in Notepad.
- **Resize active window**: the hotkey, if it's active.
- **Exit**

### Hotkey

Press **Win+Alt+Z** to resize whichever window is active to the current size.
Unlike the menu, the hotkey also works on resizable windows that don't show in
Alt+Tab, such as some secondary windows. If another app already uses the
combination, you'll get a notification and the menu says so; pick another one
in the config file.

### Configuration

Settings live in `%APPDATA%\window-manager\config.json`:

```json
{
  "percent": 60,
  "hotkey": "Win+Alt+Z"
}
```

- `percent` is a percentage of the screen's width and height (10–100). The
  window is centred in the area not covered by the taskbar and never made larger
  than it.
- `hotkey` is one or more of `Win`, `Ctrl`, `Alt`, `Shift` plus one key: `A`–`Z`,
  `0`–`9`, `F1`–`F24`, `Space`, `Enter`, `Tab`, `Left`, `Right`, `Up`, `Down`,
  `Insert`, `Delete`, `Home`, `End`, `PageUp` or `PageDown`. Use `""` to turn
  the hotkey off. Avoid `Ctrl+Alt` combinations: on many keyboard layouts they
  are the same as AltGr and would block typing some characters.

The file is re-read every time the menu opens or the hotkey is pressed, so
edits apply without a restart.

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
resizing work against the actual Win32 APIs. `go test -short ./...` skips them.

## Releases

GitHub Actions (`.github/workflows/ci.yml`) checks formatting, vets and tests
every push to `main` and every pull request. Pushing a tag that starts with `v`
also builds the release:

```powershell
git tag v1.0.0
git push origin v1.0.0
```

The workflow builds `window-manager.exe` for x64 and ARM64 with the tag as its
version. It zips each one with this README and writes a `SHA256SUMS.txt`. Then
it publishes all of them to a GitHub release named after the tag, with notes
generated from the commits. Tags with a hyphen (such as `v1.1.0-rc1`) are
marked as pre-releases. The files are also kept as a workflow artifact on the
run.
