package client

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	LauncherVersion      = "2.4.50"
	selfUpdateHelperFlag = "--swp-apply-update"
)

func LauncherUpdateAvailable(manifest Manifest) bool {
	return manifest.Launcher != nil && compareVersions(manifest.Launcher.Version, LauncherVersion) > 0
}

func StartLauncherUpdate(manifest Manifest) (bool, error) {
	return StartLauncherUpdateWithProgress(manifest, nil)
}

func StartLauncherUpdateWithProgress(manifest Manifest, progress func(string)) (bool, error) {
	if !LauncherUpdateAvailable(manifest) {
		return false, nil
	}
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}
	release := manifest.Launcher
	report("Downloading SWP Launcher " + release.Version + "…")
	data, err := downloadVerified(release.URL, release.SHA256, release.Size, "launcher "+release.Version)
	if err != nil {
		return false, err
	}
	report("Download complete. Verifying the signed update…")
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return false, fmt.Errorf("locate update cache: %w", err)
	}
	updateDir := filepath.Join(cacheRoot, "SWP", "Launcher", "updates")
	if err = os.MkdirAll(updateDir, 0o755); err != nil {
		return false, fmt.Errorf("create update cache: %w", err)
	}
	staged := filepath.Join(updateDir, "SWPLauncher-"+safeVersion(release.Version)+".exe")
	if err = writeAtomic(staged, data); err != nil {
		return false, fmt.Errorf("stage launcher update: %w", err)
	}
	report("Signature verified. Preparing the updater…")
	current, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("locate current launcher: %w", err)
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return false, fmt.Errorf("resolve current launcher: %w", err)
	}
	permissionProbe, err := os.CreateTemp(filepath.Dir(current), ".swp-write-test-*.tmp")
	if err != nil {
		return false, fmt.Errorf("launcher directory is not writable: %w", err)
	}
	probePath := permissionProbe.Name()
	_ = permissionProbe.Close()
	_ = os.Remove(probePath)
	command := exec.Command(staged, selfUpdateHelperFlag, strconv.Itoa(os.Getpid()), current)
	if err = command.Start(); err != nil {
		return false, fmt.Errorf("start launcher updater: %w", err)
	}
	report("Updater ready. Switching to the installation window…")
	return true, nil
}

// RunSelfUpdateHelper handles the private invocation used by a downloaded,
// signature-verified launcher to replace the older executable after it exits.
func RunSelfUpdateHelper(args []string) bool {
	return RunSelfUpdateHelperWithProgress(args, nil)
}

// RunSelfUpdateHelperWithProgress applies a staged launcher update while
// reporting user-facing state changes to the helper UI.
func RunSelfUpdateHelperWithProgress(args []string, progress func(string)) bool {
	if len(args) == 0 || args[0] != selfUpdateHelperFlag {
		return false
	}
	if len(args) != 3 {
		return true
	}
	parentPID, err := strconv.Atoi(args[1])
	if err != nil || parentPID <= 0 {
		return true
	}
	target, err := filepath.Abs(args[2])
	if err != nil || !strings.EqualFold(filepath.Ext(target), ".exe") {
		return true
	}
	source, err := os.Executable()
	if err != nil {
		return true
	}
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}
	report("Waiting for the current launcher to finish…")
	if err = waitForProcessExit(parentPID, 60*time.Second); err != nil {
		report("Could not close the old launcher: " + err.Error())
		time.Sleep(8 * time.Second)
		return true
	}
	if err = replaceWhenUnlockedWithProgress(source, target, report); err != nil {
		report(fmt.Sprintf("Could not replace %s: %v", target, err))
		time.Sleep(8 * time.Second)
		_ = exec.Command(target).Start()
		return true
	}
	for seconds := 3; seconds > 0; seconds-- {
		report(fmt.Sprintf("Update installed successfully. Restarting in %d…", seconds))
		time.Sleep(time.Second)
	}
	report("Restarting SWP Launcher now…")
	_ = exec.Command(target).Start()
	return true
}

func replaceWhenUnlocked(source, target string) error {
	return replaceWhenUnlockedWithProgress(source, target, nil)
}

func replaceWhenUnlockedWithProgress(source, target string, progress func(string)) error {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}
	report("Preparing the verified launcher update…")
	update, err := os.CreateTemp(filepath.Dir(target), ".swp-launcher-*.new")
	if err != nil {
		return err
	}
	updatePath := update.Name()
	defer os.Remove(updatePath)
	sourceFile, err := os.Open(source)
	if err != nil {
		update.Close()
		return err
	}
	_, copyErr := io.Copy(update, sourceFile)
	closeSourceErr := sourceFile.Close()
	closeUpdateErr := update.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSourceErr != nil {
		return closeSourceErr
	}
	if closeUpdateErr != nil {
		return closeUpdateErr
	}

	backup := target + ".previous-" + time.Now().UTC().Format("20060102-150405")
	deadline := time.Now().Add(30 * time.Second)
	report("Installing the new launcher…")
	for {
		err = os.Rename(target, backup)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("old launcher did not exit: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err = os.Rename(updatePath, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	if err = verifySameFile(source, target); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
		return fmt.Errorf("verify installed launcher: %w", err)
	}
	return nil
}

func verifySameFile(leftPath, rightPath string) error {
	left, err := os.ReadFile(leftPath)
	if err != nil {
		return err
	}
	right, err := os.ReadFile(rightPath)
	if err != nil {
		return err
	}
	if sha256.Sum256(left) != sha256.Sum256(right) {
		return fmt.Errorf("installed executable does not match the verified download")
	}
	return nil
}

func writeAtomic(target string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".swp-download-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, target)
}

func safeVersion(version string) string {
	return strings.Map(func(character rune) rune {
		if character >= '0' && character <= '9' || character == '.' || character == '-' {
			return character
		}
		return '-'
	}, version)
}

func compareVersions(left, right string) int {
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	count := len(leftParts)
	if len(rightParts) > count {
		count = len(rightParts)
	}
	for index := 0; index < count; index++ {
		leftValue, rightValue := 0, 0
		if index < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[index])
		}
		if index < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[index])
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}
