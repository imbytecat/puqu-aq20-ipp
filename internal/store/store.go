package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/pressly/goose/v3"

	"github.com/imbytecat/puqu-aq20-ipp/internal/store/sqlitedb"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Settings = sqlitedb.AppSetting
type Device = sqlitedb.BleDevice
type Profile = sqlitedb.LabelProfile
type Job = sqlitedb.PrintJob

type Store struct {
	db *sql.DB
	q  *sqlitedb.Queries
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "puqu-aq20-ipp", "puqu.db"), nil
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	dsn := "file:" + filepath.ToSlash(path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, err
		}
	}

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		db.Close()
		return nil, err
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db, q: sqlitedb.New(db)}
	if err := s.ensurePrinterUUID(ctx); err != nil {
		db.Close()
		return nil, err
	}
	now := nullableTime(time.Now())
	if err := s.q.AbortInterruptedJobs(ctx, now); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.q.PruneJobs(ctx, 500); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ensurePrinterUUID(ctx context.Context) error {
	settings, err := s.q.GetSettings(ctx)
	if err != nil {
		return err
	}
	if settings.PrinterUuid != "" {
		return nil
	}
	id, err := randomUUID()
	if err != nil {
		return err
	}
	return s.q.SetPrinterUUID(ctx, sqlitedb.SetPrinterUUIDParams{
		PrinterUuid: id,
		UpdatedAt:   unixMillis(time.Now()),
	})
}

func (s *Store) Settings(ctx context.Context) (*Settings, error) {
	return s.q.GetSettings(ctx)
}

type SettingsUpdate struct {
	IPPName     string
	IPPListen   string
	AdminListen string
	Advertise   bool
	AirPrint    bool
}

func (s *Store) UpdateSettings(ctx context.Context, update SettingsUpdate) (*Settings, error) {
	update.IPPName = strings.TrimSpace(update.IPPName)
	update.IPPListen = strings.TrimSpace(update.IPPListen)
	update.AdminListen = strings.TrimSpace(update.AdminListen)
	if update.IPPName == "" || update.IPPListen == "" || update.AdminListen == "" {
		return nil, errors.New("IPP name and listen addresses are required")
	}
	return s.q.UpdateSettings(ctx, sqlitedb.UpdateSettingsParams{
		IppName:     update.IPPName,
		IppListen:   update.IPPListen,
		AdminListen: update.AdminListen,
		Advertise:   boolInt(update.Advertise),
		Airprint:    boolInt(update.AirPrint),
		UpdatedAt:   unixMillis(time.Now()),
	})
}

func (s *Store) Devices(ctx context.Context) ([]*Device, error) {
	return s.q.ListDevices(ctx)
}

func (s *Store) SelectedDevice(ctx context.Context) (*Device, error) {
	return s.q.GetSelectedDevice(ctx)
}

type DeviceInput struct {
	NativeID   string
	Name       string
	Address    string
	WriteUUID  string
	NotifyUUID string
	Selected   bool
}

