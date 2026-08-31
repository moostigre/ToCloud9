package client

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maddonsCatalogURL       = "https://raw.githubusercontent.com/PentSec/API-MADDONS/main/API/Maddons.json"
	maddonsArchiveBaseURL   = "https://raw.githubusercontent.com/PentSec/API-MADDONS/main/API/Addons/Lichking/"
	githubRepositorySearch  = "https://api.github.com/search/repositories"
	maximumAddonArchiveSize = 256 << 20
	maximumAddonInstallSize = 512 << 20
	maximumAddonFiles       = 10000
	maximumAddonTOCSize     = 1 << 20
)

type Addon struct {
	ID          string
	Name        string
	Version     string
	Source      string
	Description string
	ProjectURL  string
	DownloadURL string
}

type AddonInstallResult struct {
	Directories []string
	Version     string
}

type maddonsEntry struct {
	Title       string   `json:"title"`
	FileName    string   `json:"file_name"`
	Description string   `json:"description"`
	Expansion   []string `json:"expansion"`
}

type githubSearchResponse struct {
	Items []struct {
		FullName      string    `json:"full_name"`
		Name          string    `json:"name"`
		Description   string    `json:"description"`
		HTMLURL       string    `json:"html_url"`
		DefaultBranch string    `json:"default_branch"`
		UpdatedAt     time.Time `json:"updated_at"`
		Archived      bool      `json:"archived"`
		Size          int64     `json:"size"`
	} `json:"items"`
}

var maddonsCache struct {
	sync.Mutex
	loaded time.Time
	items  []Addon
}

// SearchAddons returns 3.3.5a archives from the dedicated Maddons catalogue
// and, when a query is supplied, maintained backports found on GitHub.
func SearchAddons(query string) ([]Addon, error) {
	query = strings.TrimSpace(query)
	maddons, maddonsErr := fetchMaddonsCatalog()
	results := append([]Addon(nil), maddons...)
	var githubErr error
	if len(query) >= 2 {
		var github []Addon
		github, githubErr = searchGitHubAddons(query)
		results = append(results, github...)
	}
	if maddonsErr != nil && githubErr != nil {
		return nil, fmt.Errorf("addon repositories are unavailable: Maddons Manager: %v; GitHub: %v", maddonsErr, githubErr)
	}
	if maddonsErr != nil && len(results) == 0 {
		return nil, fmt.Errorf("Maddons Manager catalogue is unavailable: %w", maddonsErr)
	}
	if githubErr != nil && len(results) == 0 {
		return nil, fmt.Errorf("GitHub addon search is unavailable: %w", githubErr)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if !strings.EqualFold(results[i].Name, results[j].Name) {
			return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
		}
		return results[i].Source < results[j].Source
	})
	return results, nil
}

func fetchMaddonsCatalog() ([]Addon, error) {
	maddonsCache.Lock()
	defer maddonsCache.Unlock()
	if len(maddonsCache.items) > 0 && time.Since(maddonsCache.loaded) < 15*time.Minute {
		return append([]Addon(nil), maddonsCache.items...), nil
	}
	var entries []maddonsEntry
	if err := fetchAddonJSON(maddonsCatalogURL, 2<<20, &entries); err != nil {
		return nil, err
	}
	items := maddonsAddons(entries)
	if len(items) == 0 {
		return nil, errors.New("catalogue contains no Lich King addons")
	}
	maddonsCache.items = append([]Addon(nil), items...)
	maddonsCache.loaded = time.Now()
	return items, nil
}

func maddonsAddons(entries []maddonsEntry) []Addon {
	items := make([]Addon, 0, len(entries))
	for _, entry := range entries {
		if !containsFold(entry.Expansion, "Lichking") || !safeAddonComponent(entry.FileName) {
			continue
		}
		name := strings.TrimSpace(entry.Title)
		if name == "" {
			name = entry.FileName
		}
		escaped := url.PathEscape(entry.FileName)
		items = append(items, Addon{
			ID:          "maddons:" + strings.ToLower(entry.FileName),
			Name:        name,
			Version:     "3.3.5a",
			Source:      "Maddons Manager",
			Description: strings.TrimSpace(entry.Description),
			ProjectURL:  "https://maddonsmanager.github.io/",
			DownloadURL: maddonsArchiveBaseURL + escaped + "/" + escaped + ".zip",
		})
	}
	return items
}

