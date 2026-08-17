package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-ipp-bridge/internal/printer"
	"github.com/imbytecat/puqu-ipp-bridge/internal/usb"
)

func printCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "print-test [serial]",
		Short: "Print a striped hardware test label over USB",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) != 0 {
				id = args[0]
			}
			device, err := selectUSBDevice(cmd.Context(), id)
			if err != nil {
				return err
			}
			fmt.Printf("Connecting to USB printer %s...\n", device.ID)
			conn, err := usb.Connect(usb.ConnectOptions{ID: device.ID})
			if err != nil {
				return err
			}
			p := printer.New(conn, nil)
			defer p.Disconnect()

			const widthBytes, height = 40, 96
			bitmap := stripes(widthBytes, height)
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result, err := p.Print(ctx, []printer.Job{{
				WidthBytes: widthBytes, HeightPx: height, Data: bitmap, Copies: 1,
			}})
			if err != nil {
				return err
			}
			fmt.Printf("Printed %d bytes\n", result.Bytes)
			time.Sleep(2500 * time.Millisecond)
			return nil
		},
	}
}

func stripes(widthBytes, height int) []byte {
	out := make([]byte, widthBytes*height)
	for y := range height {
		if (y/8)%2 == 0 {
			for x := range widthBytes {
				out[y*widthBytes+x] = 0xff
			}
		}
	}
	return out
}
