package metrics

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const (
	metricBytesInLegacy     = "mtproto_secret_traffic_bytes_in"
	metricBytesOutLegacy    = "mtproto_secret_traffic_bytes_out"
	metricConnectionsLegacy = "mtproto_secret_connections_total"
	metricBytesIn           = "teleproxy_secret_bytes_received_total"
	metricBytesOut          = "teleproxy_secret_bytes_sent_total"
	metricConnections       = "teleproxy_secret_connections"
	metricConnectionLimit   = "teleproxy_secret_connection_limit"
	metricUniqueIPs         = "teleproxy_secret_unique_ips"
	metricMaxIPs            = "teleproxy_secret_max_ips"
	metricSecretRejected    = "teleproxy_secret_connections_rejected_total"
	metricRejectedQuota     = "teleproxy_secret_rejected_quota_total"
	metricRejectedIPs       = "teleproxy_secret_rejected_ips_total"
	metricRejectedExpired   = "teleproxy_secret_rejected_expired_total"
	metricAcceptedTotal     = "teleproxy_ext_connections_created_total"
	metricFailedLRUTotal    = "teleproxy_connections_failed_lru_total"
	metricFailedFloodTotal  = "teleproxy_connections_failed_flood_total"
	metricIPACLRejected     = "teleproxy_ip_acl_rejected_total"
	metricSOCKS5Attempted   = "teleproxy_socks5_connects_attempted_total"
	metricSOCKS5Succeeded   = "teleproxy_socks5_connects_succeeded_total"
	metricSOCKS5Failed      = "teleproxy_socks5_connects_failed_total"
	metricJA4Seen           = "teleproxy_ja4_seen"
	metricDCLatencyLast     = "teleproxy_dc_latency_last_seconds"
	metricDCProbeFailures   = "teleproxy_dc_probe_failures_total"
	metricProxyProtoConns   = "teleproxy_proxy_protocol_connections_total"
	metricProxyProtoErrors  = "teleproxy_proxy_protocol_errors_total"
	labelKeyLegacy          = "label"
	labelKeySecret          = "secret"
	labelKeyJA4Hash         = "hash"
	labelKeyDC              = "dc"
	defaultHTTPTimeout      = 10 * time.Second
)

// MaxResponseBytes caps a single Prometheus scrape response.
const MaxResponseBytes = 1024 * 1024

// Sample holds one point-in-time measurement for a single user.
type Sample struct {
	UserLabel           string
	BytesIn             int64
	BytesOut            int64
	Connections         int64
	ConnectionLimit     int64
	UniqueIPs           int64
	MaxIPs              int64
	RejectedConnections int64
	RejectedQuota       int64
	RejectedIPs         int64
	RejectedExpired     int64
}

// UpstreamCounters holds Teleproxy upstream proxy counters from one scrape.
type UpstreamCounters struct {
	Attempted int64
	Succeeded int64
	Failed    int64
}

// JA4Counter holds one observed ClientHello JA4 fingerprint bucket.
type JA4Counter struct {
	Hash  string
	Count int64
}

// DCStat holds Telegram datacenter upstream-probe metrics for one DC.
// LastLatencyMs is the most recent probe round-trip in milliseconds, or -1
// when no latency was reported (e.g. only the failures counter was present).
type DCStat struct {
	DC            string
	LastLatencyMs float64
	ProbeFailures int64
}

// ProxyProtocolCounters holds PROXY-protocol acceptance counters from one scrape.
type ProxyProtocolCounters struct {
	Connections int64
	Errors      int64
}

// Snapshot holds all Teleproxy metrics parsed from one scrape.
type Snapshot struct {
	Samples                  []Sample
	AcceptedConnectionsTotal int64
	RejectedConnectionsTotal int64
	SOCKS5                   UpstreamCounters
	JA4                      []JA4Counter
	DCStats                  []DCStat
	ProxyProtocol            ProxyProtocolCounters
}

// HTTPClient lets tests inject a fake HTTP client.
type HTTPClient interface {
	Get(url string) (*http.Response, error)
}

// Scraper fetches and parses Teleproxy metrics.
type Scraper struct {
	Client    HTTPClient
	StatsAddr string // e.g. "http://127.0.0.1:9091"
}

// DefaultScraper returns a Scraper wired to the real HTTP client.
func DefaultScraper(statsAddr string) Scraper {
	return Scraper{
		Client:    &http.Client{Timeout: defaultHTTPTimeout},
		StatsAddr: statsAddr,
	}
}

// Scrape fetches /metrics and returns per-user samples.
// Returns an empty slice (not error) when no metrics are present.
func (s Scraper) Scrape() ([]Sample, error) {
	snapshot, err := s.ScrapeSnapshot()
	if err != nil {
		return nil, err
	}
	return snapshot.Samples, nil
}