func searchGitHubAddons(query string) ([]Addon, error) {
	terms := strings.TrimSpace(query) + " 3.3.5a addon in:name,description,readme archived:false fork:false"
	endpoint := githubRepositorySearch + "?per_page=20&sort=stars&order=desc&q=" + url.QueryEscape(terms)
	var response githubSearchResponse
	if err := fetchAddonJSON(endpoint, 2<<20, &response); err != nil {
		return nil, err
	}
	items := make([]Addon, 0, len(response.Items))
	for _, repository := range response.Items {
		if repository.Archived || repository.FullName == "" || repository.DefaultBranch == "" || repository.Size > maximumAddonArchiveSize>>10 {
			continue
		}
		version := repository.DefaultBranch
		if !repository.UpdatedAt.IsZero() {
			version += " · " + repository.UpdatedAt.Format("2006-01-02")
		}
		items = append(items, Addon{
			ID:          "github:" + strings.ToLower(repository.FullName),
			Name:        repository.Name,
			Version:     version,
			Source:      "GitHub",
			Description: strings.TrimSpace(repository.Description),
			ProjectURL:  repository.HTMLURL,
			DownloadURL: "https://api.github.com/repos/" + repository.FullName + "/zipball/" + url.PathEscape(repository.DefaultBranch),
		})
	}
	return filterAddons(items, query), nil
}

func filterAddons(items []Addon, query string) []Addon {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]Addon(nil), items...)
	}
	words := strings.Fields(query)
	filtered := make([]Addon, 0, len(items))
	for _, addon := range items {
		haystack := strings.ToLower(strings.Join([]string{addon.Name, addon.Description, addon.Source}, " "))
		matched := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, addon)
		}
	}
	return filtered
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

func fetchAddonJSON(endpoint string, maximum int64, target any) error {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json, application/json")
	request.Header.Set("User-Agent", "SWPLauncher/"+LauncherVersion)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := addonHTTPClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("repository returned %s", response.Status)
	}
	limited := io.LimitReader(response.Body, maximum+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(body)) > maximum {
		return errors.New("repository response is too large")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode repository response: %w", err)
	}
	return nil
}

func addonHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 3 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 || request.URL.Scheme != "https" || !trustedAddonHost(request.URL.Hostname()) {
				return errors.New("addon download redirected outside trusted repositories")
			}
			return nil
		},
	}
}

func trustedAddonHost(host string) bool {
	switch strings.ToLower(host) {
	case "api.github.com", "codeload.github.com", "raw.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

// InstallAddon downloads, validates, stages, and activates a third-party addon
// below Interface/AddOns. Only archives declaring interface 30300 are accepted.
func InstallAddon(root string, addon Addon, progress func(UpdateProgress)) (AddonInstallResult, error) {
	if _, err := Validate(root); err != nil {
		return AddonInstallResult{}, err
	}
	archive, err := downloadAddonArchive(addon, progress)
	if err != nil {
		return AddonInstallResult{}, err
	}
	return installAddonArchive(root, archive, addon.Version)
}

func downloadAddonArchive(addon Addon, progress func(UpdateProgress)) ([]byte, error) {
	parsed, err := url.Parse(addon.DownloadURL)
	if err != nil || parsed.Scheme != "https" || !trustedAddonHost(parsed.Hostname()) {
		return nil, errors.New("addon download URL is not from a trusted repository")
	}
	request, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "SWPLauncher/"+LauncherVersion)
	response, err := addonHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", addon.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: repository returned %s", addon.Name, response.Status)
	}
	if response.ContentLength > maximumAddonArchiveSize {
		return nil, errors.New("addon archive exceeds the 256 MiB safety limit")
	}
	reader := io.Reader(io.LimitReader(response.Body, maximumAddonArchiveSize+1))
	if progress != nil {
		progress(UpdateProgress{Message: "Downloading " + addon.Name + "…", TotalBytes: response.ContentLength})
		reader = &progressReader{reader: reader, progress: func(downloaded int64) {
			progress(UpdateProgress{Message: "Downloading " + addon.Name + "…", BytesDownloaded: downloaded, TotalBytes: response.ContentLength})
		}}
	}
	archive, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", addon.Name, err)
	}
	if len(archive) > maximumAddonArchiveSize {
		return nil, errors.New("addon archive exceeds the 256 MiB safety limit")
	}
	if progress != nil {
		progress(UpdateProgress{Message: "Validating 3.3.5a addon files…", BytesDownloaded: int64(len(archive)), TotalBytes: int64(len(archive))})
	}
	return archive, nil
}

type addonArchiveRoot struct {
	sourceDir string
	targetDir string
	version   string
}

