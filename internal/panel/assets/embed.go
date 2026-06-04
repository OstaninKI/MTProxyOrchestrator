package assets

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed panel.css panel.js vendor/* fonts/**/*
var files embed.FS

// etags maps revalidated asset names to a content-derived ETag computed at
// startup. Embedded files have a zero modtime, so without an explicit ETag the
// FileServer cannot answer conditional requests and a stale panel.js/panel.css
// would keep running for the full max-age window after a panel upgrade.
var etags = computeETags("panel.css", "panel.js")

// versionTokens maps revalidated asset names to a short cache-busting token
// derived from the same content hash as the ETag. Templates append it as a
// "?v=<token>" query string so each asset version has a unique URL.
var versionTokens = computeVersionTokens()

func computeETags(names ...string) map[string]string {
	m := make(map[string]string, len(names))
	for _, name := range names {
		data, err := files.ReadFile(name)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		m[name] = `"` + hex.EncodeToString(sum[:]) + `"`
	}
	return m
}

func computeVersionTokens() map[string]string {
	m := make(map[string]string, len(etags))
	for name, tag := range etags {
		h := strings.Trim(tag, `"`)
		if len(h) > 12 {
			h = h[:12]
		}
		m[name] = h
	}
	return m
}

// Version returns a cache-busting query suffix ("?v=<token>") for revalidated
// assets such as panel.css and panel.js, or "" for unknown names. The token is
// derived from the embedded content hash, so it changes whenever an upgrade
// modifies the asset — turning every version into a distinct, immutably
// cacheable URL.
func Version(name string) string {
	if t := versionTokens[name]; t != "" {
		return "?v=" + t
	}
	return ""
}

func Handler() http.Handler {
	sub, err := fs.Sub(files, ".")
	if err != nil {
		panic(err)
	}
	return cacheHeaders(http.FileServer(http.FS(sub)))
}

func cacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case strings.HasPrefix(path, "vendor/"), strings.HasPrefix(path, "fonts/"):
			// Versioned vendor bundles and content-stable font files never
			// change at a given URL, so cache them hard.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case etags[path] != "":
			w.Header().Set("ETag", etags[path])
			if r.URL.Query().Get("v") != "" {
				// The link carries a content-hash query string, so a new
				// version is a new URL; these bytes can be cached immutably.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				// No fingerprint (e.g. a direct hit): revalidate every load;
				// ServeContent answers If-None-Match with 304 when unchanged.
				w.Header().Set("Cache-Control", "no-cache")
			}
		default:
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}
