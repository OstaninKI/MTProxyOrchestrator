package update

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeHTTPClient returns preconfigured responses keyed by URL substring.
type fakeHTTPClient struct {
	responses map[string]fakeResponse
}

type fakeResponse struct {
	status int
	body   string
}

func (f *fakeHTTPClient) Get(url string) (*http.Response, error) {
	for key, resp := range f.responses {
		if strings.Contains(url, key) {
			return &http.Response{
				StatusCode: resp.status,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
			}, nil
		}
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{"message":"Not Found"}`)),
	}, nil
}

// fakeClock is a settable clock for tests.
type fakeClock struct {
	current time.Time
}

func (c *fakeClock) Now() time.Time { return c.current }

// fakeStore implements StateStore in memory.
type fakeStore struct {
	lastChecks map[Component]time.Time
	history    []CheckRecord
}

func newFakeStore() *fakeStore {
	return &fakeStore{lastChecks: make(map[Component]time.Time)}
}

func (s *fakeStore) LoadLastCheck(c Component) (time.Time, error) {
	return s.lastChecks[c], nil
}

func (s *fakeStore) SaveLastCheck(c Component, t time.Time) error {
	s.lastChecks[c] = t
	return nil
}

func (s *fakeStore) AppendRecord(r CheckRecord) error {
	s.history = append(s.history, r)
	return nil
}

func (s *fakeStore) LoadHistory() ([]CheckRecord, error) {
	return s.history, nil
}

// githubReleaseJSON builds a minimal GitHub Releases API response.
func githubReleaseJSON(tag string, assets []githubAsset) string {
	rel := githubRelease{TagName: tag, Assets: assets}
	data, _ := json.Marshal(rel)
	return string(data)
}

func TestCheckerFetchesLatestVersion(t *testing.T) {
	tests := []struct {
		name            string
		component       Component
		urlKeyword      string
		releaseTag      string
		assetName       string
		assetURL        string
		wantVersion     string
		wantDownloadURL string
	}{
		{
			name:            "teleproxy linux amd64",
			component:       ComponentTeleproxy,
			urlKeyword:      "teleproxy/teleproxy",
			releaseTag:      "v4.13.0",
			assetName:       "teleproxy-linux-amd64",
			assetURL:        "https://github.com/teleproxy/teleproxy/releases/download/v4.13.0/teleproxy-linux-amd64",
			wantVersion:     "4.13.0",
			wantDownloadURL: "https://github.com/teleproxy/teleproxy/releases/download/v4.13.0/teleproxy-linux-amd64",
		},
		{
			name:            "sing-box linux amd64",
			component:       ComponentSingbox,
			urlKeyword:      "SagerNet/sing-box",
			releaseTag:      "v1.10.0",
			assetName:       "sing-box-1.10.0-linux-amd64.tar.gz",
			assetURL:        "https://github.com/SagerNet/sing-box/releases/download/v1.10.0/sing-box-1.10.0-linux-amd64.tar.gz",
			wantVersion:     "1.10.0",
			wantDownloadURL: "https://github.com/SagerNet/sing-box/releases/download/v1.10.0/sing-box-1.10.0-linux-amd64.tar.gz",
		},
		{
			name:            "tgproxy-cli linux amd64",
			component:       ComponentCLI,
			urlKeyword:      "mtproto-orchestrator/mtproto-orchestrator",
			releaseTag:      "v0.3.0",
			assetName:       "tgproxy-cli-linux-amd64",
			assetURL:        "https://github.com/mtproto-orchestrator/mtproto-orchestrator/releases/download/v0.3.0/tgproxy-cli-linux-amd64",
			wantVersion:     "0.3.0",
			wantDownloadURL: "https://github.com/mtproto-orchestrator/mtproto-orchestrator/releases/download/v0.3.0/tgproxy-cli-linux-amd64",
		},
		{
			name:            "tgproxy-panel linux amd64",
			component:       ComponentPanel,
			urlKeyword:      "mtproto-orchestrator/mtproto-orchestrator",
			releaseTag:      "v0.3.0",
			assetName:       "tgproxy-panel-linux-amd64",
			assetURL:        "https://github.com/mtproto-orchestrator/mtproto-orchestrator/releases/download/v0.3.0/tgproxy-panel-linux-amd64",
			wantVersion:     "0.3.0",
			wantDownloadURL: "https://github.com/mtproto-orchestrator/mtproto-orchestrator/releases/download/v0.3.0/tgproxy-panel-linux-amd64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assets := []githubAsset{{Name: tc.assetName, BrowserDownloadURL: tc.assetURL}}
			body := githubReleaseJSON(tc.releaseTag, assets)

			store := newFakeStore()
			clk := &fakeClock{current: time.Now()}
			checker := &Checker{
				Client: &fakeHTTPClient{
					responses: map[string]fakeResponse{
						tc.urlKeyword: {status: http.StatusOK, body: body},
					},
				},
				Store:           store,
				Clock:           clk,
				CurrentVersions: map[Component]string{tc.component: "0.0.0"},
			}

			info, err := checker.CheckOne(tc.component, true)
			if err != nil {
				t.Fatalf("CheckOne: %v", err)
			}
			if info == nil {
				t.Fatal("expected non-nil UpdateInfo")
			}
			if info.AvailableVersion != tc.wantVersion {
				t.Errorf("AvailableVersion = %q, want %q", info.AvailableVersion, tc.wantVersion)
			}
			if info.DownloadURL != tc.wantDownloadURL {
				t.Errorf("DownloadURL = %q, want %q", info.DownloadURL, tc.wantDownloadURL)
			}
		})
	}
}

func TestCheckerRateLimitAutomatic(t *testing.T) {
	store := newFakeStore()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{current: baseTime}

	body := githubReleaseJSON("v1.0.0", []githubAsset{
		{Name: "teleproxy-linux-amd64", BrowserDownloadURL: "https://example.com/teleproxy-linux-amd64"},
	})
	checker := &Checker{
		Client: &fakeHTTPClient{
			responses: map[string]fakeResponse{
				"teleproxy/teleproxy": {status: http.StatusOK, body: body},
			},
		},
		Store:           store,
		Clock:           clk,
		CurrentVersions: map[Component]string{ComponentTeleproxy: "0.9.0"},
	}

	// First automatic check: should succeed and record the time.
	info, err := checker.CheckOne(ComponentTeleproxy, false)
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	if info == nil {
		t.Fatal("first check: expected non-nil result")
	}

	// Second automatic check within 18 hours: should be rate-limited (nil, nil).
	clk.current = baseTime.Add(17 * time.Hour)
	info, err = checker.CheckOne(ComponentTeleproxy, false)
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if info != nil {
		t.Errorf("second check within rate limit: expected nil, got %+v", info)
	}

	// Check after 18+ hours: should proceed again.
	clk.current = baseTime.Add(19 * time.Hour)
	info, err = checker.CheckOne(ComponentTeleproxy, false)
	if err != nil {
		t.Fatalf("third check: %v", err)
	}
	if info == nil {
		t.Fatal("third check after rate limit window: expected non-nil result")
	}
}

func TestCheckerManualBypassesRateLimit(t *testing.T) {
	store := newFakeStore()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{current: baseTime}

	body := githubReleaseJSON("v2.0.0", []githubAsset{
		{Name: "teleproxy-linux-amd64", BrowserDownloadURL: "https://example.com/teleproxy-linux-amd64"},
	})
	checker := &Checker{
		Client: &fakeHTTPClient{
			responses: map[string]fakeResponse{
				"teleproxy/teleproxy": {status: http.StatusOK, body: body},
			},
		},
		Store:           store,
		Clock:           clk,
		CurrentVersions: map[Component]string{ComponentTeleproxy: "1.9.0"},
	}

	// Record a recent check (just 1 hour ago).
	store.lastChecks[ComponentTeleproxy] = baseTime.Add(-1 * time.Hour)

	// Automatic check: should be blocked.
	info, err := checker.CheckOne(ComponentTeleproxy, false)
	if err != nil {
		t.Fatalf("auto check: %v", err)
	}
	if info != nil {
		t.Errorf("auto check: expected nil (rate limited), got %+v", info)
	}

	// Manual check: should bypass rate limit.
	info, err = checker.CheckOne(ComponentTeleproxy, true)
	if err != nil {
		t.Fatalf("manual check: %v", err)
	}
	if info == nil {
		t.Fatal("manual check: expected non-nil result")
	}
	if info.AvailableVersion != "2.0.0" {
		t.Errorf("manual check: AvailableVersion = %q, want %q", info.AvailableVersion, "2.0.0")
	}
}

func TestCheckerHistoryRecorded(t *testing.T) {
	store := newFakeStore()
	baseTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	clk := &fakeClock{current: baseTime}

	mkBody := func(tag string) string {
		return githubReleaseJSON(tag, []githubAsset{
			{Name: "teleproxy-linux-amd64", BrowserDownloadURL: "https://example.com/teleproxy"},
		})
	}

	checker := &Checker{
		Client: &fakeHTTPClient{
			responses: map[string]fakeResponse{
				"teleproxy/teleproxy": {status: http.StatusOK, body: mkBody("v3.0.0")},
			},
		},
		Store:           store,
		Clock:           clk,
		CurrentVersions: map[Component]string{ComponentTeleproxy: "2.9.0"},
	}

	if _, err := checker.CheckOne(ComponentTeleproxy, true); err != nil {
		t.Fatalf("check: %v", err)
	}

	history, err := store.LoadHistory()
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
	r := history[0]
	if r.Component != ComponentTeleproxy {
		t.Errorf("record.Component = %q, want %q", r.Component, ComponentTeleproxy)
	}
	if r.CurrentVersion != "2.9.0" {
		t.Errorf("record.CurrentVersion = %q, want %q", r.CurrentVersion, "2.9.0")
	}
	if r.AvailableVersion != "3.0.0" {
		t.Errorf("record.AvailableVersion = %q, want %q", r.AvailableVersion, "3.0.0")
	}
	if !r.CheckedAt.Equal(baseTime) {
		t.Errorf("record.CheckedAt = %v, want %v", r.CheckedAt, baseTime)
	}
}

func TestCheckerIgnoresNonUpgradesButRecordsHistory(t *testing.T) {
	tests := []struct {
		name            string
		currentVersion  string
		releaseTag      string
		wantHistoryVers string
	}{
		{
			name:            "same version",
			currentVersion:  "3.0.0",
			releaseTag:      "v3.0.0",
			wantHistoryVers: "3.0.0",
		},
		{
			name:            "older release",
			currentVersion:  "3.1.0",
			releaseTag:      "v3.0.0",
			wantHistoryVers: "3.0.0",
		},
		{
			name:            "unparseable current version",
			currentVersion:  "main",
			releaseTag:      "v3.0.0",
			wantHistoryVers: "3.0.0",
		},
		{
			name:            "unparseable available version",
			currentVersion:  "2.9.0",
			releaseTag:      "latest",
			wantHistoryVers: "latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			baseTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
			clk := &fakeClock{current: baseTime}

			body := githubReleaseJSON(tc.releaseTag, []githubAsset{
				{Name: "teleproxy-linux-amd64", BrowserDownloadURL: "https://example.com/teleproxy"},
			})

			checker := &Checker{
				Client: &fakeHTTPClient{
					responses: map[string]fakeResponse{
						"teleproxy/teleproxy": {status: http.StatusOK, body: body},
					},
				},
				Store:           store,
				Clock:           clk,
				CurrentVersions: map[Component]string{ComponentTeleproxy: tc.currentVersion},
			}

			info, err := checker.CheckOne(ComponentTeleproxy, true)
			if err != nil {
				t.Fatalf("CheckOne: %v", err)
			}
			if info != nil {
				t.Fatalf("expected nil UpdateInfo for non-upgrade, got %+v", info)
			}

			history, err := store.LoadHistory()
			if err != nil {
				t.Fatalf("LoadHistory: %v", err)
			}
			if len(history) != 1 {
				t.Fatalf("expected 1 history record, got %d", len(history))
			}
			if history[0].AvailableVersion != tc.wantHistoryVers {
				t.Fatalf("history available version = %q, want %q", history[0].AvailableVersion, tc.wantHistoryVers)
			}
			if got := store.lastChecks[ComponentTeleproxy]; !got.Equal(baseTime) {
				t.Fatalf("last check = %v, want %v", got, baseTime)
			}
		})
	}
}

func TestCheckerCheckAllFourComponents(t *testing.T) {
	mkBody := func(owner, repo, tag string) string {
		assetName := repo + "-linux-amd64"
		return githubReleaseJSON(tag, []githubAsset{
			{Name: assetName, BrowserDownloadURL: "https://github.com/" + owner + "/" + repo + "/releases/download/" + tag + "/" + assetName},
		})
	}

	store := newFakeStore()
	clk := &fakeClock{current: time.Now()}
	checker := &Checker{
		Client: &fakeHTTPClient{
			responses: map[string]fakeResponse{
				"mtproto-orchestrator/mtproto-orchestrator": {status: http.StatusOK, body: mkBody("mtproto-orchestrator", "mtproto-orchestrator", "v1.0.0")},
				"teleproxy/teleproxy":                       {status: http.StatusOK, body: mkBody("teleproxy", "teleproxy", "v4.13.0")},
				"SagerNet/sing-box":                         {status: http.StatusOK, body: mkBody("SagerNet", "sing-box", "v1.10.0")},
			},
		},
		Store: store,
		Clock: clk,
		CurrentVersions: map[Component]string{
			ComponentCLI:       "0.1.0",
			ComponentPanel:     "0.1.0",
			ComponentTeleproxy: "4.12.0",
			ComponentSingbox:   "1.9.0",
		},
	}

	results, err := checker.CheckAll(true)
	if err != nil {
		t.Fatalf("CheckAll: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	history, err := store.LoadHistory()
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 4 {
		t.Errorf("expected 4 history records, got %d", len(history))
	}
}

func TestCheckerAPIErrorPropagated(t *testing.T) {
	store := newFakeStore()
	clk := &fakeClock{current: time.Now()}
	checker := &Checker{
		Client: &fakeHTTPClient{
			responses: map[string]fakeResponse{
				"teleproxy/teleproxy": {status: http.StatusForbidden, body: `{"message":"API rate limit exceeded"}`},
			},
		},
		Store:           store,
		Clock:           clk,
		CurrentVersions: map[Component]string{ComponentTeleproxy: "4.12.0"},
	}

	_, err := checker.CheckOne(ComponentTeleproxy, true)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}