func installAddonArchive(root string, archive []byte, fallbackVersion string) (AddonInstallResult, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return AddonInstallResult{}, errors.New("addon download is not a valid ZIP archive")
	}
	if len(zipReader.File) == 0 || len(zipReader.File) > maximumAddonFiles {
		return AddonInstallResult{}, errors.New("addon archive has an unsafe file count")
	}
	var totalSize uint64
	cleanPaths := make(map[*zip.File]string, len(zipReader.File))
	for _, file := range zipReader.File {
		clean, cleanErr := cleanAddonArchivePath(file.Name)
		if cleanErr != nil {
			return AddonInstallResult{}, cleanErr
		}
		if file.Mode()&os.ModeSymlink != 0 || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return AddonInstallResult{}, fmt.Errorf("addon archive contains unsupported file %q", file.Name)
		}
		if unsafeAddonFile(clean) {
			return AddonInstallResult{}, fmt.Errorf("addon archive contains blocked executable file %q", file.Name)
		}
		totalSize += file.UncompressedSize64
		if totalSize > maximumAddonInstallSize {
			return AddonInstallResult{}, errors.New("addon archive expands beyond the 512 MiB safety limit")
		}
		cleanPaths[file] = clean
	}
	roots, err := discoverAddonRoots(zipReader.File, cleanPaths, fallbackVersion)
	if err != nil {
		return AddonInstallResult{}, err
	}
	addonsDir := filepath.Join(root, "Interface", "AddOns")
	if err := os.MkdirAll(addonsDir, 0o755); err != nil {
		return AddonInstallResult{}, fmt.Errorf("create addon directory: %w", err)
	}
	stageRoot, err := os.MkdirTemp(addonsDir, ".swp-addon-stage-")
	if err != nil {
		return AddonInstallResult{}, fmt.Errorf("create addon staging directory: %w", err)
	}
	defer os.RemoveAll(stageRoot)
	for _, addonRoot := range roots {
		if err := os.MkdirAll(filepath.Join(stageRoot, addonRoot.targetDir), 0o755); err != nil {
			return AddonInstallResult{}, err
		}
	}
	destinations := make(map[string]bool, len(zipReader.File))
	for _, file := range zipReader.File {
		clean := cleanPaths[file]
		addonRoot, relative, found := archiveDestination(clean, roots)
		if !found || relative == "" || file.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(stageRoot, addonRoot.targetDir, filepath.FromSlash(relative))
		destinationKey := strings.ToLower(filepath.Clean(target))
		if destinations[destinationKey] {
			return AddonInstallResult{}, fmt.Errorf("addon archive contains duplicate file path %q", file.Name)
		}
		destinations[destinationKey] = true
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return AddonInstallResult{}, err
		}
		if err := extractAddonFile(file, target); err != nil {
			return AddonInstallResult{}, err
		}
	}
	backupRoot := filepath.Join(root, ".swp-backup", "addons", time.Now().UTC().Format("20060102-150405.000000000"))
	type activatedAddon struct{ target, backup string }
	activated := make([]activatedAddon, 0, len(roots))
	rollback := func() {
		for index := len(activated) - 1; index >= 0; index-- {
			_ = os.RemoveAll(activated[index].target)
			if activated[index].backup != "" {
				_ = os.Rename(activated[index].backup, activated[index].target)
			}
		}
	}
	for _, addonRoot := range roots {
		target := filepath.Join(addonsDir, addonRoot.targetDir)
		staged := filepath.Join(stageRoot, addonRoot.targetDir)
		backup := ""
		if info, statErr := os.Lstat(target); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				rollback()
				return AddonInstallResult{}, fmt.Errorf("refusing to replace symlinked addon directory %s", addonRoot.targetDir)
			}
			if err := os.MkdirAll(backupRoot, 0o755); err != nil {
				rollback()
				return AddonInstallResult{}, err
			}
			backup = filepath.Join(backupRoot, addonRoot.targetDir)
			if err := os.Rename(target, backup); err != nil {
				rollback()
				return AddonInstallResult{}, fmt.Errorf("back up existing addon %s: %w", addonRoot.targetDir, err)
			}
		} else if !os.IsNotExist(statErr) {
			rollback()
			return AddonInstallResult{}, statErr
		}
		if err := os.Rename(staged, target); err != nil {
			if backup != "" {
				_ = os.Rename(backup, target)
			}
			rollback()
			return AddonInstallResult{}, fmt.Errorf("activate addon %s: %w", addonRoot.targetDir, err)
		}
		activated = append(activated, activatedAddon{target: target, backup: backup})
	}
	directories := make([]string, 0, len(roots))
	version := fallbackVersion
	for _, addonRoot := range roots {
		directories = append(directories, addonRoot.targetDir)
		if addonRoot.version != "" {
			version = addonRoot.version
		}
	}
	if err := EnableAddons(root, directories); err != nil {
		rollback()
		return AddonInstallResult{}, err
	}
	return AddonInstallResult{Directories: directories, Version: version}, nil
}

