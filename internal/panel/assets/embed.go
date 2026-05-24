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
		case strings.HasPrefix(path, "vendor/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case etags[path] != "":
			// Revalidate on every load; ServeContent answers If-None-Match
			// with 304 when the embedded content is unchanged.
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("ETag", etags[path])
		default:
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}
