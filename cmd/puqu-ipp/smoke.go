package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-ipp-bridge/internal/usb"
)

func smokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "smoke",
		Short: "Check direct access to a PUQU USB printer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			device, err := selectUSBDevice(cmd.Context(), "")
			if err != nil {
				return err
			}
			conn, err := usb.Connect(usb.ConnectOptions{ID: device.ID})
			if err != nil {
				return err
			}
			defer conn.Disconnect()
			fmt.Printf("connected: %s serial=%s address=%s\n", device.Name, device.ID, device.Address)
			return nil
		},
	}
}
