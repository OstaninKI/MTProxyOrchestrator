package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "tgproxy-panel",
	Short:   "MTProto proxy admin panel",
	Version: version.Version,
}

var (
	dbPath      string
	panelPath   string
	listenAddr  string
	mtprotoPort int
	maskHost    string
	statsPort   int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the admin panel HTTP server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVar(&dbPath, "db", "/etc/tgproxy/panel.db", "path to SQLite database")
	serveCmd.Flags().StringVar(&panelPath, "path", "/p-changeme/", "panel URL path prefix")
	serveCmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:8443", "listen address")
	serveCmd.Flags().IntVar(&mtprotoPort, "mtproto-port", 443, "MTProto listen port used when rendering Teleproxy config")
	serveCmd.Flags().StringVar(&maskHost, "mask-host", "www.microsoft.com", "FakeTLS mask host used when rendering Teleproxy config")
	serveCmd.Flags().IntVar(&statsPort, "stats-port", 9091, "Teleproxy stats port used when rendering Teleproxy config")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	d, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer d.Close()

	srv := &panel.Server{
		DB:          d,
		PanelPath:   panelPath,
		RateLimiter: panel.NewRateLimiter(),
		Secure:      true,
		BridgeCfg: &panel.BridgeConfig{
			MTProtoPort: mtprotoPort,
			MaskHost:    maskHost,
			StatsPort:   statsPort,
		},
	}

	fmt.Fprintf(cmd.OutOrStdout(), "panel listening on %s%s\n", listenAddr, panelPath)
	return http.ListenAndServe(listenAddr, srv.Handler())
}
