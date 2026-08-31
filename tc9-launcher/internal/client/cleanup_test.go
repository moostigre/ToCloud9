package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupTimestampedEntriesKeepsNewestAndUnknownEntries(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"20260801-120000", "20260802-120000", "20260803-120000", "keep-me"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanupTimestampedEntries(directory, "", "20060102-150405", true, 1); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"20260803-120000", "keep-me"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("expected %s to remain: %v", name, err)
		}
	}
	for _, name := range []string{"20260801-120000", "20260802-120000"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", name, err)
		}
	}
}

func TestCleanupTimestampedEntriesDoesNotFollowSymlinks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(t.TempDir(), "important")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "20260801-120000")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := cleanupTimestampedEntries(directory, "", "20060102-150405", true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was affected: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("unknown symlink should be left untouched: %v", err)
	}
}

func TestCleanupLauncherUpdateCacheKeepsCurrentVersion(t *testing.T) {
	directory := t.TempDir()
	current := "SWPLauncher-" + safeVersion(LauncherVersion) + ".exe"
	for _, name := range []string{current, "SWPLauncher-2.4.50.exe", "SWPLauncher-2.4.53.exe", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanupLauncherUpdateCache(directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{current, "notes.txt"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("expected %s to remain: %v", name, err)
		}
	}
	for _, name := range []string{"SWPLauncher-2.4.50.exe", "SWPLauncher-2.4.53.exe"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", name, err)
		}
	}
}
