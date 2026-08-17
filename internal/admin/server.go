// Package admin serves the local React configuration interface and its JSON endpoints.
package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/imbytecat/puqu-ipp-bridge/internal/config"
	"github.com/imbytecat/puqu-ipp-bridge/internal/fleet"
	"github.com/imbytecat/puqu-ipp-bridge/internal/ipp"
	"github.com/imbytecat/puqu-ipp-bridge/internal/printer"
	"github.com/imbytecat/puqu-ipp-bridge/internal/store"
)

type Server struct {
	store   *store.Store
	fleet   *fleet.Fleet
	ipp     *ipp.Gateway
	config  config.Config
	ui      http.Handler
	version string
}

func New(st *store.Store, printerFleet *fleet.Fleet, ippGateway *ipp.Gateway, runtimeConfig config.Config, ui http.Handler, version string) *Server {
	return &Server{store: st, fleet: printerFleet, ipp: ippGateway, config: runtimeConfig, ui: ui, version: version}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/usb/scan", s.scanUSB)
	mux.HandleFunc("POST /api/devices", s.saveDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", s.deleteDevice)
	mux.HandleFunc("POST /api/printers", s.createPrinter)
	mux.HandleFunc("PUT /api/printers/{id}", s.updatePrinter)
	mux.HandleFunc("DELETE /api/printers/{id}", s.deletePrinter)
	mux.HandleFunc("POST /api/printers/{id}/connect", s.connect)
	mux.HandleFunc("POST /api/printers/{id}/test", s.testPrint)
	mux.HandleFunc("GET /api/profiles", s.profiles)
	mux.HandleFunc("POST /api/profiles", s.createProfile)
	mux.HandleFunc("PUT /api/profiles/{id}", s.updateProfile)
	mux.HandleFunc("DELETE /api/profiles/{id}", s.deleteProfile)
	mux.HandleFunc("GET /api/jobs", s.jobs)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { writeError(w, http.StatusNotFound, "endpoint not found") })
	mux.Handle("/", s.ui)
	return securityHeaders(mux)
}

