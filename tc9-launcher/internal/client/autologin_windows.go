//go:build windows

package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LaunchWithAccount remembers the account name and starts the game. Passwords
// are deliberately handled by the launcher UI's clipboard and are never sent
// to the game as simulated keyboard input.
func LaunchWithAccount(root, username string) error {
	if err := EnableManagedAddons(root); err != nil {
		return err
	}
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("account name is missing")
	}
	if err := rememberAccountName(root, username); err != nil {
		return err
	}
	_, err := launchProcess(root)
	return err
}

func launchProcess(root string) (*os.Process, error) {
	if _, err := Validate(root); err != nil {
		return nil, err
	}
	if err := clearItemQueryCache(root); err != nil {
		return nil, fmt.Errorf("clear stale item cache: %w", err)
	}
	cmd := exec.Command(filepath.Join(root, "Wow.exe"))
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

func rememberAccountName(root, username string) error {
	configPath := filepath.Join(root, "WTF", "Config.wtf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, _ := os.ReadFile(configPath)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	setting := `SET accountName "` + strings.ReplaceAll(strings.ReplaceAll(username, `\`, ``), `"`, ``) + `"`
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "set accountname ") {
			lines[i], replaced = setting, true
		}
	}
	if !replaced {
		lines = append(lines, setting)
	}
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\r\n")), 0o600)
}
