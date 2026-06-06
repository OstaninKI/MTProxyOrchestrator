package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

const fixtureMetrics = `# HELP mtproto_secret_traffic_bytes_in Total bytes received per secret
# TYPE mtproto_secret_traffic_bytes_in counter
mtproto_secret_traffic_bytes_in{label="alice"} 100
mtproto_secret_traffic_bytes_in{label="bob"} 200
# HELP mtproto_secret_traffic_bytes_out Total bytes sent per secret
# TYPE mtproto_secret_traffic_bytes_out counter
mtproto_secret_traffic_bytes_out{label="alice"} 300
mtproto_secret_traffic_bytes_out{label="bob"} 400
# HELP mtproto_secret_connections_total Active connections per secret
# TYPE mtproto_secret_connections_total gauge
mtproto_secret_connections_total{label="alice"} 2
mtproto_secret_connections_total{label="bob"} 5
`

const teleproxyFixtureMetrics = `# HELP teleproxy_secret_connections Current connections per configured secret.
# TYPE teleproxy_secret_connections gauge
teleproxy_secret_connections{secret="alice"} 2
teleproxy_secret_connections{secret="bob"} 0
# HELP teleproxy_secret_bytes_received_total Bytes received by proxy from clients (i.e. client uploads) per secret. Direct mode only.
# TYPE teleproxy_secret_bytes_received_total counter
teleproxy_secret_bytes_received_total{secret="alice"} 100
teleproxy_secret_bytes_received_total{secret="bob"} 200
# HELP teleproxy_secret_bytes_sent_total Bytes sent by proxy to clients (i.e. client downloads) per secret. Direct mode only.
# TYPE teleproxy_secret_bytes_sent_total counter
teleproxy_secret_bytes_sent_total{secret="alice"} 300
teleproxy_secret_bytes_sent_total{secret="bob"} 400
`

const teleproxyOpsFixtureMetrics = `# HELP teleproxy_ext_connections Current client connections.
# TYPE teleproxy_ext_connections gauge
teleproxy_ext_connections 7
# HELP teleproxy_ext_connections_created_total New client connections.
# TYPE teleproxy_ext_connections_created_total counter
teleproxy_ext_connections_created_total 101
# HELP teleproxy_connections_failed_lru_total LRU failed connections.
# TYPE teleproxy_connections_failed_lru_total counter
teleproxy_connections_failed_lru_total 3
# HELP teleproxy_connections_failed_flood_total Flood failed connections.
# TYPE teleproxy_connections_failed_flood_total counter
teleproxy_connections_failed_flood_total 4
# HELP teleproxy_ip_acl_rejected_total IP ACL rejected connections.
# TYPE teleproxy_ip_acl_rejected_total counter
teleproxy_ip_acl_rejected_total 5
# HELP teleproxy_secret_connection_limit Current connection limit per configured secret.
# TYPE teleproxy_secret_connection_limit gauge
teleproxy_secret_connection_limit{secret="alice"} 10
# HELP teleproxy_secret_unique_ips Current unique IPs per configured secret.
# TYPE teleproxy_secret_unique_ips gauge
teleproxy_secret_unique_ips{secret="alice"} 2
# HELP teleproxy_secret_max_ips Current max IPs per configured secret.
# TYPE teleproxy_secret_max_ips gauge
teleproxy_secret_max_ips{secret="alice"} 5
# HELP teleproxy_secret_connections_rejected_total Rejected connections per secret.
# TYPE teleproxy_secret_connections_rejected_total counter
teleproxy_secret_connections_rejected_total{secret="alice"} 9
# HELP teleproxy_secret_rejected_quota_total Quota rejections per secret.
# TYPE teleproxy_secret_rejected_quota_total counter
teleproxy_secret_rejected_quota_total{secret="alice"} 1
# HELP teleproxy_secret_rejected_ips_total IP limit rejections per secret.
# TYPE teleproxy_secret_rejected_ips_total counter
teleproxy_secret_rejected_ips_total{secret="alice"} 2
# HELP teleproxy_secret_rejected_expired_total Expired secret rejections per secret.
# TYPE teleproxy_secret_rejected_expired_total counter
teleproxy_secret_rejected_expired_total{secret="alice"} 3
# HELP teleproxy_socks5_connects_attempted_total SOCKS5 upstream attempts.
# TYPE teleproxy_socks5_connects_attempted_total counter
teleproxy_socks5_connects_attempted_total 20
# HELP teleproxy_socks5_connects_succeeded_total SOCKS5 upstream successes.
# TYPE teleproxy_socks5_connects_succeeded_total counter
teleproxy_socks5_connects_succeeded_total 18
# HELP teleproxy_socks5_connects_failed_total SOCKS5 upstream failures.
# TYPE teleproxy_socks5_connects_failed_total counter
teleproxy_socks5_connects_failed_total 2
# HELP teleproxy_ja4_seen ClientHello JA4 fingerprints observed.
# TYPE teleproxy_ja4_seen counter
teleproxy_ja4_seen{hash="t13d1615h2_46e7e9700bed_45f260be83e2"} 8
teleproxy_ja4_seen{hash="t13d1516h2_8daaf6152771_b0da82dd1658"} 5
`

