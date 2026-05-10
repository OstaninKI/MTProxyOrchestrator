package update

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Component identifies a managed binary.
type Component string

const (
	ComponentCLI       Component = "tgproxy-cli"
	ComponentPanel     Component = "tgproxy-panel"
	ComponentTeleproxy Component = "teleproxy"
	ComponentSingbox   Component = "sing-box"
)

// componentRepo maps a Component to its GitHub owner/repo pair.
var componentRepo = map[Component][2]string{
	ComponentCLI:       {"OstaninKI", "MTProxyOrchestrator"},
	ComponentPanel:     {"OstaninKI", "MTProxyOrchestrator"},
	ComponentTeleproxy: {"teleproxy", "teleproxy"},
	ComponentSingbox:   {"SagerNet", "sing-box"},
}

// UpdateInfo describes an available update for one component.
type UpdateInfo struct {
	Component        Component
	CurrentVersion   string
	AvailableVersion string
	DownloadURL      string
	SHA256           string // may be empty if not published in release body
}

// CheckRecord stores the result of one update check in history.
type CheckRecord struct {
	Component        Component
	CurrentVersion   string
	AvailableVersion string
	CheckedAt        time.Time
}

// HTTPClient is the minimal interface used to query the GitHub Releases API.
// The real implementation uses *http.Client; tests inject a fake.
type HTTPClient interface {
	Get(url string) (*http.Response, error)
}

// Clock lets tests inject a fake time source.
type Clock interface {
	Now() time.Time
}

// realClock uses the system clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// StateStore persists the last check time and check history.
// Implementations may use a file or SQLite; tests inject a fake.
type StateStore interface {
	// LoadLastCheck returns the last time a check was performed for a component.
	// Returns zero time and nil error when no record exists.
	LoadLastCheck(c Component) (time.Time, error)

	// SaveLastCheck records the time a check was performed.
	SaveLastCheck(c Component, t time.Time) error

	// AppendRecord appends a check result to the history.
	AppendRecord(r CheckRecord) error

	// LoadHistory returns all check records, oldest first.
	LoadHistory() ([]CheckRecord, error)
}

const checkInterval = 18 * time.Hour
const defaultHTTPTimeout = 30 * time.Second

// Checker queries GitHub Releases for available versions.
type Checker struct {
	Client          HTTPClient
	Store           StateStore
	Clock           Clock
	CurrentVersions map[Component]string // component → currently installed version
}

// NewChecker creates a Checker with real dependencies.
// stateDir is the directory to store check state (e.g. /etc/tgproxy).
// currentVersions maps each component to its installed version string.
func NewChecker(stateDir string, currentVersions map[Component]string) (*Checker, error) {
	store, err := NewFileStore(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open state store: %w", err)
	}
	return &Checker{
		Client:          &http.Client{Timeout: defaultHTTPTimeout},
		Store:           store,
		Clock:           realClock{},
		CurrentVersions: currentVersions,
	}, nil
}

// CheckAll checks all four managed components.
// When manual is false, components whose last check is within 18 hours are skipped.
// When manual is true, the 18-hour limit is bypassed for all components.
func (c *Checker) CheckAll(manual bool) ([]UpdateInfo, error) {
	components := []Component{
		ComponentCLI,
		ComponentPanel,
		ComponentTeleproxy,
		ComponentSingbox,
	}

	var results []UpdateInfo
	for _, comp := range components {
		info, err := c.CheckOne(comp, manual)
		if err != nil {
			return results, fmt.Errorf("check %s: %w", comp, err)
		}
		if info != nil {
			results = append(results, *info)
		}
	}
	return results, nil
}

