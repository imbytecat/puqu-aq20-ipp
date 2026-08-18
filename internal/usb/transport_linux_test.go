//go:build linux

package usb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestScanFindsStableAQ20Identity(t *testing.T) {
	root := t.TempDir()
	device := filepath.Join(root, "1-2")
	if err := os.Mkdir(device, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"idVendor": "8888\n", "idProduct": "0026\n", "busnum": "1\n", "devnum": "10\n",
		"serial": "4250313332393404\n", "product": "PUQU Label Printer\n",
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(device, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	devices, err := scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %+v", devices)
	}
	got := devices[0]
	if got.ID != "4250313332393404" || got.Name != "PUQU Label Printer" || got.Address != "/dev/bus/usb/001/010" {
		t.Fatalf("device = %+v", got)
	}
}

func TestClaimPrinterInterfaceDetachesOnlyUsblp(t *testing.T) {
	calls := 0
	err := claimPrinterInterface(7, func(fd, request uintptr, data unsafe.Pointer) error {
		calls++
		if fd != 7 {
			t.Fatalf("fd = %d", fd)
		}
		switch request {
		case claimInterfaceRequest:
			if iface := *(*uint32)(data); iface != interfaceNumber {
				t.Fatalf("interface = %d", iface)
			}
			return unix.EBUSY
		case disconnectClaimRequest:
			claim := (*disconnectClaim)(data)
			if claim.Interface != interfaceNumber || claim.Flags != disconnectClaimIfDriver || string(claim.Driver[:5]) != "usblp" {
				t.Fatalf("disconnect claim = %+v", claim)
			}
			return nil
		default:
			t.Fatalf("unexpected ioctl request %#x", request)
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestClaimPrinterInterfacePreservesOtherOwners(t *testing.T) {
	err := claimPrinterInterface(7, func(_ uintptr, request uintptr, _ unsafe.Pointer) error {
		if request == claimInterfaceRequest || request == disconnectClaimRequest {
			return unix.EBUSY
		}
		return nil
	})
	if !errors.Is(err, unix.EBUSY) {
		t.Fatalf("error = %v, want EBUSY", err)
	}
}

func TestMonitorChecksOnlyOnTicks(t *testing.T) {
	done := make(chan struct{})
	ticks := make(chan time.Time)
	checked := make(chan struct{})
	result := make(chan error, 1)
	checks := 0
	go func() {
		result <- monitor(done, ticks, func() error {
			checks++
			checked <- struct{}{}
			return nil
		})
	}()
	for range 3 {
		ticks <- time.Time{}
		<-checked
	}
	close(done)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if checks != 3 {
		t.Fatalf("checks = %d, want 3", checks)
	}
}
