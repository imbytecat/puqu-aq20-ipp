//go:build linux

package ble

import (
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestStaleBlueZWriteDisconnectsLink(t *testing.T) {
	conn := &Conn{address: "AA:BB:CC:DD:EE:FF"}
	disconnected := false
	conn.OnDisconnect(func() { disconnected = true })

	message := `Method "WriteValue" with signature "aya{sv}" on interface "org.bluez.GattCharacteristic1" doesn't exist`
	err := dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod", Body: []any{message}}
	got := conn.handleWriteError(err)
	if !errors.Is(got, ErrStaleGatt) {
		t.Fatalf("error = %v, want ErrStaleGatt", got)
	}
	if got.Error() != ErrStaleGatt.Error()+": "+message {
		t.Fatalf("error = %q", got)
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
