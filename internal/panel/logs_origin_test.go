package panel

import "testing"

func TestWSOriginAllowed(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"empty origin (non-browser)", "", "mtp.example.com", true},
		{"exact match no port", "https://mtp.example.com", "mtp.example.com", true},
		{"exact match with port", "https://mtp.example.com:8443", "mtp.example.com:8443", true},
		// The real-world bug: browser on :8443 sends Origin with port, nginx
		// forwards Host without port via proxy_set_header Host $host.
		{"origin has port, host stripped by nginx", "https://mtp.example.com:8443", "mtp.example.com", true},
		{"host has port, origin none", "https://mtp.example.com", "mtp.example.com:8443", true},
		{"http scheme tolerated", "http://mtp.example.com", "mtp.example.com", true},
		// Cross-site protection must still hold.
		{"different domain rejected", "https://evil.com", "mtp.example.com", false},
		{"different domain with port rejected", "https://evil.com:8443", "mtp.example.com", false},
		{"subdomain rejected", "https://evil.mtp.example.com", "mtp.example.com", false},
		{"garbage origin rejected", "not a url", "mtp.example.com", false},
		{"null origin rejected", "null", "mtp.example.com", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wsOriginAllowed(tc.origin, tc.host); got != tc.want {
				t.Errorf("wsOriginAllowed(%q, %q) = %v, want %v", tc.origin, tc.host, got, tc.want)
			}
		})
	}
}
