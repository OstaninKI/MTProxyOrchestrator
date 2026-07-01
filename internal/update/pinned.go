package update

import (
	"fmt"
	"net/http"
	"strings"
)

// ResolvePinnedAsset returns the deterministic GitHub release download URL and
// SHA256 hex for comp at the exact version (a "v" prefix is tolerated, e.g.
// "2026.6.3" or "v2026.6.3"). It derives the asset URL from componentRepo and
// fetches the release's checksums.txt to obtain the SHA256. Intended for
// fetching a project binary (CLI/panel) pinned to the running CLI's own version.
// Pass a nil client to use a default HTTP client.
func ResolvePinnedAsset(client HTTPClient, comp Component, version string) (downloadURL, sha256hex string, err error) {
	repo, ok := componentRepo[comp]
	if !ok {
		return "", "", fmt.Errorf("unknown component: %s", comp)
	}

	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return "", "", fmt.Errorf("release version is required to resolve %s", comp)
	}

	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	owner, repoName := repo[0], repo[1]
	assetName := string(comp)
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s/", owner, repoName, version)
	downloadURL = base + assetName
	// ponytail: trust ceiling = SHA256 over TLS, checksums.txt from the SAME
	// release as the binary. A compromised tag or a TLS-valid MITM can supply
	// both binary and hash. No offline signature (cosign/pkx) yet. Upgrade path:
	// pin a public key and verify a detached signature when the threat model
	// demands it.
	checksumURL := base + "checksums.txt"

	sha256hex, err = fetchChecksumForAsset(client, checksumURL, assetName)
	if err != nil {
		return "", "", fmt.Errorf("resolve checksum for %s@%s: %w", comp, version, err)
	}

	return downloadURL, sha256hex, nil
}
