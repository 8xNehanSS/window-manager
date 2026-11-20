//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultPercent = 60
	minPercent     = 10
	maxPercent     = 100
)

// config is persisted as JSON, by default in %APPDATA%\window-manager\config.json.
type config struct {
	Percent int `json:"percent"`
	// Hotkey resizes the active window, e.g. "Win+`". Empty disables it.
	Hotkey string `json:"hotkey"`

	path       string
	incomplete bool // the file on disk is missing a setting
}

func defaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "window-manager", "config.json"), nil
}

// loadConfig reads the config at path, creating it with defaults if it does not
// exist and adding any settings it lacks (so they're visible to edit). It
// always returns a usable config; on error the defaults are kept.
func loadConfig(path string) (*config, error) {
	c := &config{Percent: defaultPercent, Hotkey: defaultHotkey, path: path}
	err := c.reload()
	if errors.Is(err, fs.ErrNotExist) || (err == nil && c.incomplete) {
		return c, c.save()
	}
	return c, err
}

// reload re-reads the file so hand edits take effect without a restart. On
// error the current values are left unchanged.
func (c *config) reload() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	// Pointers tell a missing setting (use the default) from an empty one.
	var file struct {
		Percent *int    `json:"percent"`
		Hotkey  *string `json:"hotkey"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", c.path, err)
	}
	c.incomplete = file.Percent == nil || file.Hotkey == nil
	if file.Percent == nil {
		c.Percent = defaultPercent
	} else {
		c.Percent = clampPercent(*file.Percent)
	}
	if file.Hotkey == nil {
		c.Hotkey = defaultHotkey
	} else {
		c.Hotkey = strings.TrimSpace(*file.Hotkey)
	}
	return nil
}

func (c *config) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(data, '\n'), 0o644)
}

func clampPercent(p int) int {
	return max(minPercent, min(maxPercent, p))
}