// ScrapeSnapshot fetches /metrics and returns all dashboard-ready metrics.
func (s Scraper) ScrapeSnapshot() (Snapshot, error) {
	resp, err := s.Client.Get(s.StatsAddr + "/metrics")
	if err != nil {
		return Snapshot{}, fmt.Errorf("metrics scrape: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("metrics scrape: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return Snapshot{}, fmt.Errorf("metrics read: %w", err)
	}
	if len(body) > MaxResponseBytes {
		return Snapshot{}, fmt.Errorf("metrics response too large")
	}

	snapshot, err := parseMetricSnapshot(bytes.NewReader(body))
	if err != nil {
		return Snapshot{}, fmt.Errorf("metrics parse: %w", err)
	}
	return snapshot, nil
}

// parseMetrics decodes Prometheus text format from r and returns per-user samples.
func parseMetrics(r io.Reader) ([]Sample, error) {
	snapshot, err := parseMetricSnapshot(r)
	if err != nil {
		return nil, err
	}
	return snapshot.Samples, nil
}

// parseMetricSnapshot decodes Prometheus text format from r.
func parseMetricSnapshot(r io.Reader) (Snapshot, error) {
	dec := expfmt.NewDecoder(r, expfmt.NewFormat(expfmt.TypeTextPlain))

	byUser := make(map[string]*Sample)
	byDC := make(map[string]*DCStat)
	var snapshot Snapshot

	getOrCreate := func(lbl string) *Sample {
		if s, ok := byUser[lbl]; ok {
			return s
		}
		s := &Sample{UserLabel: lbl}
		byUser[lbl] = s
		return s
	}

	getOrCreateDC := func(dc string) *DCStat {
		if d, ok := byDC[dc]; ok {
			return d
		}
		// LastLatencyMs defaults to -1 ("unknown") until a gauge sample is seen.
		d := &DCStat{DC: dc, LastLatencyMs: -1}
		byDC[dc] = d
		return d
	}

	for {
		var mf dto.MetricFamily
		if err := dec.Decode(&mf); err != nil {
			if err == io.EOF {
				break
			}
			// Tolerate parse errors on individual families.
			continue
		}

		name := mf.GetName()

		// Process all metrics in this family.
		for _, m := range mf.Metric {
			switch name {
			case metricAcceptedTotal:
				snapshot.AcceptedConnectionsTotal = metricValue(m)
				continue
			case metricFailedLRUTotal, metricFailedFloodTotal, metricIPACLRejected:
				snapshot.RejectedConnectionsTotal += metricValue(m)
				continue
			case metricSOCKS5Attempted:
				snapshot.SOCKS5.Attempted = metricValue(m)
				continue
			case metricSOCKS5Succeeded:
				snapshot.SOCKS5.Succeeded = metricValue(m)
				continue
			case metricSOCKS5Failed:
				snapshot.SOCKS5.Failed = metricValue(m)
				continue
			case metricJA4Seen:
				if hash := metricLabel(m, labelKeyJA4Hash); hash != "" {
					snapshot.JA4 = append(snapshot.JA4, JA4Counter{Hash: hash, Count: metricValue(m)})
				}
				continue
			case metricProxyProtoConns:
				snapshot.ProxyProtocol.Connections = metricValue(m)
				continue
			case metricProxyProtoErrors:
				snapshot.ProxyProtocol.Errors = metricValue(m)
				continue
			case metricDCLatencyLast:
				if dc := metricLabel(m, labelKeyDC); dc != "" {
					// Gauge is reported in seconds; surface milliseconds.
					getOrCreateDC(dc).LastLatencyMs = metricValueFloat(m) * 1000
				}
				continue
			case metricDCProbeFailures:
				if dc := metricLabel(m, labelKeyDC); dc != "" {
					getOrCreateDC(dc).ProbeFailures = metricValue(m)
				}
				continue
			}

			userLabel := ""
			for _, lp := range m.Label {
				switch lp.GetName() {
				case labelKeySecret, labelKeyLegacy:
					userLabel = lp.GetValue()
				}
				if userLabel != "" {
					break
				}
			}
			if userLabel == "" {
				continue
			}

			s := getOrCreate(userLabel)

			switch name {
			case metricBytesIn, metricBytesInLegacy:
				s.BytesIn = metricValue(m)
			case metricBytesOut, metricBytesOutLegacy:
				s.BytesOut = metricValue(m)
			case metricConnections, metricConnectionsLegacy:
				s.Connections = metricValue(m)
			case metricConnectionLimit:
				s.ConnectionLimit = metricValue(m)
			case metricUniqueIPs:
				s.UniqueIPs = metricValue(m)
			case metricMaxIPs:
				s.MaxIPs = metricValue(m)
			case metricSecretRejected:
				s.RejectedConnections = metricValue(m)
			case metricRejectedQuota:
				s.RejectedQuota = metricValue(m)
			case metricRejectedIPs:
				s.RejectedIPs = metricValue(m)
			case metricRejectedExpired:
				s.RejectedExpired = metricValue(m)
			}
		}
	}

	out := make([]Sample, 0, len(byUser))
	for _, s := range byUser {
		out = append(out, *s)
	}
	snapshot.Samples = out
	sort.Slice(snapshot.JA4, func(i, j int) bool {
		if snapshot.JA4[i].Count == snapshot.JA4[j].Count {
			return snapshot.JA4[i].Hash < snapshot.JA4[j].Hash
		}
		return snapshot.JA4[i].Count > snapshot.JA4[j].Count
	})

	dcs := make([]DCStat, 0, len(byDC))
	for _, d := range byDC {
		dcs = append(dcs, *d)
	}
	// Sort by DC number when both parse as ints, falling back to string order.
	sort.Slice(dcs, func(i, j int) bool {
		ai, aerr := strconv.Atoi(dcs[i].DC)
		bi, berr := strconv.Atoi(dcs[j].DC)
		if aerr == nil && berr == nil {
			return ai < bi
		}
		return dcs[i].DC < dcs[j].DC
	})
	snapshot.DCStats = dcs
	return snapshot, nil
}

func metricValue(m *dto.Metric) int64 {
	if m.Counter != nil {
		return int64(m.Counter.GetValue())
	}
	if m.Gauge != nil {
		return int64(m.Gauge.GetValue())
	}
	if m.Untyped != nil {
		return int64(m.Untyped.GetValue())
	}
	return 0
}

func metricValueFloat(m *dto.Metric) float64 {
	if m.Gauge != nil {
		return m.Gauge.GetValue()
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	if m.Untyped != nil {
		return m.Untyped.GetValue()
	}
	return 0
}

func metricLabel(m *dto.Metric, name string) string {
	for _, lp := range m.Label {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}
