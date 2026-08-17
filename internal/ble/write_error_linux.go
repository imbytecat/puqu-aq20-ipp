//go:build linux

package ble

import (
	"errors"

	"github.com/godbus/dbus/v5"
)

func isStaleGattError(err error) bool {
	var busErr dbus.Error
	if !errors.As(err, &busErr) {
		return false
	}
	switch busErr.Name {
	case "org.freedesktop.DBus.Error.UnknownMethod",
		"org.freedesktop.DBus.Error.UnknownObject",
		"org.freedesktop.DBus.Error.NoSuchObject",
		"org.freedesktop.DBus.Error.UnknownInterface",
		"org.bluez.Error.NotConnected":
		return true
	default:
		return false
	}
}
