package client

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ManifestURL            = "https://launcher.expanded.space/downloads/swp/v2/manifest.json"
	publicKeyBase64        = "c2h94zpGvd/K5YmlvYRkMaoo1y3T9V/a6tNioBICwFM="
	maximumManagedFileSize = 100 << 20
)

type SignedManifest struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type Manifest struct {
	Version            string             `json:"version"`
	Files              []ManifestFile     `json:"files"`
	Launcher           *LauncherRelease   `json:"launcher,omitempty"`
	Realms             []RealmEnvironment `json:"realms,omitempty"`
	DefaultEnvironment string             `json:"default_environment,omitempty"`
}

type RealmEnvironment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Realmlist string `json:"realmlist"`
	RealmName string `json:"realm_name"`
}

type LauncherRelease struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type downloadedFile struct {
	ManifestFile
	data []byte
}

type ContentStatus struct {
	Version      string
	Current      bool
	ChangedFiles int
}

type UpdateProgress struct {
	Message         string
	BytesDownloaded int64
	TotalBytes      int64
}

// CheckContent compares managed local files with the signed manifest without
// downloading or changing anything in the game directory.
func CheckContent(root string) (ContentStatus, error) {
	if _, err := Validate(root); err != nil {
		return ContentStatus{}, err
	}
	manifest, err := FetchManifest()
	if err != nil {
		return ContentStatus{}, err
	}
	status := ContentStatus{Version: manifest.Version, Current: true}
	for _, file := range manifest.Files {
		localPath := filepath.Join(root, filepath.FromSlash(file.Path))
		data, readErr := os.ReadFile(localPath)
		if readErr != nil || int64(len(data)) != file.Size {
			status.Current = false
			status.ChangedFiles++
			continue
		}
		digest := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), file.SHA256) {
			status.Current = false
			status.ChangedFiles++
		}
	}
	return status, nil
}

func FetchManifest() (Manifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	request, err := http.NewRequest(http.MethodGet, ManifestURL+"?t="+strconv.FormatInt(time.Now().UnixNano(), 10), nil)
	if err != nil {
		return Manifest{}, err
	}
	// A launch check must observe a release published while this launcher is
	// already open. Do not let an intermediary or OS cache reuse the manifest
	// fetched at startup.
	request.Header.Set("Cache-Control", "no-cache, no-store, max-age=0")
	request.Header.Set("Pragma", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return Manifest{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("manifest server returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Manifest{}, err
	}
	var signed SignedManifest
	if err = json.Unmarshal(body, &signed); err != nil {
		return Manifest{}, fmt.Errorf("decode signed manifest: %w", err)
	}
	payload, err := base64.StdEncoding.DecodeString(signed.Payload)
	if err != nil {
		return Manifest{}, errors.New("manifest payload is not valid base64")
	}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil {
		return Manifest{}, errors.New("manifest signature is not valid base64")
	}
	publicKey, _ := base64.StdEncoding.DecodeString(publicKeyBase64)
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, payload, signature) {
		return Manifest{}, errors.New("manifest signature verification failed")
	}
	var manifest Manifest
	if err = json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest payload: %w", err)
	}
	if manifest.Version == "" || len(manifest.Files) == 0 {
		return Manifest{}, errors.New("manifest has no version or files")
	}
	for _, file := range manifest.Files {
		if err = validateManagedPath(file.Path); err != nil {
			return Manifest{}, err
		}
		if file.Size < 0 || file.Size > maximumManagedFileSize || len(file.SHA256) != 64 {
			return Manifest{}, fmt.Errorf("invalid metadata for %s", file.Path)
		}
	}
	if manifest.Launcher != nil {
		if manifest.Launcher.Version == "" || manifest.Launcher.URL == "" ||
			manifest.Launcher.Size <= 0 || manifest.Launcher.Size > maximumManagedFileSize ||
			len(manifest.Launcher.SHA256) != 64 {
			return Manifest{}, errors.New("invalid launcher update metadata")
		}
	}
	seenRealms := make(map[string]bool, len(manifest.Realms))
	for _, realm := range manifest.Realms {
		if realm.ID == "" || realm.Name == "" || realm.RealmName == "" || !validRealmlist(realm.Realmlist) {
			return Manifest{}, errors.New("invalid realm environment metadata")
		}
		if seenRealms[realm.ID] {
			return Manifest{}, fmt.Errorf("duplicate realm environment %q", realm.ID)
		}
		seenRealms[realm.ID] = true
	}
	if len(manifest.Realms) > 0 && !seenRealms[manifest.DefaultEnvironment] {
		return Manifest{}, errors.New("default realm environment is missing")
	}
	return manifest, nil
}

func validRealmlist(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 255 && len(strings.Fields(value)) == 1 && !strings.ContainsAny(value, "\"\r\n")
}

func validateManagedPath(path string) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe managed path %q", path)
	}
	normalized := strings.ToLower(filepath.ToSlash(clean))
	if normalized == "data/patch-t.mpq" ||
		strings.HasPrefix(normalized, "interface/addons/swp/") ||
		strings.HasPrefix(normalized, "interface/addons/swpmultispecs/") ||
		strings.HasPrefix(normalized, "interface/addons/swpheroicui/") {
		return nil
	}
	return fmt.Errorf("manifest path is outside the launcher allowlist: %s", path)
}

func downloadManifestFiles(manifest Manifest) ([]downloadedFile, error) {
	return downloadManifestFilesWithProgress(manifest, nil)
}

