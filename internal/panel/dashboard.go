package panel

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
)

// ComponentVersion holds the name and installed version of a component.
type ComponentVersion struct {
	Name    string
	Version string // "unknown" if binary not found or command fails
}

// DashboardData holds what the dashboard template receives.
type DashboardData struct {
	Services        []health.ServiceState
	BridgeSteps     []health.BridgeStepStatus // populated in Bridge mode
	IsBridge        bool
	Healthy         bool
	HealthLabel     string
	CurrentNav      string
	PanelPath       string
	CSRFField       string
	CSRFToken       string
	Period          metrics.Period
	TopUsers        []metrics.UserTraffic
	TrafficSeries   []metrics.TrafficBucket
	LiveConnections []metrics.Sample // latest per-user active connection counts
	Components      []ComponentVersion
	Users           []UserRow
	BridgeNodes     []bridge.Node
	System          SystemSnapshot
}

// SystemSnapshot holds best-effort host stats for the dashboard.
type SystemSnapshot struct {
	MemoryPercent float64
	DiskPercent   float64
	MemoryTotal   int64 // total RAM in bytes, 0 when unknown
	DiskTotal     int64 // total disk capacity of "/" in bytes, 0 when unknown
	LoadAvg       string
	Uptime        string
	Kernel        string
}

// collectComponentVersions returns installed versions for all known components.
func collectComponentVersions() []ComponentVersion {
	paths := config.DefaultPaths()

	// teleproxy has no --version flag; it is a C fork of Telegram's MTProxy
	// and only prints its version banner ("teleproxy-X.Y.Z compiled at ...")
	// from usage(), which exits with a non-zero status. Run `--help` and parse
	// the banner from the output regardless of the exit code.
	teleproxyVersion := "unknown"
	if out, _ := exec.Command(paths.TeleproxyBin, "--help").CombinedOutput(); len(out) > 0 {
		if v := parseTeleproxyVersion(string(out)); v != "" {
			teleproxyVersion = v
		}
	}

	singboxVersion := "unknown"
	if out, err := exec.Command(paths.SingboxBin, "version").CombinedOutput(); err == nil {
		if v := parseSingboxVersion(string(out)); v != "" {
			singboxVersion = v
		}
	}

	return []ComponentVersion{
		{Name: "tgproxy-cli", Version: version.Version},
		{Name: "tgproxy-panel", Version: version.Version},
		{Name: "teleproxy", Version: teleproxyVersion},
		{Name: "sing-box", Version: singboxVersion},
	}
}

// parseTeleproxyVersion extracts the semantic version from teleproxy's banner.
// The banner line looks like "teleproxy-4.15.0 compiled at Jan 1 2026 ...";
// it returns "4.15.0", or "" if no banner is found.
func parseTeleproxyVersion(out string) string {
	const prefix = "teleproxy-"
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		rest := line[i+len(prefix):]
		if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			rest = rest[:sp]
		}
		if v := strings.TrimSpace(rest); v != "" {
			return v
		}
	}
	return ""
}

// parseSingboxVersion extracts the version number from sing-box's `version`
// output. The first line looks like "sing-box version 1.13.12"; it returns
// "1.13.12", or "" if the output is empty.
func parseSingboxVersion(out string) string {
	line := strings.TrimSpace(out)
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// isBridgeMode returns true when Bridge mode is active, determined by the
// presence of at least one enabled node in outbounds.json.
func (s *Server) isBridgeMode() bool {
	if mode, err := teleproxy.DetectMode(s.bridgePaths().TeleproxyTOML); err == nil {
		return mode == config.ModeBridge
	}
	nl, err := bridge.Load(s.nodePath())
	if err != nil {
		return false
	}
	return len(nl.Active()) > 0
}

// statsAddr returns the Teleproxy metrics base URL, e.g. "http://127.0.0.1:9091".
func (s *Server) statsAddr() string {
	port := s.bridgeStatsPort()
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// scrapeLiveConnections fetches current per-user metrics from Teleproxy without
// persisting them. Returns nil on error (dashboard degrades gracefully).
func (s *Server) scrapeLiveConnections() []metrics.Sample {
	scraper := metrics.DefaultScraper(s.statsAddr())
	samples, err := scraper.Scrape()
	if err != nil {
		return nil
	}
	// Scrape returns samples in map (non-deterministic) order, which makes the
	// dashboard list reshuffle on every refresh. Sort into a stable order:
	// active users (open connections) first, idle users last, and by user label
	// within each group.
	sort.SliceStable(samples, func(i, j int) bool {
		a, b := samples[i], samples[j]
		ai, bi := a.Connections > 0, b.Connections > 0
		if ai != bi {
			return ai // active before inactive
		}
		return a.UserLabel < b.UserLabel
	})
	return samples
}

func collectSystemSnapshot() SystemSnapshot {
	snapshot := SystemSnapshot{
		MemoryPercent: -1,
		DiskPercent:   -1,
		LoadAvg:       "unknown",
		Uptime:        "unknown",
		Kernel:        "unknown",
	}

	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		snapshot.Kernel = strings.TrimSpace(string(out))
	}

	if file, err := os.Open("/proc/loadavg"); err == nil {
		defer file.Close()
		var load string
		if _, err := fmt.Fscan(file, &load); err == nil && load != "" {
			snapshot.LoadAvg = load
		}
	}

	if file, err := os.Open("/proc/uptime"); err == nil {
		defer file.Close()
		var uptimeSeconds float64
		if _, err := fmt.Fscan(file, &uptimeSeconds); err == nil {
			snapshot.Uptime = humanDuration(time.Duration(uptimeSeconds) * time.Second)
		}
	}

	if meminfo, err := readMemInfo("/proc/meminfo"); err == nil {
		total := meminfo["MemTotal"]
		available := meminfo["MemAvailable"]
		if total > 0 && available >= 0 {
			used := total - available
			snapshot.MemoryPercent = percent(float64(used), float64(total))
			// /proc/meminfo reports kibibytes; convert to bytes.
			snapshot.MemoryTotal = total * 1024
		}
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(filepath.Clean("/"), &stat); err == nil && stat.Blocks > 0 {
		total := float64(stat.Blocks) * float64(stat.Bsize)
		free := float64(stat.Bavail) * float64(stat.Bsize)
		used := total - free
		snapshot.DiskPercent = percent(used, total)
		snapshot.DiskTotal = int64(total)
	}

	return snapshot
}

func readMemInfo(path string) (map[string]int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(map[string]int64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		values[strings.TrimSuffix(parts[0], ":")] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func percent(used, total float64) float64 {
	if total <= 0 {
		return -1
	}
	return (used / total) * 100
}

func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	minutes := int(d / time.Minute)
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	period := metrics.ParsePeriod(r.URL.Query().Get("period"))
	csrfToken, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "failed to create csrf token", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, csrfToken, s.Secure, s.PanelPath)

	data := s.collectDashboardData(period)
	data.CSRFField = CSRFField()
	data.CSRFToken = csrfToken

	setStrictPanelCSP(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	dashboardPage(w, data)
}

func (s *Server) handleDashboardFragment(w http.ResponseWriter, r *http.Request) {
	period := metrics.ParsePeriod(r.URL.Query().Get("period"))
	data := s.collectDashboardData(period)

	setStrictPanelCSP(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	switch chi.URLParam(r, "name") {
	case "health":
		dashboardHealthFragment(w, data)
	case "connections":
		dashboardConnectionsFragment(w, data)
	case "traffic":
		dashboardTrafficFragment(w, data)
	case "components":
		dashboardComponentsFragment(w, data)
	default:
		http.NotFound(w, r)
	}
}
