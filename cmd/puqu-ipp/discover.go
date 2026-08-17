package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-ipp-bridge/internal/usb"
)

func discoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover [serial]",
		Short: "List connected PUQU USB printers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			devices, err := usb.Scan(cmd.Context())
			if err != nil {
				return err
			}
			for _, device := range devices {
				fmt.Printf("%s %s %s\n", device.ID, device.Address, device.Name)
			}
			if len(args) == 0 {
				return nil
			}
			device, err := selectUSBDevice(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			conn, err := usb.Connect(usb.ConnectOptions{ID: device.ID})
			if err != nil {
				return err
			}
			defer conn.Disconnect()
			fmt.Printf("Connected: %s serial=%s address=%s\n", device.Name, device.ID, device.Address)
			return nil
		},
	}
}

func selectUSBDevice(ctx context.Context, id string) (usb.Device, error) {
	devices, err := usb.Scan(ctx)
	if err != nil {
		return usb.Device{}, err
	}
	for _, device := range devices {
		if id == "" || device.ID == id {
			return device, nil
		}
	}
	if id == "" {
		return usb.Device{}, errors.New("no PUQU USB printer found")
	}
	return usb.Device{}, fmt.Errorf("PUQU USB printer %q not found", id)
}
