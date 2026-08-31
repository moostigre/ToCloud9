package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	PatchName   = "patch-T.MPQ"
	RealmHost   = "logon.expanded.space"
	RealmPort   = "32767"
	ClientBuild = 12340
)

type Validation struct {
	Path    string
	Version string
}

func Validate(root string) (Validation, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == string(filepath.Separator) {
		return Validation{}, errors.New("select the World of Warcraft 3.3.5a installation folder")
	}
	wow := filepath.Join(root, "Wow.exe")
	data := filepath.Join(root, "Data")
	if info, err := os.Stat(wow); err != nil || info.IsDir() {
		return Validation{}, errors.New("Wow.exe was not found in the selected folder")
	}
	if info, err := os.Stat(data); err != nil || !info.IsDir() {
		return Validation{}, errors.New("the selected folder has no Data directory")
	}
	version, build, err := executableVersion(wow)
	if err != nil {
		return Validation{}, fmt.Errorf("cannot read Wow.exe version: %w", err)
	}
	if build != ClientBuild {
		return Validation{}, fmt.Errorf("unsupported client build %d (%s); expected 3.3.5a build %d", build, version, ClientBuild)
	}
	return Validation{Path: root, Version: version}, nil
}

func InstallPatch(root string, patch []byte) (string, error) {
	if _, err := Validate(root); err != nil {
		return "", err
	}
	dataDir := filepath.Join(root, "Data")
	target := filepath.Join(dataDir, PatchName)
	wanted := sha256.Sum256(patch)
	if current, err := os.ReadFile(target); err == nil && sha256.Sum256(current) == wanted {
		return "Patch is already current (SHA-256 " + hex.EncodeToString(wanted[:8]) + "…).", nil
	}

	tmp, err := os.CreateTemp(dataDir, ".swp-patch-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create staged patch: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(patch); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("stage patch: %w", err)
	}
	written, err := os.ReadFile(tmpName)
	if err != nil || sha256.Sum256(written) != wanted {
		return "", errors.New("staged patch failed SHA-256 verification")
	}

	backup := ""
	if _, err = os.Stat(target); err == nil {
		backupDir := filepath.Join(root, ".swp-backup", time.Now().UTC().Format("20060102-150405"))
		if err = os.MkdirAll(backupDir, 0o755); err != nil {
			return "", fmt.Errorf("create backup directory: %w", err)
		}
		backup = filepath.Join(backupDir, PatchName)
		if err = os.Rename(target, backup); err != nil {
			return "", fmt.Errorf("back up existing %s: %w", PatchName, err)
		}
	}
	if err = os.Rename(tmpName, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		return "", fmt.Errorf("activate patch: %w", err)
	}
	message := "Installed " + PatchName + " (SHA-256 " + hex.EncodeToString(wanted[:8]) + "…)."
	if backup != "" {
		message += " Previous patch backed up to " + backup + "."
	}
	return message, nil
}

func Launch(root string) error {
	if err := EnableManagedAddons(root); err != nil {
		return err
	}
	_, err := launchProcess(root)
	return err
}

// ConfigureRealm selects both the authentication endpoint and the realm that
// the 3.3.5a client should enter after authentication.
func ConfigureRealm(root, realmlist, realmName string) error {
	if _, err := Validate(root); err != nil {
		return err
	}
	return configureRealmFiles(root, realmlist, realmName)
}

func configureRealmFiles(root, realmlist, realmName string) error {
	if !validRealmlist(realmlist) {
		return errors.New("invalid realmlist")
	}
	realmName = strings.TrimSpace(realmName)
	if realmName == "" || strings.ContainsAny(realmName, "\"\r\n") {
		return errors.New("invalid realm name")
	}
	locales := []string{"enUS", "enGB", "deDE", "esES", "esMX", "frFR", "koKR", "ruRU", "zhCN", "zhTW"}
	written := 0
	for _, locale := range locales {
		directory := filepath.Join(root, "Data", locale)
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			continue
		}
		if err := os.WriteFile(filepath.Join(directory, "realmlist.wtf"), []byte("set realmlist "+strings.TrimSpace(realmlist)+"\r\n"), 0o644); err != nil {
			return fmt.Errorf("write %s realmlist: %w", locale, err)
		}
		written++
	}
	if written == 0 {
		return errors.New("no supported client locale directory was found")
	}
	configPath := filepath.Join(root, "WTF", "Config.wtf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, _ := os.ReadFile(configPath)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	setting := `SET realmName "` + realmName + `"`
	replaced := false
	for index, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "set realmname ") {
			lines[index], replaced = setting, true
		}
	}
	if !replaced {
		lines = append(lines, setting)
	}
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\r\n")), 0o600)
}

func clearItemQueryCache(root string) error {
	cacheRoot := filepath.Join(root, "Cache", "WDB")
	return filepath.WalkDir(cacheRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "itemcache.wdb") {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}

func ServerOnline() bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(RealmHost, RealmPort), 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func Copy(dst string, src io.Reader) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

func IsWindows() bool { return runtime.GOOS == "windows" }
