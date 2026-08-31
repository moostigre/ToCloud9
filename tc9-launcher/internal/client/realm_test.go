package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureRealmUpdatesLocaleAndConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Wow.exe"), make([]byte, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Data", "enUS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data", "enUS", "locale-enUS.MPQ"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "WTF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WTF", "Config.wtf"), []byte("SET realmName \"Old Realm\"\r\nSET gxApi \"d3d9\"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := configureRealmFiles(root, "163.172.51.144", "PTR"); err != nil {
		t.Fatal(err)
	}
	realmlist, err := os.ReadFile(filepath.Join(root, "Data", "enUS", "realmlist.wtf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(realmlist) != "set realmlist 163.172.51.144\r\n" {
		t.Fatalf("unexpected realmlist: %q", realmlist)
	}
	config, err := os.ReadFile(filepath.Join(root, "WTF", "Config.wtf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `SET realmName "PTR"`) || !strings.Contains(string(config), `SET gxApi "d3d9"`) {
		t.Fatalf("unexpected config: %q", config)
	}
}

func TestConfigureRealmRejectsInjectedRealmlist(t *testing.T) {
	if validRealmlist("127.0.0.1\nSET x") {
		t.Fatal("realmlist containing a second setting was accepted")
	}
}

func TestValidRealmlistAcceptsHostnameWithPort(t *testing.T) {
	if !validRealmlist("logon.expanded.space:32767") {
		t.Fatal("hostname-based realmlist was rejected")
	}
}

func TestConfigureRealmWritesHostnameWithPort(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Data", "enUS"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := configureRealmFiles(root, "logon.expanded.space:32767", "PTR"); err != nil {
		t.Fatal(err)
	}
	realmlist, err := os.ReadFile(filepath.Join(root, "Data", "enUS", "realmlist.wtf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(realmlist) != "set realmlist logon.expanded.space:32767\r\n" {
		t.Fatalf("unexpected realmlist: %q", realmlist)
	}
}

func TestConfigureRealmAcceptsProductionName(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Data", "enUS"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := configureRealmFiles(root, "163.172.51.144", "Production"); err != nil {
		t.Fatalf("Production realm name was rejected: %v", err)
	}

	config, err := os.ReadFile(filepath.Join(root, "WTF", "Config.wtf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `SET realmName "Production"`) {
		t.Fatalf("unexpected config: %q", config)
	}
}
