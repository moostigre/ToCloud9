package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnableManagedAddons makes the launcher's managed SWP addon authoritative in
// the per-account addon state used by the 3.3.5 client. Installing fresh files
// is insufficient when a previous session recorded SWP as disabled.
func EnableManagedAddons(root string) error {
	accountRoot := filepath.Join(root, "WTF", "Account")
	accounts, err := os.ReadDir(accountRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read account addon settings: %w", err)
	}
	for _, account := range accounts {
		if !account.IsDir() {
			continue
		}
		accountPath := filepath.Join(accountRoot, account.Name())
		// Older 3.3.5 clients use the account-level file, while some client
		// distributions persist the selection per realm/character. Maintain
		// both forms and update every existing character-specific file.
		if err = enableAddonInFile(filepath.Join(accountPath, "AddOns.txt"), "SWP"); err != nil {
			return err
		}
		err = filepath.WalkDir(accountPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(entry.Name(), "AddOns.txt") ||
				strings.EqualFold(filepath.Dir(path), accountPath) {
				return nil
			}
			return enableAddonInFile(path, "SWP")
		})
		if err != nil {
			return fmt.Errorf("update character addon settings for %s: %w", account.Name(), err)
		}
	}
	return nil
}

func enableAddonInFile(path, addon string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	wanted := strings.ToLower(addon) + ":"
	found := false
	for index, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), wanted) {
			lines[index] = addon + ": enabled"
			found = true
		}
	}
	if !found {
		lines = append(lines, addon+": enabled")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\r\n")), 0o600)
}
