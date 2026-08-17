//go:build linux

package ble

import (
	"strings"

	"github.com/godbus/dbus/v5"
)

// charFlags reads each GATT characteristic's BlueZ "Flags" straight from bluetoothd
// over D-Bus, keyed by lowercased 128-bit UUID. tinygo/bluetooth doesn't surface
// properties on Linux, so this restores property-based auto-pick and the full GATT
// table the UI shows. Best-effort: returns nil on any failure (auto-pick then falls
// back to pinned UUIDs / notify-probe).
func charFlags(deviceAddr string) map[string][]string {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil
	}

	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	obj := conn.Object("org.bluez", "/")
	if err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&managed); err != nil {
		return nil
	}

	// BlueZ device path fragment, e.g. "dev_CD_AC_BB_8F_9B_6C".
	frag := "dev_" + strings.ToUpper(strings.ReplaceAll(deviceAddr, ":", "_"))

	out := map[string][]string{}
	for path, ifaces := range managed {
		if !strings.Contains(string(path), frag) {
			continue
		}
		props, ok := ifaces["org.bluez.GattCharacteristic1"]
		if !ok {
			continue
		}
		uuid, _ := props["UUID"].Value().(string)
		flags, _ := props["Flags"].Value().([]string)
		if uuid == "" {
			continue
		}
		out[strings.ToLower(uuid)] = toProperties(flags)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
