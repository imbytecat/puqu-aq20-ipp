package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStorePersistsPrinterDeviceAndAbortsInterruptedJobs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "puqu.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	printers, err := s.Printers(ctx)
	if err != nil || len(printers) != 1 || printers[0].Uuid == "" {
		t.Fatalf("default printers = %+v, %v", printers, err)
	}
	device, err := s.SaveDevice(ctx, DeviceInput{
		NativeID: "dev-1", Name: "Q20-test", Address: "dev-1", WriteUUID: "ae01", NotifyUUID: "ae02",
	})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := s.UpdatePrinter(ctx, printers[0].ID, PrinterInput{
		Name: printers[0].Name, Driver: printers[0].Driver, DeviceID: device.ID, ProfileID: printers[0].ProfileID,
		Enabled: true, Advertise: true, AirPrint: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.CreateJob(ctx, JobInput{
		PrinterID: configured.ID, Name: "test", UserName: "tester", DocumentFormat: "image/pwg-raster", Copies: 1, Bytes: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	persistedPrinter, err := s.Printer(ctx, configured.ID)
	if err != nil || !persistedPrinter.DeviceID.Valid || persistedPrinter.DeviceID.Int64 != device.ID {
		t.Fatalf("printer = %+v, %v", persistedPrinter, err)
	}
	persisted, err := s.Job(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PrinterID != configured.ID || persisted.State != "aborted" || !persisted.Error.Valid {
		t.Fatalf("job = %+v", persisted)
	}
}

func TestStoreSupportsSharedProfilesAndExclusiveDevices(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "puqu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profile, err := s.CreateProfile(ctx, ProfileInput{
		Name: "40 x 20", WidthUM: 40000, HeightUM: 20000, GapUM: 2000, PaperType: 2, Darkness: 7, Speed: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.SaveDevice(ctx, DeviceInput{NativeID: "dev-1", Name: "Q20", Address: "dev-1", WriteUUID: "ae01"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreatePrinter(ctx, PrinterInput{
		Name: "Shipping", Slug: "shipping", DeviceID: device.ID, ProfileID: profile.ID, Enabled: true, Driver: DriverPUQUAQ20,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreatePrinter(ctx, PrinterInput{
		Name: "Returns", Slug: "returns", ProfileID: profile.ID, Enabled: true, Driver: DriverPUQUAQ20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ProfileID != second.ProfileID {
		t.Fatalf("profiles differ: %d != %d", first.ProfileID, second.ProfileID)
	}
	if _, err := s.UpdatePrinter(ctx, second.ID, PrinterInput{
		Name: second.Name, Driver: second.Driver, DeviceID: device.ID, ProfileID: profile.ID, Enabled: true,
	}); err == nil {
		t.Fatal("same physical device should not be assigned to two printers")
	}
}