const teleproxyUpstreamFixtureMetrics = `# HELP teleproxy_dc_latency_last_seconds Most recent probe latency per Telegram DC.
# TYPE teleproxy_dc_latency_last_seconds gauge
teleproxy_dc_latency_last_seconds{dc="1"} 0.042
teleproxy_dc_latency_last_seconds{dc="2"} 0.118
teleproxy_dc_latency_last_seconds{dc="5"} 0.500
# HELP teleproxy_dc_probe_failures_total Probe failures per Telegram DC.
# TYPE teleproxy_dc_probe_failures_total counter
teleproxy_dc_probe_failures_total{dc="1"} 0
teleproxy_dc_probe_failures_total{dc="5"} 3
# HELP teleproxy_proxy_protocol_connections_total PROXY-protocol connections.
# TYPE teleproxy_proxy_protocol_connections_total counter
teleproxy_proxy_protocol_connections_total 17
# HELP teleproxy_proxy_protocol_errors_total PROXY-protocol parse errors.
# TYPE teleproxy_proxy_protocol_errors_total counter
teleproxy_proxy_protocol_errors_total 2
`

func TestScrapeSnapshotParsesUpstreamMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(teleproxyOpsFixtureMetrics + teleproxyUpstreamFixtureMetrics)) //nolint:errcheck
	}))
	defer srv.Close()

	snapshot, err := newScraper(srv).ScrapeSnapshot()
	if err != nil {
		t.Fatalf("ScrapeSnapshot() error: %v", err)
	}

	if len(snapshot.DCStats) != 3 {
		t.Fatalf("DCStats count = %d, want 3", len(snapshot.DCStats))
	}
	// DCStats must be sorted by DC.
	dc1 := snapshot.DCStats[0]
	if dc1.DC != "1" {
		t.Fatalf("first DC = %q, want 1", dc1.DC)
	}
	if dc1.LastLatencyMs != 42 {
		t.Errorf("dc1 latency = %v ms, want 42", dc1.LastLatencyMs)
	}
	if dc1.ProbeFailures != 0 {
		t.Errorf("dc1 failures = %d, want 0", dc1.ProbeFailures)
	}
	dc5 := snapshot.DCStats[2]
	if dc5.DC != "5" || dc5.LastLatencyMs != 500 || dc5.ProbeFailures != 3 {
		t.Errorf("dc5 = %+v, want DC 5 / 500ms / 3 failures", dc5)
	}
	// DC 2 has latency but no failures metric: failures default 0.
	dc2 := snapshot.DCStats[1]
	if dc2.DC != "2" || dc2.LastLatencyMs != 118 || dc2.ProbeFailures != 0 {
		t.Errorf("dc2 = %+v, want DC 2 / 118ms / 0 failures", dc2)
	}

	if snapshot.ProxyProtocol.Connections != 17 || snapshot.ProxyProtocol.Errors != 2 {
		t.Errorf("ProxyProtocol = %+v, want 17 conns / 2 errors", snapshot.ProxyProtocol)
	}
}

func newScraper(srv *httptest.Server) metrics.Scraper {
	return metrics.Scraper{
		Client:    srv.Client(),
		StatsAddr: srv.URL,
	}
}

func TestScrapeParsesSamples(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fixtureMetrics)) //nolint:errcheck
	}))
	defer srv.Close()

	samples, err := newScraper(srv).Scrape()
	if err != nil {
		t.Fatalf("Scrape() error: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}

	// Sort for deterministic comparison.
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].UserLabel < samples[j].UserLabel
	})

	alice := samples[0]
	if alice.UserLabel != "alice" {
		t.Errorf("want alice, got %q", alice.UserLabel)
	}
	if alice.BytesIn != 100 {
		t.Errorf("alice BytesIn: want 100, got %d", alice.BytesIn)
	}
	if alice.BytesOut != 300 {
		t.Errorf("alice BytesOut: want 300, got %d", alice.BytesOut)
	}
	if alice.Connections != 2 {
		t.Errorf("alice Connections: want 2, got %d", alice.Connections)
	}

	bob := samples[1]
	if bob.UserLabel != "bob" {
		t.Errorf("want bob, got %q", bob.UserLabel)
	}
	if bob.BytesIn != 200 {
		t.Errorf("bob BytesIn: want 200, got %d", bob.BytesIn)
	}
	if bob.BytesOut != 400 {
		t.Errorf("bob BytesOut: want 400, got %d", bob.BytesOut)
	}
	if bob.Connections != 5 {
		t.Errorf("bob Connections: want 5, got %d", bob.Connections)
	}
}

func TestScrapeParsesCurrentTeleproxyMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(teleproxyFixtureMetrics)) //nolint:errcheck
	}))
	defer srv.Close()

	samples, err := newScraper(srv).Scrape()
	if err != nil {
		t.Fatalf("Scrape() error: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}

	sort.Slice(samples, func(i, j int) bool {
		return samples[i].UserLabel < samples[j].UserLabel
	})

	alice := samples[0]
	if alice.UserLabel != "alice" {
		t.Errorf("want alice, got %q", alice.UserLabel)
	}
	if alice.BytesIn != 100 {
		t.Errorf("alice BytesIn: want 100, got %d", alice.BytesIn)
	}
	if alice.BytesOut != 300 {
		t.Errorf("alice BytesOut: want 300, got %d", alice.BytesOut)
	}
	if alice.Connections != 2 {
		t.Errorf("alice Connections: want 2, got %d", alice.Connections)
	}
}

func TestScrapeSnapshotParsesTeleproxyOperationalMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(teleproxyFixtureMetrics + teleproxyOpsFixtureMetrics)) //nolint:errcheck
	}))
	defer srv.Close()

	snapshot, err := newScraper(srv).ScrapeSnapshot()
	if err != nil {
		t.Fatalf("ScrapeSnapshot() error: %v", err)
	}

	sort.Slice(snapshot.Samples, func(i, j int) bool {
		return snapshot.Samples[i].UserLabel < snapshot.Samples[j].UserLabel
	})
	alice := snapshot.Samples[0]
	if alice.ConnectionLimit != 10 || alice.UniqueIPs != 2 || alice.MaxIPs != 5 {
		t.Fatalf("alice limits = conn:%d unique:%d max:%d, want 10/2/5", alice.ConnectionLimit, alice.UniqueIPs, alice.MaxIPs)
	}
	if alice.RejectedConnections != 9 || alice.RejectedQuota != 1 || alice.RejectedIPs != 2 || alice.RejectedExpired != 3 {
		t.Fatalf("alice rejections = total:%d quota:%d ips:%d expired:%d, want 9/1/2/3",
			alice.RejectedConnections, alice.RejectedQuota, alice.RejectedIPs, alice.RejectedExpired)
	}
	if snapshot.AcceptedConnectionsTotal != 101 {
		t.Fatalf("AcceptedConnectionsTotal = %d, want 101", snapshot.AcceptedConnectionsTotal)
	}
	if snapshot.RejectedConnectionsTotal != 12 {
		t.Fatalf("RejectedConnectionsTotal = %d, want 12", snapshot.RejectedConnectionsTotal)
	}
	if snapshot.SOCKS5.Attempted != 20 || snapshot.SOCKS5.Succeeded != 18 || snapshot.SOCKS5.Failed != 2 {
		t.Fatalf("SOCKS5 counters = %+v, want 20/18/2", snapshot.SOCKS5)
	}
	if len(snapshot.JA4) != 2 {
		t.Fatalf("JA4 count = %d, want 2", len(snapshot.JA4))
	}
	if snapshot.JA4[0].Hash != "t13d1615h2_46e7e9700bed_45f260be83e2" || snapshot.JA4[0].Count != 8 {
		t.Fatalf("top JA4 = %+v, want first fixture hash count 8", snapshot.JA4[0])
	}
}

func TestDefaultScraperUsesHTTPClientWithTimeout(t *testing.T) {
	s := metrics.DefaultScraper("http://127.0.0.1:9091")
	client, ok := s.Client.(*http.Client)
	if !ok {
		t.Fatalf("DefaultScraper Client type = %T, want *http.Client", s.Client)
	}
	if client.Timeout <= 0 {
		t.Fatal("DefaultScraper HTTP client must set a timeout")
	}
}

func TestScrapeRejectsOversizedMetricsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("A", 2*1024*1024))
	}))
	defer srv.Close()

	_, err := newScraper(srv).Scrape()
	if err == nil {
		t.Fatal("expected oversized metrics response to be rejected")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestScrapeEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	samples, err := newScraper(srv).Scrape()
	if err != nil {
		t.Fatalf("Scrape() on empty body: unexpected error: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected empty slice, got %d samples", len(samples))
	}
}

func TestScrapeHTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := newScraper(srv).Scrape()
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

func TestScrapePartialMetrics(t *testing.T) {
	const partial = `# HELP mtproto_secret_traffic_bytes_in Total bytes received per secret
# TYPE mtproto_secret_traffic_bytes_in counter
mtproto_secret_traffic_bytes_in{label="charlie"} 42
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(partial)) //nolint:errcheck
	}))
	defer srv.Close()

	samples, err := newScraper(srv).Scrape()
	if err != nil {
		t.Fatalf("Scrape() error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	s := samples[0]
	if s.UserLabel != "charlie" {
		t.Errorf("want charlie, got %q", s.UserLabel)
	}
	if s.BytesIn != 42 {
		t.Errorf("BytesIn: want 42, got %d", s.BytesIn)
	}
	if s.BytesOut != 0 {
		t.Errorf("BytesOut: want 0, got %d", s.BytesOut)
	}
	if s.Connections != 0 {
		t.Errorf("Connections: want 0, got %d", s.Connections)
	}
}
