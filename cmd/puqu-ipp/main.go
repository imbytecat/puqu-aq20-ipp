// Command puqu-ipp exposes PUQU AQ20 Bluetooth printers through IPP Everywhere.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "puqu-ipp",
		Short:         "IPP Everywhere bridge for PUQU AQ20 Bluetooth printers",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return runServe(cmd) },
	}
	root.PersistentFlags().String("data", "", "SQLite database path (default: OS user config directory)")
	root.PersistentFlags().String("ipp-listen", envOr("PUQU_IPP_LISTEN", ":8631"), "IPP listen address")
	root.PersistentFlags().String("admin-listen", envOr("PUQU_ADMIN_LISTEN", "127.0.0.1:8080"), "Local admin UI listen address")
	root.AddCommand(serveCmd(), discoverCmd(), printCmd(), smokeCmd(), serviceCmd(), serviceRunCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
