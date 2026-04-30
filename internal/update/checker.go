package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	ComponentCLI:       {"mtproto-orchestrator", "mtproto-orchestrator"},
	ComponentPanel:     {"mtproto-orchestrator", "mtproto-orchestrator"},
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
		Client:          http.DefaultClient,
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

	available, downloadURL, err := c.fetchLatestRelease(comp)
	if err != nil {
		return nil, err
	}

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
	}
	return info, nil
}

// githubRelease is the subset of the GitHub Releases API response we use.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// fetchLatestRelease queries the GitHub Releases API and returns the latest
// tag name and a suitable download URL for the current platform.
// For project binaries (CLI and panel) both share the same repo; we return
// the shared tag name and an asset URL that matches the component name.
func (c *Checker) fetchLatestRelease(comp Component) (tagName, downloadURL string, err error) {
	repo, ok := componentRepo[comp]
	if !ok {
		return "", "", fmt.Errorf("unknown component: %s", comp)
	}

	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repo[0], repo[1])
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("github api get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github api returned status %d for %s", resp.StatusCode, url)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", fmt.Errorf("decode github release: %w", err)
	}

	tagName = strings.TrimPrefix(rel.TagName, "v")

	// Find the most appropriate asset for this component and platform.
	downloadURL = findAssetURL(comp, rel.Assets)

	return tagName, downloadURL, nil
}

// findAssetURL picks the best download URL from a list of release assets.
// It prefers linux-amd64 variants matching the component name.
func findAssetURL(comp Component, assets []githubAsset) string {
	compStr := string(comp)

	// Preference order: exact match with linux-amd64, then linux-amd64 alone,
	// then any asset containing the component name.
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, compStr) && strings.Contains(name, "linux") && strings.Contains(name, "amd64") {
			return a.BrowserDownloadURL
		}
	}
	// Teleproxy uses pattern: teleproxy-linux-amd64 (no archive)
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "linux") && strings.Contains(name, "amd64") && !strings.HasSuffix(name, ".sha256") {
			return a.BrowserDownloadURL
		}
	}
	// Fallback: first non-checksum asset.
	for _, a := range assets {
		if !strings.HasSuffix(strings.ToLower(a.Name), ".sha256") {
			return a.BrowserDownloadURL
		}
	}
	return ""
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