func downloadManifestFilesWithProgress(manifest Manifest, progress func(string)) ([]downloadedFile, error) {
	return downloadManifestFilesWithDetailedProgress(manifest, func(update UpdateProgress) {
		if progress != nil {
			progress(update.Message)
		}
	})
}

func downloadManifestFilesWithDetailedProgress(manifest Manifest, progress func(UpdateProgress)) ([]downloadedFile, error) {
	files := make([]downloadedFile, 0, len(manifest.Files))
	var totalBytes int64
	for _, file := range manifest.Files {
		totalBytes += file.Size
	}
	var completedBytes int64
	for index, file := range manifest.Files {
		message := fmt.Sprintf("Downloading update file %d of %d: %s", index+1, len(manifest.Files), filepath.Base(file.Path))
		if progress != nil {
			progress(UpdateProgress{Message: message, BytesDownloaded: completedBytes, TotalBytes: totalBytes})
		}
		data, err := downloadVerifiedWithProgress(file.URL, file.SHA256, file.Size, file.Path, func(downloaded int64) {
			if progress != nil {
				progress(UpdateProgress{Message: message, BytesDownloaded: completedBytes + downloaded, TotalBytes: totalBytes})
			}
		})
		if err != nil {
			return nil, err
		}
		files = append(files, downloadedFile{ManifestFile: file, data: data})
		completedBytes += file.Size
	}
	return files, nil
}

func downloadVerified(url, expectedSHA string, expectedSize int64, label string) ([]byte, error) {
	return downloadVerifiedWithProgress(url, expectedSHA, expectedSize, label, nil)
}

func downloadVerifiedWithProgress(url, expectedSHA string, expectedSize int64, label string, progress func(int64)) ([]byte, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: server returned %s", label, response.Status)
	}
	reader := io.Reader(io.LimitReader(response.Body, maximumManagedFileSize+1))
	if progress != nil {
		reader = &progressReader{reader: reader, progress: progress}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	if int64(len(data)) != expectedSize {
		return nil, fmt.Errorf("download %s: expected %d bytes, received %d", label, expectedSize, len(data))
	}
	digest := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), expectedSHA) {
		return nil, fmt.Errorf("download %s failed SHA-256 verification", label)
	}
	return data, nil
}

type progressReader struct {
	reader     io.Reader
	downloaded int64
	progress   func(int64)
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.downloaded += int64(count)
		reader.progress(reader.downloaded)
	}
	return count, err
}

func installManagedFile(root, relativePath, backupRoot string, data []byte) (bool, error) {
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	if current, err := os.ReadFile(target); err == nil && sha256.Sum256(current) == sha256.Sum256(data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".swp-update-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	backup := filepath.Join(backupRoot, filepath.FromSlash(relativePath))
	if _, err = os.Stat(target); err == nil {
		if err = os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return false, err
		}
		if err = os.Rename(target, backup); err != nil {
			return false, err
		}
	}
	if err = os.Rename(tmpName, target); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, target)
		}
		return false, err
	}
	return true, nil
}

func Update(root string) (string, error) {
	return UpdateWithProgress(root, nil)
}

func UpdateWithProgress(root string, progress func(string)) (string, error) {
	return UpdateWithDetailedProgress(root, func(update UpdateProgress) {
		if progress != nil {
			progress(update.Message)
		}
	})
}

func UpdateWithDetailedProgress(root string, progress func(UpdateProgress)) (string, error) {
	report := func(message string) {
		if progress != nil {
			progress(UpdateProgress{Message: message})
		}
	}
	report("Validating your World of Warcraft installation…")
	if _, err := Validate(root); err != nil {
		return "", err
	}
	report("Checking the signed SWP content manifest…")
	manifest, err := FetchManifest()
	if err != nil {
		return "", err
	}
	files, err := downloadManifestFilesWithDetailedProgress(manifest, progress)
	if err != nil {
		return "", err
	}
	backupRoot := filepath.Join(root, ".swp-backup", time.Now().UTC().Format("20060102-150405"))
	changed := 0
	for index, file := range files {
		report(fmt.Sprintf("Installing update file %d of %d: %s", index+1, len(files), filepath.Base(file.Path)))
		installed, installErr := installManagedFile(root, file.Path, backupRoot, file.data)
		if installErr != nil {
			return "", fmt.Errorf("install %s: %w", file.Path, installErr)
		}
		if installed {
			changed++
		}
	}
	for _, legacyName := range []string{"ToCloud9Client", "SWPClient"} {
		legacyAddon := filepath.Join(root, "Interface", "AddOns", legacyName)
		if _, statErr := os.Stat(legacyAddon); statErr != nil {
			continue
		}
		legacyBackup := filepath.Join(backupRoot, "Interface", "AddOns", legacyName)
		if err = os.MkdirAll(filepath.Dir(legacyBackup), 0o755); err != nil {
			return "", fmt.Errorf("prepare legacy addon backup: %w", err)
		}
		if err = os.Rename(legacyAddon, legacyBackup); err != nil {
			return "", fmt.Errorf("remove legacy addon: %w", err)
		}
		changed++
	}
	if changed == 0 {
		report("Verification complete. Your client is already up to date.")
		return "Client content " + manifest.Version + " is already current.", nil
	}
	report("Installation complete. Your client is ready to play.")
	return fmt.Sprintf("Installed client content %s (%d files updated).", manifest.Version, changed), nil
}
