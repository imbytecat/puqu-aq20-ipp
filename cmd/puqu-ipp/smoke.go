package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-ipp-bridge/internal/ble"
)

func smokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "smoke",
		Short: "Check access to the native Bluetooth stack",
		RunE: func(_ *cobra.Command, _ []string) error {
			devices, err := ble.Scan(6 * time.Second)
			if err != nil {
				return err
			}
			defer ble.Shutdown()
			fmt.Printf("devices seen: %d\n", len(devices))
			for _, device := range devices {
				marker := ""
				name := strings.ToUpper(device.Name)
				if strings.Contains(name, "Q20") || strings.Contains(name, "PUQU") || strings.Contains(name, "AQ") {
					marker = "  possible PUQU printer"
				}
				fmt.Printf("%s %s%s\n", device.Address, device.Name, marker)
			}
			return nil
		},
	}
}