func discoverAddonRoots(files []*zip.File, cleanPaths map[*zip.File]string, fallbackVersion string) ([]addonArchiveRoot, error) {
	roots := map[string]addonArchiveRoot{}
	usedTargets := map[string]bool{}
	for _, file := range files {
		clean := cleanPaths[file]
		if file.FileInfo().IsDir() || !strings.EqualFold(path.Ext(clean), ".toc") || file.UncompressedSize64 > maximumAddonTOCSize {
			continue
		}
		content, err := readZipFile(file, maximumAddonTOCSize)
		if err != nil {
			return nil, err
		}
		metadata := parseAddonTOC(string(content))
		if !metadata.compatible {
			continue
		}
		sourceDir := path.Dir(clean)
		if sourceDir == "." {
			sourceDir = ""
		}
		target := strings.TrimSuffix(path.Base(clean), path.Ext(clean))
		if !safeAddonComponent(target) || managedAddonDirectory(target) {
			continue
		}
		key := strings.ToLower(sourceDir)
		if existing, found := roots[key]; found {
			if existing.version == "" && metadata.version != "" {
				existing.version = metadata.version
				roots[key] = existing
			}
			continue
		}
		if usedTargets[strings.ToLower(target)] {
			return nil, fmt.Errorf("addon archive contains duplicate target directory %q", target)
		}
		usedTargets[strings.ToLower(target)] = true
		version := metadata.version
		if version == "" {
			version = fallbackVersion
		}
		roots[key] = addonArchiveRoot{sourceDir: sourceDir, targetDir: target, version: version}
	}
	if len(roots) == 0 {
		return nil, errors.New("addon archive does not contain a WoW 3.3.5a TOC (Interface 30300)")
	}
	items := make([]addonArchiveRoot, 0, len(roots))
	for _, candidate := range roots {
		nested := false
		for _, parent := range roots {
			if parent.sourceDir != candidate.sourceDir && (parent.sourceDir == "" || strings.HasPrefix(candidate.sourceDir, parent.sourceDir+"/")) {
				nested = true
				break
			}
		}
		if !nested {
			items = append(items, candidate)
		}
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].targetDir) < strings.ToLower(items[j].targetDir) })
	return items, nil
}

type addonTOC struct {
	compatible bool
	version    string
}

func parseAddonTOC(content string) addonTOC {
	var metadata addonTOC
	content = strings.TrimPrefix(content, "\ufeff")
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "## interface:"):
			values := strings.FieldsFunc(strings.TrimSpace(line[len("## Interface:"):]), func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t'
			})
			for _, value := range values {
				if value == "30300" {
					metadata.compatible = true
				}
			}
		case strings.HasPrefix(lower, "## version:"):
			metadata.version = strings.TrimSpace(line[len("## Version:"):])
			if len(metadata.version) > 80 {
				metadata.version = metadata.version[:80]
			}
		}
	}
	return metadata
}

func archiveDestination(clean string, roots []addonArchiveRoot) (addonArchiveRoot, string, bool) {
	for _, addonRoot := range roots {
		if addonRoot.sourceDir == "" {
			return addonRoot, clean, true
		}
		if clean == addonRoot.sourceDir {
			return addonRoot, "", true
		}
		if strings.HasPrefix(clean, addonRoot.sourceDir+"/") {
			return addonRoot, strings.TrimPrefix(clean, addonRoot.sourceDir+"/"), true
		}
	}
	return addonArchiveRoot{}, "", false
}

func cleanAddonArchivePath(value string) (string, error) {
	value = strings.ReplaceAll(value, "\\", "/")
	clean := path.Clean(value)
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", fmt.Errorf("addon archive contains unsafe path %q", value)
	}
	return clean, nil
}

func safeAddonComponent(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || len(value) > 120 || strings.TrimRight(value, ". ") != value ||
		strings.ContainsAny(value, `/\\<>:"|?*`) || strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(value, path.Ext(value)))
	if base == "con" || base == "prn" || base == "aux" || base == "nul" ||
		(len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9') {
		return false
	}
	return true
}

func managedAddonDirectory(value string) bool {
	switch strings.ToLower(value) {
	case "swp", "swpmultispecs", "swpheroicui":
		return true
	default:
		return false
	}
}

func unsafeAddonFile(value string) bool {
	switch strings.ToLower(path.Ext(value)) {
	case ".bat", ".cmd", ".com", ".dll", ".exe", ".lnk", ".msi", ".ps1", ".scr":
		return true
	default:
		return false
	}
}

func extractAddonFile(file *zip.File, target string) error {
	content, err := readZipFile(file, int64(file.UncompressedSize64))
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return fmt.Errorf("extract %s: %w", file.Name, err)
	}
	return nil
}

func readZipFile(file *zip.File, maximum int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum || uint64(len(content)) != file.UncompressedSize64 {
		return nil, fmt.Errorf("addon file %q exceeds its declared size", file.Name)
	}
	return content, nil
}
