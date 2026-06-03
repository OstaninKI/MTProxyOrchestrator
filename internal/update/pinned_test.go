package update

import (
	"net/http"
	"strings"
	"testing"
)

const testChecksumHex = "a3b4c5d6e7f8091011121314151617181920212223242526272829303132333435"

func makeChecksumBody(assetName, hex string) string {
	return hex + "  " + assetName + "\n"
}

func TestResolvePinnedAssetHappyPath(t *testing.T) {
	const version = "2026.6.3"
	const comp = ComponentPanel
	const wantURL = "https://github.com/OstaninKI/MTProxyOrchestrator/releases/download/v2026.6.3/tgproxy-panel"
	const wantChecksumURL = "https://github.com/OstaninKI/MTProxyOrchestrator/releases/download/v2026.6.3/checksums.txt"

	var gotURL string
	client := &recordingFakeClient{
		inner: &fakeHTTPClient{
			responses: map[string]fakeResponse{
				"checksums.txt": {
					status: http.StatusOK,
					body:   makeChecksumBody("tgproxy-panel", testChecksumHex),
				},
			},
		},
		onGet: func(url string) { gotURL = url },
	}

	downloadURL, sha256hex, err := ResolvePinnedAsset(client, comp, version)
	if err != nil {
		t.Fatalf("ResolvePinnedAsset: %v", err)
	}
	if downloadURL != wantURL {
		t.Errorf("downloadURL = %q, want %q", downloadURL, wantURL)
	}
	if sha256hex != testChecksumHex {
		t.Errorf("sha256hex = %q, want %q", sha256hex, testChecksumHex)
	}
	if gotURL != wantChecksumURL {
		t.Errorf("client was asked for %q, want %q", gotURL, wantChecksumURL)
	}
}

func TestResolvePinnedAssetVPrefix(t *testing.T) {
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			"checksums.txt": {
				status: http.StatusOK,
				body:   makeChecksumBody("tgproxy-panel", testChecksumHex),
			},
		},
	}

	const wantURL = "https://github.com/OstaninKI/MTProxyOrchestrator/releases/download/v2026.6.3/tgproxy-panel"

	downloadURL, sha256hex, err := ResolvePinnedAsset(client, ComponentPanel, "v2026.6.3")
	if err != nil {
		t.Fatalf("ResolvePinnedAsset with v prefix: %v", err)
	}
	if downloadURL != wantURL {
		t.Errorf("downloadURL = %q, want %q", downloadURL, wantURL)
	}
	if sha256hex != testChecksumHex {
		t.Errorf("sha256hex = %q, want %q", sha256hex, testChecksumHex)
	}
}

func TestResolvePinnedAssetEmptyVersion(t *testing.T) {
	_, _, err := ResolvePinnedAsset(nil, ComponentPanel, "")
	if err == nil {
		t.Fatal("expected error for empty version, got nil")
	}
	if !strings.Contains(err.Error(), "version is required") {
		t.Errorf("error message %q does not mention 'version is required'", err.Error())
	}
}

func TestResolvePinnedAssetVOnlyVersion(t *testing.T) {
	_, _, err := ResolvePinnedAsset(nil, ComponentPanel, "v")
	if err == nil {
		t.Fatal("expected error for version='v', got nil")
	}
}

func TestResolvePinnedAssetUnknownComponent(t *testing.T) {
	_, _, err := ResolvePinnedAsset(nil, Component("nope"), "2026.6.3")
	if err == nil {
		t.Fatal("expected error for unknown component, got nil")
	}
	if !strings.Contains(err.Error(), "unknown component") {
		t.Errorf("error message %q does not mention 'unknown component'", err.Error())
	}
}

func TestResolvePinnedAssetMissingChecksumLine(t *testing.T) {
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			"checksums.txt": {
				status: http.StatusOK,
				// checksums.txt exists but only has the CLI entry, not the panel
				body: makeChecksumBody("tgproxy-cli", testChecksumHex),
			},
		},
	}

	_, sha256hex, err := ResolvePinnedAsset(client, ComponentPanel, "2026.6.3")
	if err == nil {
		t.Fatalf("expected error when checksum line is missing, got sha256=%q", sha256hex)
	}
	if sha256hex != "" {
		t.Errorf("sha256hex must be empty on error, got %q", sha256hex)
	}
}

// recordingFakeClient wraps fakeHTTPClient and records the last URL it was called with.
type recordingFakeClient struct {
	inner *fakeHTTPClient
	onGet func(url string)
}

func (r *recordingFakeClient) Get(url string) (*http.Response, error) {
	if r.onGet != nil {
		r.onGet(url)
	}
	return r.inner.Get(url)
}
