package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-aq20-ipp/internal/ble"
)

func discoverCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "discover [namePrefix]",
		Short: "Connect and dump the GATT table and selected endpoints",
		RunE: func(_ *cobra.Command, args []string) error {
			namePrefix := "Q20"
			if len(args) > 0 {
				namePrefix = args[0]
			}
			conn, err := ble.Connect(ble.ConnectOptions{NamePrefix: namePrefix, ID: id})
			if err != nil {
				return err
			}
			defer func() {
				_ = conn.Disconnect()
				ble.Shutdown()
			}()
			info := conn.Info()
			fmt.Printf("Connected: %s address=%s\n", info.Name, info.Address)
			for _, svc := range conn.Gatt() {
				fmt.Printf("Service %s\n", svc.UUID)
				for _, characteristic := range svc.Characteristics {
					fmt.Printf("  char %s [%s]\n", characteristic.UUID, strings.Join(characteristic.Properties, ", "))
				}
			}
			notify := "none"
			if info.NotifyChar != nil {
				notify = *info.NotifyChar
			}
			fmt.Printf("writeUuid=%s notifyUuid=%s writeWithoutResponse=%v\n", info.WriteChar, notify, info.WithoutResponse)
			if info.NotifyChar != nil {
				conn.OnData(func(data []byte) { fmt.Printf("notify <- %x\n", data) })
				time.Sleep(4 * time.Second)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "match an exact native Bluetooth id")
	return cmd
}
