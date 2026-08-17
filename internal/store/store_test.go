package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
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
		NativeID: "dev-1", Name: "Q20-test", Address: "/dev/bus/usb/001/010",
	})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := s.UpdatePrinter(ctx, printers[0].ID, PrinterInput{
		Name: printers[0].Name, Driver: printers[0].Driver, DeviceID: device.ID, ProfileID: printers[0].ProfileID,
		Enabled: true,
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

func TestMigrationRemovesDiscoveryColumnsWithoutLosingPrinter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "puqu.db")
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", 2); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	printers, err := s.Printers(ctx)
	if err != nil || len(printers) != 1 {
		t.Fatalf("printers = %+v, %v", printers, err)
	}
	var obsolete int64
	if err := s.db.QueryRowContext(ctx, "SELECT advertise FROM printers LIMIT 1").Scan(&obsolete); err == nil {
		t.Fatal("obsolete discovery columns still exist")
	}
}

func TestUSBMigrationClearsBluetoothAssignmentAndRestoresItOnRollback(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "puqu.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", 3); err != nil {
		t.Fatal(err)
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO ble_devices (native_id, name, address, write_uuid, notify_uuid, last_seen_at, updated_at)
		VALUES ('ble-1', 'AQ20 BLE', 'AA:BB:CC:DD:EE:FF', 'ae01', 'ae02', 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	deviceID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE printers SET device_id = ?", deviceID); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", 4); err != nil {
		t.Fatal(err)
	}
	var assigned sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT device_id FROM printers LIMIT 1").Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned.Valid {
		t.Fatalf("Bluetooth assignment survived USB cutover: %d", assigned.Int64)
	}
	if err := goose.DownTo(db, "migrations", 3); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT device_id FROM printers LIMIT 1").Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if !assigned.Valid || assigned.Int64 != deviceID {
		t.Fatalf("rollback assignment = %+v, want %d", assigned, deviceID)
	}
}

func TestOfficialRasterSettingsMigrationRestoresLegacyValues(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "puqu.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", 4); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE label_profiles SET paper_type = 3, darkness = 11, speed = 5"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", 5); err != nil {
		t.Fatal(err)
	}
	var halftone, brightness int64
	if err := db.QueryRowContext(ctx, "SELECT halftone_method, brightness FROM label_profiles LIMIT 1").Scan(&halftone, &brightness); err != nil {
		t.Fatal(err)
	}
	if halftone != 0 || brightness != 0 {
		t.Fatalf("official defaults = %d/%d, want 0/0", halftone, brightness)
	}
	if err := goose.DownTo(db, "migrations", 4); err != nil {
		t.Fatal(err)
	}
	var paperType, darkness, speed int64
	if err := db.QueryRowContext(ctx, "SELECT paper_type, darkness, speed FROM label_profiles LIMIT 1").Scan(&paperType, &darkness, &speed); err != nil {
		t.Fatal(err)
	}
	if paperType != 3 || darkness != 11 || speed != 5 {
		t.Fatalf("restored settings = %d/%d/%d", paperType, darkness, speed)
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
		Name: "40 x 20", WidthUM: 40000, HeightUM: 20000, GapUM: 2000, HalftoneMethod: 1, Brightness: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	device, err := s.SaveDevice(ctx, DeviceInput{NativeID: "dev-1", Name: "Q20", Address: "/dev/bus/usb/001/010"})
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
