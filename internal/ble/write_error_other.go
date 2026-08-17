//go:build !linux

package ble

func isStaleGattError(error) bool { return false }
