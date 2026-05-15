package metrics

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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
	labelKeyLegacy          = "label"
	labelKeySecret          = "secret"
	defaultHTTPTimeout      = 10 * time.Second
)

// MaxResponseBytes caps a single Prometheus scrape response.
const MaxResponseBytes = 1024 * 1024

// Sample holds one point-in-time measurement for a single user.
type Sample struct {
	UserLabel   string
	BytesIn     int64
	BytesOut    int64
	Connections int64
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
	resp, err := s.Client.Get(s.StatsAddr + "/metrics")
	if err != nil {
		return nil, fmt.Errorf("metrics scrape: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics scrape: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("metrics read: %w", err)
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("metrics response too large")
	}

	samples, err := parseMetrics(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("metrics parse: %w", err)
	}
	return samples, nil
}

// parseMetrics decodes Prometheus text format from r and returns per-user samples.
func parseMetrics(r io.Reader) ([]Sample, error) {
	dec := expfmt.NewDecoder(r, expfmt.NewFormat(expfmt.TypeTextPlain))

	byUser := make(map[string]*Sample)

	getOrCreate := func(lbl string) *Sample {
		if s, ok := byUser[lbl]; ok {
			return s
		}
		s := &Sample{UserLabel: lbl}
		byUser[lbl] = s
		return s
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
				if m.Counter != nil {
					s.BytesIn = int64(m.Counter.GetValue())
				} else if m.Gauge != nil {
					s.BytesIn = int64(m.Gauge.GetValue())
				} else if m.Untyped != nil {
					s.BytesIn = int64(m.Untyped.GetValue())
				}
			case metricBytesOut, metricBytesOutLegacy:
				if m.Counter != nil {
					s.BytesOut = int64(m.Counter.GetValue())
				} else if m.Gauge != nil {
					s.BytesOut = int64(m.Gauge.GetValue())
				} else if m.Untyped != nil {
					s.BytesOut = int64(m.Untyped.GetValue())
				}
			case metricConnections, metricConnectionsLegacy:
				if m.Gauge != nil {
					s.Connections = int64(m.Gauge.GetValue())
				} else if m.Counter != nil {
					s.Connections = int64(m.Counter.GetValue())
				} else if m.Untyped != nil {
					s.Connections = int64(m.Untyped.GetValue())
				}
			}
		}
	}

	if len(byUser) == 0 {
		return nil, nil
	}

	out := make([]Sample, 0, len(byUser))
	for _, s := range byUser {
		out = append(out, *s)
	}
	return out, nil
}
