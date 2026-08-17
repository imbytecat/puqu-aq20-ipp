//go:build linux

package ble

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestStaleBlueZWriteDisconnectsLink(t *testing.T) {
	conn := &Conn{address: "AA:BB:CC:DD:EE:FF"}
	disconnected := false
	conn.OnDisconnect(func() { disconnected = true })

	message := `Method "WriteValue" with signature "aya{sv}" on interface "org.bluez.GattCharacteristic1" doesn't exist`
	err := dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod", Body: []any{message}}
	if got := conn.handleWriteError(err); got.Error() != message {
		t.Fatalf("error = %q, want %q", got, message)
	}
	if conn.IsConnected() {
		t.Fatal("stale GATT method should invalidate the connection")
	}
	if !disconnected {
		t.Fatal("stale GATT method should notify the reconnect loop")
	}
}

func TestNonFatalBlueZWriteErrorKeepsLink(t *testing.T) {
	conn := &Conn{address: "AA:BB:CC:DD:EE:FF"}
	err := dbus.Error{Name: "org.bluez.Error.NotPermitted", Body: []any{"write not permitted"}}

	_ = conn.handleWriteError(err)
	if !conn.IsConnected() {
		t.Fatal("non-fatal write error should not discard the connection")
	}
}
