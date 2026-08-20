package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"0.4.0", "0.3.0", 1},
		{"0.4.0", "0.4.0", 0},
		{"0.4", "0.4.1", -1},
		{"v1.10.0", "1.9.9", 1},
	}
	for _, test := range tests {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestReplaceWhenUnlocked(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "new.exe")
	target := filepath.Join(directory, "launcher.exe")
	if err := os.WriteFile(source, []byte("new launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceWhenUnlocked(source, target); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "new launcher" {
		t.Fatalf("installed content = %q", installed)
	}
	backups, err := filepath.Glob(target + ".previous-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, err = %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || string(backup) != "old launcher" {
		t.Fatalf("backup content = %q, err = %v", backup, err)
	}
}
