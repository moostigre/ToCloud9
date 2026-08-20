//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type accountProfile struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Username string `json:"username"`
}

type settings struct {
	ClientPath       string           `json:"client_path"`
	Accounts         []accountProfile `json:"accounts,omitempty"`
	DefaultAccount   string           `json:"default_account,omitempty"`
	AutoUpdate       bool             `json:"auto_update"`
	CloseAfterLaunch bool             `json:"close_after_launch"`
	AutoLoginDelay   int              `json:"auto_login_delay_seconds"`
	Environment      string           `json:"environment,omitempty"`
}

func settingsPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "SWP", "launcher-settings.json")
}

func legacySettingsPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "SWP", "launcher-path.txt")
}

func defaultSettings() settings {
	return settings{AutoUpdate: true, CloseAfterLaunch: false, AutoLoginDelay: 8, Environment: "ptr"}
}

func loadSettings() settings {
	value := defaultSettings()
	if b, err := os.ReadFile(settingsPath()); err == nil && json.Unmarshal(b, &value) == nil {
		if value.AutoLoginDelay < 3 || value.AutoLoginDelay > 30 {
			value.AutoLoginDelay = 8
		}
		return value
	}
	if b, err := os.ReadFile(legacySettingsPath()); err == nil {
		value.ClientPath = strings.TrimSpace(string(b))
	}
	return value
}

func saveSettings(value settings) {
	path := settingsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}