type statusResponse struct {
	Version  string         `json:"version"`
	Config   config.Config  `json:"config"`
	Drivers  []fleet.Driver `json:"drivers"`
	Printers []printerDTO   `json:"printers"`
	Devices  []deviceDTO    `json:"devices"`
	Profiles []profileDTO   `json:"profiles"`
	Jobs     []jobDTO       `json:"jobs"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	printers, devices, profiles, jobs, err := s.snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Version: s.version, Config: s.config, Drivers: fleet.Drivers(),
		Printers: s.mapPrinters(printers), Devices: mapDevices(devices, printers),
		Profiles: mapProfiles(profiles), Jobs: mapJobs(jobs),
	})
}

func (s *Server) snapshot(ctx context.Context) ([]*store.Printer, []*store.Device, []*store.Profile, []*store.Job, error) {
	printers, err := s.store.Printers(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	devices, err := s.store.Devices(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	profiles, err := s.store.Profiles(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	jobs, err := s.store.Jobs(ctx, 100)
	return printers, devices, profiles, jobs, err
}

func (s *Server) scanUSB(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	devices, err := s.fleet.ScanUSB(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

type deviceInput struct {
	NativeID string `json:"nativeId"`
	Name     string `json:"name"`
	Address  string `json:"address"`
}

func (s *Server) saveDevice(w http.ResponseWriter, r *http.Request) {
	var input deviceInput
	if !decodeJSON(w, r, &input) {
		return
	}
	device, err := s.store.SaveDevice(r.Context(), store.DeviceInput{
		NativeID: input.NativeID, Name: input.Name, Address: input.Address,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.fleet.Reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDevice(device, nil))
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteDevice(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type printerInput struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Driver    string `json:"driver"`
	DeviceID  *int64 `json:"deviceId"`
	ProfileID int64  `json:"profileId"`
	Enabled   bool   `json:"enabled"`
}

func (input printerInput) storeInput() store.PrinterInput {
	deviceID := int64(0)
	if input.DeviceID != nil {
		deviceID = *input.DeviceID
	}
	return store.PrinterInput{
		Name: input.Name, Slug: input.Slug, Driver: input.Driver, DeviceID: deviceID,
		ProfileID: input.ProfileID, Enabled: input.Enabled,
	}
}

func (s *Server) createPrinter(w http.ResponseWriter, r *http.Request) {
	var input printerInput
	if !decodeJSON(w, r, &input) {
		return
	}
	configured, err := s.store.CreatePrinter(r.Context(), input.storeInput())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.toPrinter(configured))
}

func (s *Server) updatePrinter(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input printerInput
	if !decodeJSON(w, r, &input) {
		return
	}
	configured, err := s.store.UpdatePrinter(r.Context(), id, input.storeInput())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.toPrinter(configured))
}

func (s *Server) deletePrinter(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeletePrinter(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.fleet.Reconnect(id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) testPrint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	configured, err := s.store.Printer(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	profile, err := s.store.Profile(r.Context(), configured.ProfileID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	job, err := testPattern(profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.fleet.Print(ctx, id, []printer.Job{job})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.Profiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapProfiles(profiles))
}

type profileInput struct {
	Name           string  `json:"name"`
	WidthMM        float64 `json:"widthMm"`
	HeightMM       float64 `json:"heightMm"`
	GapMM          float64 `json:"gapMm"`
	HalftoneMethod int64   `json:"halftoneMethod"`
	Brightness     int64   `json:"brightness"`
}

func (input profileInput) storeInput() store.ProfileInput {
	return store.ProfileInput{
		Name: input.Name, WidthUM: int64(input.WidthMM * 1000), HeightUM: int64(input.HeightMM * 1000),
		GapUM: int64(input.GapMM * 1000), HalftoneMethod: input.HalftoneMethod, Brightness: input.Brightness,
	}
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var input profileInput
	if !decodeJSON(w, r, &input) {
		return
	}
	profile, err := s.store.CreateProfile(r.Context(), input.storeInput())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toProfile(profile))
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input profileInput
	if !decodeJSON(w, r, &input) {
		return
	}
	profile, err := s.store.UpdateProfile(r.Context(), id, input.storeInput())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProfile(profile))
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteProfile(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.Jobs(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapJobs(jobs))
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.ipp.Cancel(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) reload(ctx context.Context) error {
	if err := s.fleet.Reload(ctx); err != nil {
		return err
	}
	return s.ipp.Reload(ctx)
}

func testPattern(profile *store.Profile) (printer.Job, error) {
	width := int((profile.WidthUm*8 + 500) / 1000)
	height := int((profile.HeightUm*8 + 500) / 1000)
	if width < 16 || width > 2040 || height < 16 || height > 65535 {
		return printer.Job{}, errors.New("label dimensions exceed printer limits")
	}
	widthBytes := (width + 7) / 8
	data := make([]byte, widthBytes*height)
	set := func(x, y int) { data[y*widthBytes+x/8] |= 0x80 >> (x % 8) }
	for y := range height {
		for x := range width {
			if x < 2 || x >= width-2 || y < 2 || y >= height-2 || x == y%width || x == width-1-y%width || (y/8)%2 == 0 && x < width/4 {
				set(x, y)
			}
		}
	}
	return printer.Job{WidthBytes: widthBytes, HeightPx: height, Data: data, Copies: 1}, nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type printerDTO struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	Slug       string         `json:"slug"`
	UUID       string         `json:"uuid"`
	Driver     string         `json:"driver"`
	DeviceID   *int64         `json:"deviceId"`
	ProfileID  int64          `json:"profileId"`
	Enabled    bool           `json:"enabled"`
	Status     printer.Status `json:"status"`
	QueueDepth int            `json:"queueDepth"`
	UpdatedAt  int64          `json:"updatedAt"`
}

type deviceDTO struct {
	ID                int64  `json:"id"`
	NativeID          string `json:"nativeId"`
	Name              string `json:"name"`
	Address           string `json:"address"`
	AssignedPrinterID *int64 `json:"assignedPrinterId"`
	LastSeenAt        int64  `json:"lastSeenAt"`
}

type profileDTO struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	WidthMM        float64 `json:"widthMm"`
	HeightMM       float64 `json:"heightMm"`
	GapMM          float64 `json:"gapMm"`
	HalftoneMethod int64   `json:"halftoneMethod"`
	Brightness     int64   `json:"brightness"`
}

type jobDTO struct {
	ID             int64   `json:"id"`
	PrinterID      int64   `json:"printerId"`
	Name           string  `json:"name"`
	UserName       string  `json:"userName"`
	State          string  `json:"state"`
	DocumentFormat string  `json:"documentFormat"`
	Copies         int64   `json:"copies"`
	Bytes          int64   `json:"bytes"`
	Error          *string `json:"error"`
	CreatedAt      int64   `json:"createdAt"`
	StartedAt      *int64  `json:"startedAt"`
	CompletedAt    *int64  `json:"completedAt"`
}

func (s *Server) mapPrinters(values []*store.Printer) []printerDTO {
	out := make([]printerDTO, len(values))
	for i, value := range values {
		out[i] = s.toPrinter(value)
	}
	return out
}

func (s *Server) toPrinter(value *store.Printer) printerDTO {
	return printerDTO{
		ID: value.ID, Name: value.Name, Slug: value.Slug, UUID: value.Uuid, Driver: value.Driver,
		DeviceID: nullInt64(value.DeviceID), ProfileID: value.ProfileID, Enabled: value.Enabled == 1,
		Status: s.fleet.Status(value.ID), QueueDepth: s.ipp.QueueDepth(value.ID), UpdatedAt: value.UpdatedAt,
	}
}

func mapDevices(values []*store.Device, printers []*store.Printer) []deviceDTO {
	assigned := make(map[int64]int64, len(printers))
	for _, configured := range printers {
		if configured.DeviceID.Valid {
			assigned[configured.DeviceID.Int64] = configured.ID
		}
	}
	out := make([]deviceDTO, len(values))
	for i, value := range values {
		var printerID *int64
		if id, ok := assigned[value.ID]; ok {
			printerID = &id
		}
		out[i] = toDevice(value, printerID)
	}
	return out
}

func toDevice(value *store.Device, assignedPrinterID *int64) deviceDTO {
	return deviceDTO{
		ID: value.ID, NativeID: value.NativeID, Name: value.Name, Address: value.Address,
		AssignedPrinterID: assignedPrinterID, LastSeenAt: value.LastSeenAt,
	}
}

func mapProfiles(values []*store.Profile) []profileDTO {
	out := make([]profileDTO, len(values))
	for i, value := range values {
		out[i] = toProfile(value)
	}
	return out
}

func toProfile(value *store.Profile) profileDTO {
	return profileDTO{
		ID: value.ID, Name: value.Name, WidthMM: float64(value.WidthUm) / 1000,
		HeightMM: float64(value.HeightUm) / 1000, GapMM: float64(value.GapUm) / 1000,
		HalftoneMethod: value.HalftoneMethod, Brightness: value.Brightness,
	}
}

func mapJobs(values []*store.Job) []jobDTO {
	out := make([]jobDTO, len(values))
	for i, value := range values {
		out[i] = jobDTO{
			ID: value.ID, PrinterID: value.PrinterID, Name: value.Name, UserName: value.UserName,
			State: value.State, DocumentFormat: value.DocumentFormat, Copies: value.Copies, Bytes: value.Bytes,
			Error: nullString(value.Error), CreatedAt: value.CreatedAt,
			StartedAt: nullInt64(value.StartedAt), CompletedAt: nullInt64(value.CompletedAt),
		}
	}
	return out
}

func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