func (s *Store) SaveDevice(ctx context.Context, input DeviceInput) (*Device, error) {
	input.NativeID = strings.TrimSpace(input.NativeID)
	input.Name = strings.TrimSpace(input.Name)
	input.Address = strings.TrimSpace(input.Address)
	input.WriteUUID = strings.TrimSpace(input.WriteUUID)
	input.NotifyUUID = strings.TrimSpace(input.NotifyUUID)
	if input.NativeID == "" || input.Name == "" || input.Address == "" || input.WriteUUID == "" {
		return nil, errors.New("device id, name, address, and write UUID are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	now := unixMillis(time.Now())
	device, err := q.UpsertDevice(ctx, sqlitedb.UpsertDeviceParams{
		NativeID:   input.NativeID,
		Name:       input.Name,
		Address:    input.Address,
		WriteUuid:  input.WriteUUID,
		NotifyUuid: nullableString(input.NotifyUUID),
		LastSeenAt: now,
		UpdatedAt:  now,
	})
	if err != nil {
		return nil, err
	}
	if input.Selected {
		if err := q.ClearSelectedDevice(ctx); err != nil {
			return nil, err
		}
		rows, err := q.SelectDevice(ctx, sqlitedb.SelectDeviceParams{UpdatedAt: now, ID: device.ID})
		if err != nil {
			return nil, err
		}
		if rows != 1 {
			return nil, sql.ErrNoRows
		}
		device.Selected = 1
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *Store) SelectDevice(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	if err := q.ClearSelectedDevice(ctx); err != nil {
		return err
	}
	rows, err := q.SelectDevice(ctx, sqlitedb.SelectDeviceParams{UpdatedAt: unixMillis(time.Now()), ID: id})
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	rows, err := s.q.DeleteDevice(ctx, id)
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Profiles(ctx context.Context) ([]*Profile, error) {
	return s.q.ListProfiles(ctx)
}

func (s *Store) ActiveProfile(ctx context.Context) (*Profile, error) {
	return s.q.GetActiveProfile(ctx)
}

type ProfileInput struct {
	Name      string
	WidthUM   int64
	HeightUM  int64
	GapUM     int64
	PaperType int64
	Darkness  int64
	Speed     int64
}

func validateProfile(input ProfileInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return errors.New("profile name is required")
	}
	if input.WidthUM <= 0 || input.HeightUM <= 0 || input.GapUM < 0 {
		return errors.New("profile dimensions are invalid")
	}
	if input.PaperType < 1 || input.PaperType > 3 || input.Darkness < 0 || input.Darkness > 11 || input.Speed < 0 || input.Speed > 5 {
		return errors.New("profile printer settings are invalid")
	}
	return nil
}

func (s *Store) CreateProfile(ctx context.Context, input ProfileInput) (*Profile, error) {
	input.Name = strings.TrimSpace(input.Name)
	if err := validateProfile(input); err != nil {
		return nil, err
	}
	now := unixMillis(time.Now())
	return s.q.CreateProfile(ctx, sqlitedb.CreateProfileParams{
		Name: input.Name, WidthUm: input.WidthUM, HeightUm: input.HeightUM, GapUm: input.GapUM,
		PaperType: input.PaperType, Darkness: input.Darkness, Speed: input.Speed,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Store) UpdateProfile(ctx context.Context, id int64, input ProfileInput) (*Profile, error) {
	input.Name = strings.TrimSpace(input.Name)
	if err := validateProfile(input); err != nil {
		return nil, err
	}
	return s.q.UpdateProfile(ctx, sqlitedb.UpdateProfileParams{
		Name: input.Name, WidthUm: input.WidthUM, HeightUm: input.HeightUM, GapUm: input.GapUM,
		PaperType: input.PaperType, Darkness: input.Darkness, Speed: input.Speed,
		UpdatedAt: unixMillis(time.Now()), ID: id,
	})
}

func (s *Store) ActivateProfile(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	if err := q.ClearActiveProfile(ctx); err != nil {
		return err
	}
	rows, err := q.ActivateProfile(ctx, sqlitedb.ActivateProfileParams{UpdatedAt: unixMillis(time.Now()), ID: id})
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) DeleteProfile(ctx context.Context, id int64) error {
	rows, err := s.q.DeleteProfile(ctx, id)
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("profile not found or active")
	}
	return nil
}

type JobInput struct {
	Name           string
	UserName       string
	DocumentFormat string
	Copies         int64
	Bytes          int64
}

func (s *Store) CreateJob(ctx context.Context, input JobInput) (*Job, error) {
	if input.Name == "" {
		input.Name = "Untitled"
	}
	if input.UserName == "" {
		input.UserName = "unknown"
	}
	if input.Copies < 1 {
		input.Copies = 1
	}
	return s.q.CreateJob(ctx, sqlitedb.CreateJobParams{
		Name: input.Name, UserName: input.UserName, DocumentFormat: input.DocumentFormat,
		Copies: input.Copies, Bytes: input.Bytes, CreatedAt: unixMillis(time.Now()),
	})
}

func (s *Store) Job(ctx context.Context, id int64) (*Job, error) {
	return s.q.GetJob(ctx, id)
}

func (s *Store) Jobs(ctx context.Context, limit int64) ([]*Job, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	return s.q.ListJobs(ctx, limit)
}

func (s *Store) PendingJobs(ctx context.Context) ([]*Job, error) {
	return s.q.ListJobsByState(ctx, "pending")
}

func (s *Store) SetJobBytes(ctx context.Context, id, bytes int64) error {
	return oneRow(s.q.SetJobBytes(ctx, sqlitedb.SetJobBytesParams{Bytes: bytes, ID: id}))
}

func (s *Store) StartJob(ctx context.Context, id int64) error {
	return oneRow(s.q.StartJob(ctx, sqlitedb.StartJobParams{StartedAt: nullableTime(time.Now()), ID: id}))
}

func (s *Store) CompleteJob(ctx context.Context, id int64) error {
	return oneRow(s.q.CompleteJob(ctx, sqlitedb.CompleteJobParams{CompletedAt: nullableTime(time.Now()), ID: id}))
}

func (s *Store) CancelJob(ctx context.Context, id int64) error {
	return oneRow(s.q.CancelJob(ctx, sqlitedb.CancelJobParams{CompletedAt: nullableTime(time.Now()), ID: id}))
}

func (s *Store) AbortJob(ctx context.Context, id int64, cause error) error {
	message := "unknown print error"
	if cause != nil {
		message = cause.Error()
	}
	return oneRow(s.q.AbortJob(ctx, sqlitedb.AbortJobParams{
		CompletedAt: nullableTime(time.Now()), Error: nullableString(message), ID: id,
	}))
}

func oneRow(rows int64, err error) error {
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func unixMillis(t time.Time) int64 { return t.UTC().UnixMilli() }

func nullableTime(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: unixMillis(t), Valid: true}
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func randomUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}
