package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-aq20-ipp/internal/ble"
	"github.com/imbytecat/puqu-aq20-ipp/internal/puqu"
)

func printCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "print-test [namePrefix]",
		Short: "Connect directly and print a striped hardware test label",
		RunE: func(_ *cobra.Command, args []string) error {
			namePrefix := "Q20"
			if len(args) > 0 {
				namePrefix = args[0]
			}
			const widthBytes, height = 40, 96
			fmt.Printf("Connecting to %q...\n", namePrefix+"*")
			conn, err := ble.Connect(ble.ConnectOptions{NamePrefix: namePrefix})
			if err != nil {
				return err
			}
			defer func() {
				_ = conn.Disconnect()
				ble.Shutdown()
			}()
			bitmap := stripes(widthBytes, height)
			for _, payload := range [][]byte{puqu.Wake(), puqu.PrintHeader(widthBytes, height, len(bitmap)), bitmap} {
				if err := conn.Write(payload); err != nil {
					return err
				}
				time.Sleep(60 * time.Millisecond)
			}
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