// CheckOne checks a single component.
// Returns nil (no error) when the rate limit prevents a check.
func (c *Checker) CheckOne(comp Component, manual bool) (*UpdateInfo, error) {
	now := c.Clock.Now()

	if !manual {
		last, err := c.Store.LoadLastCheck(comp)
		if err != nil {
			return nil, fmt.Errorf("load last check: %w", err)
		}
		if !last.IsZero() && now.Sub(last) < checkInterval {
			// Rate limit: skip this component.
			return nil, nil
		}
	}

	available, downloadURL, assetName, sha256, err := c.fetchLatestRelease(comp)
	if err != nil {
		return nil, err
	}
	_ = assetName // used internally; retained for clarity

	current := c.CurrentVersions[comp]

	// Record the check regardless of whether an update is available.
	rec := CheckRecord{
		Component:        comp,
		CurrentVersion:   current,
		AvailableVersion: available,
		CheckedAt:        now,
	}
	if err := c.Store.SaveLastCheck(comp, now); err != nil {
		return nil, fmt.Errorf("save last check: %w", err)
	}
	if err := c.Store.AppendRecord(rec); err != nil {
		return nil, fmt.Errorf("append record: %w", err)
	}

	info := &UpdateInfo{
		Component:        comp,
		CurrentVersion:   current,
		AvailableVersion: available,
		DownloadURL:      downloadURL,
		SHA256:           sha256,
	}
	if !isUpgrade(current, available) {
		return nil, nil
	}
	return info, nil
}

func isUpgrade(current, available string) bool {
	currentParts, ok := parseVersion(current)
	if !ok {
		return false
	}
	availableParts, ok := parseVersion(available)
	if !ok {
		return false
	}
	return compareVersionParts(currentParts, availableParts) < 0
}

func parseVersion(raw string) ([]int, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "v"))
	if raw == "" {
		return nil, false
	}
	fixNum := 0
	if idx := strings.Index(raw, "-"); idx >= 0 {
		suffix := raw[idx+1:]
		raw = raw[:idx]
		if strings.HasPrefix(suffix, "f") {
			suffix = suffix[1:]
		}
		n, err := strconv.Atoi(suffix)
		if err == nil {
			fixNum = n
		}
	}

	parts := strings.Split(raw, ".")
	version := make([]int, len(parts)+1)
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		value := 0
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return nil, false
			}
			value = value*10 + int(ch-'0')
		}
		version[i] = value
	}
	version[len(parts)] = fixNum
	return version, true
}

func compareVersionParts(a, b []int) int {
	limit := len(a)
	if len(b) > limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

// githubRelease is the subset of the GitHub Releases API response we use.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

// fetchLatestRelease queries the GitHub Releases API and returns the latest
// tag name, a suitable download URL, the selected asset name, and the SHA256
// checksum for that asset parsed from a checksum file in the same release.
// For project binaries (CLI and panel) both share the same repo.
func (c *Checker) fetchLatestRelease(comp Component) (tagName, downloadURL, assetName, sha256 string, err error) {
	repo, ok := componentRepo[comp]
	if !ok {
		return "", "", "", "", fmt.Errorf("unknown component: %s", comp)
	}

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repo[0], repo[1])
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("github api get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", "", fmt.Errorf("github api returned status %d for %s", resp.StatusCode, apiURL)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", "", "", fmt.Errorf("decode github release: %w", err)
	}

	tagName = strings.TrimPrefix(rel.TagName, "v")

	// Find the most appropriate asset for this component and platform.
	downloadURL, assetName = findAssetURLAndName(comp, rel.Assets)

	if downloadURL != "" && assetName != "" {
		checksumURL := findChecksumURL(rel.Assets)
		if checksumURL != "" {
			sha256, err = fetchChecksumForAsset(client, checksumURL, assetName)
			if err != nil {
				sha256 = ""
			}
		}
		if sha256 == "" {
			for _, a := range rel.Assets {
				if a.Name == assetName && a.Digest != "" {
					if hex, ok := parseDigest(a.Digest); ok {
						sha256 = hex
					}
					break
				}
			}
		}
	}

	return tagName, downloadURL, assetName, sha256, nil
}

// findChecksumURL returns the download URL of the first checksum file asset
// in the release (e.g. *_checksums.txt or *.sha256).
func findChecksumURL(assets []githubAsset) string {
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.HasSuffix(name, "_checksums.txt") || strings.HasSuffix(name, ".sha256") ||
			strings.HasSuffix(name, "_checksums.sha256") || strings.Contains(name, "checksums") ||
			strings.Contains(name, "sha256sums") {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// fetchChecksumForAsset downloads the checksum file at url and extracts the
// SHA256 hex string for assetName.  The file may use either of these formats:
//
//	<hex>  <filename>          (GNU coreutils sha256sum format)
//	<hex>  <filename>
func fetchChecksumForAsset(client HTTPClient, url, assetName string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch checksum file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum file returned status %d", resp.StatusCode)
	}

	// Read at most 1 MiB to guard against enormous responses.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read checksum body: %w", err)
	}

	return parseChecksumFile(string(body), assetName)
}

