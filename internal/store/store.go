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

	"github.com/imbytecat/puqu-ipp-bridge/internal/store/sqlitedb"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Device = sqlitedb.Device
type Profile = sqlitedb.LabelProfile
type Printer = sqlitedb.Printer
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
	return filepath.Join(dir, "puqu-ipp", "puqu.db"), nil
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

const TransportUSB = "usb"

func (s *Store) Devices(ctx context.Context) ([]*Device, error) {
	return s.q.ListDevices(ctx)
}

type DeviceInput struct {
	NativeID string
	Name     string
	Address  string
}

func (s *Store) SaveDevice(ctx context.Context, input DeviceInput) (*Device, error) {
	input.NativeID = strings.TrimSpace(input.NativeID)
	input.Name = strings.TrimSpace(input.Name)
	input.Address = strings.TrimSpace(input.Address)
	if input.NativeID == "" || input.Name == "" || input.Address == "" {
		return nil, errors.New("device id, name, and address are required")
	}
	now := unixMillis(time.Now())
	return s.q.UpsertDevice(ctx, sqlitedb.UpsertDeviceParams{
		NativeID: input.NativeID, Name: input.Name, Address: input.Address,
		LastSeenAt: now, UpdatedAt: now,
	})
}

func (s *Store) Device(ctx context.Context, id int64) (*Device, error) {
	return s.q.GetDevice(ctx, id)
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

func (s *Store) Profile(ctx context.Context, id int64) (*Profile, error) {
	return s.q.GetProfile(ctx, id)
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

func (s *Store) DeleteProfile(ctx context.Context, id int64) error {
	rows, err := s.q.DeleteProfile(ctx, id)
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

const DriverPUQUAQ20 = "puqu-aq20"

type PrinterInput struct {
	Name      string
	Slug      string
	Driver    string
	DeviceID  int64
	ProfileID int64
	Enabled   bool
}

func (s *Store) Printers(ctx context.Context) ([]*Printer, error) {
	return s.q.ListPrinters(ctx)
}

func (s *Store) Printer(ctx context.Context, id int64) (*Printer, error) {
	return s.q.GetPrinter(ctx, id)
}

func (s *Store) PrinterBySlug(ctx context.Context, slug string) (*Printer, error) {
	return s.q.GetPrinterBySlug(ctx, slug)
}

func (s *Store) CreatePrinter(ctx context.Context, input PrinterInput) (*Printer, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = slugify(input.Slug)
	if input.Slug == "" {
		input.Slug = slugify(input.Name)
	}
	if input.Driver == "" {
		input.Driver = DriverPUQUAQ20
	}
	if err := validatePrinter(input, true); err != nil {
		return nil, err
	}
	uuid, err := randomUUID()
	if err != nil {
		return nil, err
	}
	now := unixMillis(time.Now())
	return s.q.CreatePrinter(ctx, sqlitedb.CreatePrinterParams{
		Name: input.Name, Slug: input.Slug, Uuid: uuid, Driver: input.Driver,
		DeviceID: nullableID(input.DeviceID), ProfileID: input.ProfileID, Enabled: boolInt(input.Enabled),
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Store) UpdatePrinter(ctx context.Context, id int64, input PrinterInput) (*Printer, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Driver == "" {
		input.Driver = DriverPUQUAQ20
	}
	if err := validatePrinter(input, false); err != nil {
		return nil, err
	}
	return s.q.UpdatePrinter(ctx, sqlitedb.UpdatePrinterParams{
		Name: input.Name, Driver: input.Driver, DeviceID: nullableID(input.DeviceID), ProfileID: input.ProfileID,
		Enabled: boolInt(input.Enabled), UpdatedAt: unixMillis(time.Now()), ID: id,
	})
}

func validatePrinter(input PrinterInput, requireSlug bool) error {
	if input.Name == "" {
		return errors.New("printer name is required")
	}
	if requireSlug && input.Slug == "" {
		return errors.New("printer queue name must contain a letter or number")
	}
	if input.Driver != DriverPUQUAQ20 {
		return fmt.Errorf("unsupported printer driver %q", input.Driver)
	}
	if input.ProfileID < 1 {
		return errors.New("label profile is required")
	}
	return nil
}

func (s *Store) DeletePrinter(ctx context.Context, id int64) error {
	rows, err := s.q.DeletePrinter(ctx, id)
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

type JobInput struct {
	PrinterID      int64
	Name           string
	UserName       string
	DocumentFormat string
	Copies         int64
	Bytes          int64
}

func (s *Store) CreateJob(ctx context.Context, input JobInput) (*Job, error) {
	if input.PrinterID < 1 {
		return nil, errors.New("printer is required")
	}
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
		PrinterID: input.PrinterID, Name: input.Name, UserName: input.UserName,
		DocumentFormat: input.DocumentFormat, Copies: input.Copies, Bytes: input.Bytes,
		CreatedAt: unixMillis(time.Now()),
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

func (s *Store) JobsByPrinter(ctx context.Context, printerID, limit int64) ([]*Job, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	return s.q.ListJobsByPrinter(ctx, sqlitedb.ListJobsByPrinterParams{PrinterID: printerID, Limit: limit})
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

func nullableID(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: value > 0}
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
