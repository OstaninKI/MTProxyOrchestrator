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