// parseChecksumFile scans a sha256sum-style text for a line whose filename
// component (after whitespace) matches assetName (case-insensitive, basename only).
// Returns an error when no matching line is found.
func parseChecksumFile(content, assetName string) (string, error) {
	needle := strings.ToLower(filepath.Base(assetName))
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Accept both "hash  filename" and "hash *filename" (binary mode).
		filename := strings.TrimLeft(fields[1], "*")
		if strings.ToLower(filepath.Base(filename)) == needle {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for asset %q", assetName)
}

func parseDigest(digest string) (string, bool) {
	const prefix = "sha256:"
	if !strings.HasPrefix(digest, prefix) {
		return "", false
	}
	hex := strings.TrimPrefix(digest, prefix)
	if len(hex) != 64 {
		return "", false
	}
	return hex, true
}

// findAssetURLAndName picks the best download URL and asset name from a list of
// release assets.  It prefers linux-amd64 variants matching the component name.
// Checksum files are never selected as the binary asset.
func findAssetURLAndName(comp Component, assets []githubAsset) (downloadURL, assetName string) {
	compStr := string(comp)

	isChecksumFile := func(name string) bool {
		n := strings.ToLower(name)
		return strings.HasSuffix(n, ".sha256") ||
			strings.HasSuffix(n, "_checksums.txt") ||
			strings.HasSuffix(n, "_checksums.sha256") ||
			strings.Contains(n, "checksums")
	}

	for _, a := range assets {
		if isChecksumFile(a.Name) {
			continue
		}
		name := strings.ToLower(a.Name)
		if strings.Contains(name, compStr) && strings.Contains(name, "linux") && strings.Contains(name, "amd64") {
			if !strings.Contains(name, "glibc") && !strings.Contains(name, "musl") {
				return a.BrowserDownloadURL, a.Name
			}
		}
	}
	for _, a := range assets {
		if isChecksumFile(a.Name) {
			continue
		}
		name := strings.ToLower(a.Name)
		if strings.Contains(name, compStr) && strings.Contains(name, "linux") && strings.Contains(name, "amd64") {
			return a.BrowserDownloadURL, a.Name
		}
	}
	for _, a := range assets {
		if isChecksumFile(a.Name) {
			continue
		}
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "linux") && strings.Contains(name, "amd64") {
			return a.BrowserDownloadURL, a.Name
		}
	}
	for _, a := range assets {
		if isChecksumFile(a.Name) {
			continue
		}
		name := strings.ToLower(a.Name)
		if strings.Contains(name, compStr) {
			return a.BrowserDownloadURL, a.Name
		}
	}
	for _, a := range assets {
		if !isChecksumFile(a.Name) {
			return a.BrowserDownloadURL, a.Name
		}
	}
	return "", ""
}

// FileStore is a StateStore backed by plain files under a directory.
// Last-check times are stored as RFC3339 text; history is newline-delimited JSON.
type FileStore struct {
	dir string
}

// NewFileStore creates a FileStore rooted at dir.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir state dir: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) lastCheckPath(c Component) string {
	return filepath.Join(s.dir, fmt.Sprintf("update_last_%s.txt", c))
}

func (s *FileStore) historyPath() string {
	return filepath.Join(s.dir, "update_history.jsonl")
}

// LoadLastCheck reads the last check time for a component from a text file.
func (s *FileStore) LoadLastCheck(c Component) (time.Time, error) {
	data, err := os.ReadFile(s.lastCheckPath(c))
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read last check file: %w", err)
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse last check time: %w", err)
	}
	return t, nil
}

// SaveLastCheck writes the check time to a text file.
func (s *FileStore) SaveLastCheck(c Component, t time.Time) error {
	path := s.lastCheckPath(c)
	data := []byte(t.UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write last check file: %w", err)
	}
	return nil
}

// AppendRecord appends a JSON line to the history file.
func (s *FileStore) AppendRecord(r CheckRecord) error {
	f, err := os.OpenFile(s.historyPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// LoadHistory reads all check records from the history file, oldest first.
func (s *FileStore) LoadHistory() ([]CheckRecord, error) {
	data, err := os.ReadFile(s.historyPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history file: %w", err)
	}

	var records []CheckRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r CheckRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("unmarshal record: %w", err)
		}
		records = append(records, r)
	}
	return records, nil
}
