package client

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addonZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestInstallAddonArchiveRequiresAndInstallsInterface30300(t *testing.T) {
	root := t.TempDir()
	accountSettings := filepath.Join(root, "WTF", "Account", "ADMIN", "AddOns.txt")
	if err := os.MkdirAll(filepath.Dir(accountSettings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accountSettings, []byte("ExampleAddon: disabled\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := installAddonArchive(root, addonZIP(t, map[string]string{
		"repository-main/ExampleAddon/ExampleAddon.toc": "## Interface: 30300\n## Version: 1.2.3\nExampleAddon.lua\n",
		"repository-main/ExampleAddon/ExampleAddon.lua": "print('safe')\n",
	}), "main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Directories, ",") != "ExampleAddon" || result.Version != "1.2.3" {
		t.Fatalf("unexpected install result: %#v", result)
	}
	if content, err := os.ReadFile(filepath.Join(root, "Interface", "AddOns", "ExampleAddon", "ExampleAddon.lua")); err != nil || string(content) != "print('safe')\n" {
		t.Fatalf("addon file was not installed: content=%q err=%v", content, err)
	}
	settings, err := os.ReadFile(accountSettings)
	if err != nil || !strings.Contains(string(settings), "ExampleAddon: enabled") || strings.Contains(string(settings), "ExampleAddon: disabled") {
		t.Fatalf("addon was not enabled: content=%q err=%v", settings, err)
	}
}

func TestInstallAddonArchiveBacksUpExistingAddon(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "Interface", "AddOns", "ExampleAddon")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "old.lua"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := installAddonArchive(root, addonZIP(t, map[string]string{
		"ExampleAddon/ExampleAddon.toc": "## Interface: 30300\n## Version: 2.0\nnew.lua\n",
		"ExampleAddon/new.lua":          "new",
	}), "2.0")
	if err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(root, ".swp-backup", "addons", "*", "ExampleAddon", "old.lua"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("existing addon was not backed up: backups=%v err=%v", backups, err)
	}
	if _, err := os.Stat(filepath.Join(existing, "new.lua")); err != nil {
		t.Fatalf("replacement addon was not activated: %v", err)
	}
}

func TestInstallAddonArchiveRejectsUnsafeOrIncompatibleContent(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"traversal", map[string]string{"../outside.lua": "bad", "Addon/Addon.toc": "## Interface: 30300"}, "unsafe path"},
		{"executable", map[string]string{"Addon/Addon.toc": "## Interface: 30300", "Addon/payload.exe": "bad"}, "blocked executable"},
		{"wrong interface", map[string]string{"Addon/Addon.toc": "## Interface: 30400"}, "Interface 30300"},
		{"managed addon", map[string]string{"SWP/SWP.toc": "## Interface: 30300"}, "Interface 30300"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := installAddonArchive(t.TempDir(), addonZIP(t, test.files), "test")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMaddonsCatalogueKeepsOnlyLichKingEntriesAndSource(t *testing.T) {
	items := maddonsAddons([]maddonsEntry{
		{Title: "Quest Helper", FileName: "QuestHelper", Description: "Questing", Expansion: []string{"Lichking"}},
		{Title: "Retail Addon", FileName: "Retail", Expansion: []string{"Retail"}},
		{Title: "Unsafe", FileName: "../Unsafe", Expansion: []string{"Lichking"}},
	})
	if len(items) != 1 || items[0].Name != "Quest Helper" || items[0].Version != "3.3.5a" || items[0].Source != "Maddons Manager" {
		t.Fatalf("unexpected catalogue entries: %#v", items)
	}
	if !strings.Contains(items[0].DownloadURL, "QuestHelper/QuestHelper.zip") {
		t.Fatalf("unexpected download URL: %s", items[0].DownloadURL)
	}
}

func TestFilterAddonsMatchesAllSearchTerms(t *testing.T) {
	items := []Addon{
		{Name: "Quest Helper", Description: "Map objectives", Source: "Maddons Manager"},
		{Name: "Quest Log", Description: "No map", Source: "GitHub"},
	}
	filtered := filterAddons(items, "quest map")
	if len(filtered) != 2 {
		t.Fatalf("source metadata should participate in filtering: %#v", filtered)
	}
	filtered = filterAddons(items, "helper map")
	if len(filtered) != 1 || filtered[0].Name != "Quest Helper" {
		t.Fatalf("unexpected filtered results: %#v", filtered)
	}
}
