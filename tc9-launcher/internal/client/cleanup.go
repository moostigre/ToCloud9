package client

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const retainedBackupVersions = 1

// CleanupObsoleteVersions prunes launcher and managed-client update history
// while retaining the newest rollback copy. It deliberately ignores unknown
// files and directories so it cannot turn a user-selected client folder into
// a general-purpose cleanup target.
func CleanupObsoleteVersions(clientRoot string) error {
	var cleanupErrors []error
	if executable, err := os.Executable(); err == nil {
		executable, _ = filepath.Abs(executable)
		if err = cleanupTimestampedEntries(
			filepath.Dir(executable),
			filepath.Base(executable)+".previous-",
			"20060102-150405",
			false,
			retainedBackupVersions,
		); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}

	if cacheRoot, err := os.UserCacheDir(); err == nil {
		if err = cleanupLauncherUpdateCache(filepath.Join(cacheRoot, "SWP", "Launcher", "updates")); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}

	clientRoot = filepath.Clean(strings.TrimSpace(clientRoot))
	if safeClientRootForCleanup(clientRoot) {
		backupRoot := filepath.Join(clientRoot, ".swp-backup")
		if err := cleanupTimestampedEntries(backupRoot, "", "20060102-150405", true, retainedBackupVersions); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
		if err := cleanupTimestampedEntries(filepath.Join(backupRoot, "addons"), "", "20060102-150405.000000000", true, retainedBackupVersions); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

func safeClientRootForCleanup(root string) bool {
	if root == "" || root == "." || root == string(filepath.Separator) {
		return false
	}
	wow, wowErr := os.Stat(filepath.Join(root, "Wow.exe"))
	data, dataErr := os.Stat(filepath.Join(root, "Data"))
	return wowErr == nil && !wow.IsDir() && dataErr == nil && data.IsDir()
}

func cleanupLauncherUpdateCache(directory string) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	current := "SWPLauncher-" + safeVersion(LauncherVersion) + ".exe"
	var cleanupErrors []error
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasPrefix(name, "SWPLauncher-") ||
			!strings.HasSuffix(strings.ToLower(name), ".exe") || strings.EqualFold(name, current) {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

type timestampedEntry struct {
	name string
	time time.Time
}

func cleanupTimestampedEntries(directory, prefix, layout string, directories bool, keep int) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	candidates := make([]timestampedEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() != directories || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		timestamp, parseErr := time.Parse(layout, strings.TrimPrefix(entry.Name(), prefix))
		if parseErr != nil {
			continue
		}
		candidates = append(candidates, timestampedEntry{name: entry.Name(), time: timestamp})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].time.Equal(candidates[right].time) {
			return candidates[left].name > candidates[right].name
		}
		return candidates[left].time.After(candidates[right].time)
	})
	if keep < 0 {
		keep = 0
	}
	var cleanupErrors []error
	for _, candidate := range candidates[min(keep, len(candidates)):] {
		path := filepath.Join(directory, candidate.name)
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}
