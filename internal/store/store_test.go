package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStorePersistsConfigurationAndAbortsInterruptedJobs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "puqu.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.PrinterUuid == "" || settings.IppListen != ":8631" {
		t.Fatalf("settings = %+v", settings)
	}
	device, err := s.SaveDevice(ctx, DeviceInput{
		NativeID: "dev-1", Name: "Q20-test", Address: "dev-1", WriteUUID: "ae01", NotifyUUID: "ae02", Selected: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if device.Selected != 1 {
		t.Fatalf("device not selected: %+v", device)
	}
	job, err := s.CreateJob(ctx, JobInput{Name: "test", UserName: "tester", DocumentFormat: "image/pwg-raster", Copies: 1, Bytes: 42})
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
	selected, err := s.SelectedDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if selected.NativeID != "dev-1" {
		t.Fatalf("selected = %+v", selected)
	}
	persisted, err := s.Job(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != "aborted" || !persisted.Error.Valid {
		t.Fatalf("job = %+v", persisted)
	}
}

func TestStoreKeepsSingleActiveProfile(t *testing.T) {
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
	if err := s.ActivateProfile(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}
	profiles, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, item := range profiles {
		if item.Active == 1 {
			active++
			if item.ID != profile.ID {
				t.Fatalf("wrong active profile: %+v", item)
			}
		}
	}
	if active != 1 {
		t.Fatalf("active profiles = %d", active)
	}
}
func TestSettingsRejectRemoteAdminListener(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "puqu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.UpdateSettings(ctx, SettingsUpdate{
		IPPName: "PUQU", IPPListen: ":8631", AdminListen: "0.0.0.0:8080", Advertise: true,
	})
	if err == nil {
		t.Fatal("remote admin listener should be rejected")
	}
}
