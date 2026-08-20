package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableManagedAddons(t *testing.T) {
	root := t.TempDir()
	accountPath := filepath.Join(root, "WTF", "Account", "ADMIN", "AddOns.txt")
	characterPath := filepath.Join(root, "WTF", "Account", "ADMIN", "PTR", "Admin", "AddOns.txt")
	if err := os.MkdirAll(filepath.Dir(characterPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{accountPath, characterPath} {
		if err := os.WriteFile(path, []byte("SWP: disabled\r\nOther: enabled\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnableManagedAddons(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{accountPath, characterPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "SWP: enabled") || strings.Contains(text, "SWP: disabled") {
			t.Fatalf("managed addon was not enabled in %s: %q", path, text)
		}
	}
}
