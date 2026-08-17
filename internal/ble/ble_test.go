package ble

import (
	"reflect"
	"testing"
)

func TestUUIDEq(t *testing.T) {
	full := "0000ae01-0000-1000-8000-00805f9b34fb"
	tests := []struct {
		a, b string
		want bool
	}{
		{"ae01", full, true},
		{"AE01", full, true},
		{"0000AE01-0000-1000-8000-00805F9B34FB", "ae01", true},
		{full, full, true},
		{"ae01", "ae02", false},
		{"ae01", "0000ae02-0000-1000-8000-00805f9b34fb", false},
	}
	for _, tc := range tests {
		if got := uuidEq(tc.a, tc.b); got != tc.want {
			t.Errorf("uuidEq(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestShortUUID(t *testing.T) {
	if got := shortUUID("0000ae30-0000-1000-8000-00805f9b34fb"); got != "ae30" {
		t.Errorf("standard 16-bit: got %q, want ae30", got)
	}
	// a non-base-range 128-bit UUID stays fully normalized (dashes stripped, lowercased)
	vendor := "12345678-1234-5678-1234-567812345678"
	if got := shortUUID(vendor); got != "12345678123456781234567812345678" {
		t.Errorf("vendor UUID: got %q", got)
	}
}

func TestToProperties(t *testing.T) {
	got := toProperties([]string{"write-without-response", "write", "notify", "read", "indicate", "broadcast"})
	want := []string{"writeWithoutResponse", "write", "notify", "read", "indicate", "broadcast"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// unknown flags pass through unchanged
	if got := toProperties([]string{"authenticated-signed-writes", "notify"}); !reflect.DeepEqual(got, []string{"authenticated-signed-writes", "notify"}) {
		t.Fatalf("unknown-flag passthrough: got %v", got)
	}
}
