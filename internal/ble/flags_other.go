//go:build !linux

package ble

// charFlags is a no-op off Linux: tinygo/bluetooth already uses the native OS stack
// (CoreBluetooth/WinRT) there, and BlueZ's D-Bus flag table doesn't exist. Auto-pick
// falls back to pinned UUIDs and the notify-probe; see connect().
func charFlags(_ string) map[string][]string { return nil }
