package panel

import panelassets "github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel/assets"

// assetVersion is the "assetv" template helper. It returns a cache-busting
// query suffix ("?v=<token>") for revalidated assets so the browser can cache
// them immutably and only refetch after an upgrade changes the bytes.
func assetVersion(name string) string {
	return panelassets.Version(name)
}
