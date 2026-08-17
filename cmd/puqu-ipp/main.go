// Command puqu-ipp exposes PUQU AQ20 Bluetooth printers through direct IPP queues.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-aq20-ipp/internal/config"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "puqu-ipp",
		Short:         "Driverless IPP bridge for PUQU AQ20 Bluetooth printers",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return runServe(cmd) },
	}
	root.PersistentFlags().String("config", "", "TOML config file (default: OS user config directory)")
	root.PersistentFlags().String("data", "", "SQLite database path")
	root.PersistentFlags().String("ipp-listen", config.DefaultIPPListen, "IPP listen address")
	root.PersistentFlags().String("admin-listen", config.DefaultAdminListen, "Local admin UI listen address")
	root.PersistentFlags().String("log-level", config.DefaultLogLevel, "Log level: debug, info, warn, or error")
	root.AddCommand(serveCmd(), discoverCmd(), printCmd(), smokeCmd(), serviceCmd(), serviceRunCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
