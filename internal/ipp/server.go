// Package ipp exposes the BLE printer through IPP Everywhere operations.
package ipp

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenPrinting/goipp"

	"github.com/imbytecat/puqu-aq20-ipp/internal/printer"
	"github.com/imbytecat/puqu-aq20-ipp/internal/raster"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

const (
	maxRequestBytes = 16 << 20
	queueCapacity   = 32
)

const printerIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><rect width="128" height="128" rx="24" fill="#111827"/><rect x="28" y="18" width="72" height="36" rx="6" fill="#8ca7ff"/><rect x="18" y="44" width="92" height="54" rx="12" fill="#dce3ee"/><rect x="32" y="82" width="64" height="30" rx="5" fill="#fff"/><path d="M42 92h44M42 101h31" stroke="#111827" stroke-width="5"/><circle cx="91" cy="62" r="5" fill="#42d69b"/></svg>`

var supportedOperations = []goipp.Op{
	goipp.OpPrintJob,
	goipp.OpValidateJob,
	goipp.OpCreateJob,
	goipp.OpSendDocument,
	goipp.OpCancelJob,
	goipp.OpGetJobAttributes,
	goipp.OpGetJobs,
	goipp.OpGetPrinterAttributes,
	goipp.OpCancelMyJobs,
	goipp.OpCloseJob,
	goipp.OpValidateDocument,
	goipp.OpIdentifyPrinter,
}

type Printer interface {
	Print(context.Context, []printer.Job, printer.Settings) (printer.Result, error)
	Cancel() error
	Status() printer.Status
}

type Server struct {
	store   *store.Store
	printer Printer
	logger  *slog.Logger
	started time.Time
	queue   chan queuedJob

	mu       sync.Mutex
	current  int64
	cancel   context.CancelFunc
	openJobs map[int64]*queuedJob

	discovery *discovery
}

type queuedJob struct {
	id       int64
	document []byte
	format   string
	copies   int
	profile  raster.Profile
	settings printer.Settings
}

func New(st *store.Store, device Printer, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		store: st, printer: device, logger: logger, started: time.Now(),
		queue: make(chan queuedJob, queueCapacity), openJobs: map[int64]*queuedJob{},
		discovery: newDiscovery(st, logger),
	}
}

func (s *Server) Start(ctx context.Context) {
	go s.worker(ctx)
	go s.discovery.Run(ctx)
}

func (s *Server) ReloadDiscovery() { s.discovery.Reload() }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ipp/print", s.handle)
	mux.HandleFunc("GET /icon.svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = io.WriteString(w, printerIcon)
	})
	return mux
}

func (s *Server) QueueDepth() int { return len(s.queue) }

func (s *Server) Cancel(ctx context.Context, id int64) error {
	s.mu.Lock()
	if open := s.openJobs[id]; open != nil {
		delete(s.openJobs, id)
	}
	cancel := s.cancel
	current := s.current
	s.mu.Unlock()
	if err := s.store.CancelJob(ctx, id); err != nil {
		return err
	}
	if current == id {
		if cancel != nil {
			cancel()
		}
		_ = s.printer.Cancel()
	}
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); contentType != goipp.ContentType {
		http.Error(w, "Content-Type must be application/ipp", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes+64*1024)
	var request goipp.Message
	if err := request.Decode(r.Body); err != nil {
		http.Error(w, "invalid IPP request", http.StatusBadRequest)
		return
	}
	if request.Version < goipp.MakeVersion(1, 1) || request.Version > goipp.DefaultVersion {
		s.writeResponse(w, response(&request, goipp.StatusErrorVersionNotSupported, "IPP version not supported"))
		return
	}
	if status, message := validateRequest(&request); status != goipp.StatusOk {
		s.writeResponse(w, response(&request, status, message))
		return
	}

	var reply *goipp.Message
	switch goipp.Op(request.Code) {
	case goipp.OpGetPrinterAttributes:
		reply = s.getPrinterAttributes(r.Context(), &request, r.Host)
	case goipp.OpValidateJob, goipp.OpValidateDocument:
		reply = s.validateJob(r.Context(), &request)
	case goipp.OpPrintJob:
		reply = s.printJob(r.Context(), &request, r.Body, r.Host)
	case goipp.OpCreateJob:
		reply = s.createJob(r.Context(), &request, r.Host)
	case goipp.OpSendDocument:
		reply = s.sendDocument(r.Context(), &request, r.Body, r.Host)
	case goipp.OpCloseJob:
		reply = s.closeJob(r.Context(), &request, r.Host)
	case goipp.OpGetJobAttributes:
		reply = s.getJobAttributes(r.Context(), &request, r.Host)
	case goipp.OpGetJobs:
		reply = s.getJobs(r.Context(), &request, r.Host)
	case goipp.OpCancelJob:
		reply = s.cancelJob(r.Context(), &request)
	case goipp.OpCancelMyJobs:
		reply = s.cancelMyJobs(r.Context(), &request)
	case goipp.OpIdentifyPrinter:
		reply = s.identifyPrinter(r.Context(), &request)
	default:
		reply = response(&request, goipp.StatusErrorOperationNotSupported, "operation not supported")
	}
	s.writeResponse(w, reply)
}

func (s *Server) writeResponse(w http.ResponseWriter, reply *goipp.Message) {
	w.Header().Set("Content-Type", goipp.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := reply.Encode(w); err != nil {
		s.logger.Error("encode IPP response", "error", err)
	}
}

func response(request *goipp.Message, status goipp.Status, message string) *goipp.Message {
	version := request.Version
	if version < goipp.MakeVersion(1, 1) || version > goipp.DefaultVersion {
		version = goipp.DefaultVersion
	}
	reply := goipp.NewResponse(version, status, request.RequestID)
	reply.Operation.Add(goipp.MakeAttr("attributes-charset", goipp.TagCharset, goipp.String("utf-8")))
	reply.Operation.Add(goipp.MakeAttr("attributes-natural-language", goipp.TagLanguage, goipp.String("en-us")))
	if message != "" {
		reply.Operation.Add(goipp.MakeAttr("status-message", goipp.TagText, goipp.String(message)))
	}
	return reply
}
func validateRequest(request *goipp.Message) (goipp.Status, string) {
	if request.RequestID == 0 {
		return goipp.StatusErrorBadRequest, "request-id must be non-zero"
	}
	if len(request.Operation) < 2 ||
		request.Operation[0].Name != "attributes-charset" ||
		len(request.Operation[0].Values) != 1 ||
		request.Operation[0].Values[0].T != goipp.TagCharset ||
		request.Operation[1].Name != "attributes-natural-language" ||
		len(request.Operation[1].Values) != 1 ||
		request.Operation[1].Values[0].T != goipp.TagLanguage {
		return goipp.StatusErrorBadRequest, "attributes-charset and attributes-natural-language must be first"
	}
	if stringAttr(request, "attributes-charset", "") != "utf-8" {
		return goipp.StatusErrorCharset, "only utf-8 is supported"
	}
	operation := goipp.Op(request.Code)
	switch operation {
	case goipp.OpPrintJob, goipp.OpValidateJob, goipp.OpCreateJob, goipp.OpGetJobs,
		goipp.OpGetPrinterAttributes, goipp.OpCancelMyJobs, goipp.OpIdentifyPrinter:
		if !hasAttrTag(request, "printer-uri", goipp.TagURI) {
			return goipp.StatusErrorBadRequest, "printer-uri is required"
		}
	case goipp.OpSendDocument, goipp.OpCloseJob, goipp.OpCancelJob, goipp.OpGetJobAttributes, goipp.OpValidateDocument:
		if _, ok := jobID(request); !ok {
			return goipp.StatusErrorBadRequest, "job-uri or printer-uri and job-id are required"
		}
	}
	return goipp.StatusOk, ""
}

func (s *Server) identifyPrinter(_ context.Context, request *goipp.Message) *goipp.Message {
	s.logger.Info("IPP printer identification requested", "message", stringAttr(request, "message", "Identify PUQU AQ20"))
	return response(request, goipp.StatusOk, "")
}

func (s *Server) validateJob(ctx context.Context, request *goipp.Message) *goipp.Message {
	_, _, _, status, message := s.jobTemplate(ctx, request)
	return response(request, status, message)
}

func (s *Server) printJob(ctx context.Context, request *goipp.Message, document io.Reader, host string) *goipp.Message {
	format, copies, profile, status, message := s.jobTemplate(ctx, request)
	if status != goipp.StatusOk {
		return response(request, status, message)
	}
	data, err := io.ReadAll(io.LimitReader(document, maxRequestBytes+1))
	if err != nil {
		return response(request, goipp.StatusErrorDocumentFormatError, err.Error())
	}
	if len(data) == 0 || len(data) > maxRequestBytes {
		return response(request, goipp.StatusErrorRequestEntity, "document is empty or too large")
	}
	job, err := s.store.CreateJob(ctx, store.JobInput{
		Name: stringAttr(request, "job-name", "Untitled"), UserName: stringAttr(request, "requesting-user-name", "unknown"),
		DocumentFormat: format, Copies: int64(copies), Bytes: int64(len(data)),
	})
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	queued := queuedJob{id: job.ID, document: data, format: format, copies: copies, profile: profileToRaster(profile), settings: profileToSettings(profile)}
	if err := s.enqueue(ctx, queued); err != nil {
		_ = s.store.AbortJob(ctx, job.ID, err)
		return response(request, goipp.StatusErrorTooManyJobs, err.Error())
	}
	return s.jobResponse(request, job, host)
}

func (s *Server) createJob(ctx context.Context, request *goipp.Message, host string) *goipp.Message {
	format, copies, profile, status, message := s.jobTemplate(ctx, request)
	if status != goipp.StatusOk {
		return response(request, status, message)
	}
	job, err := s.store.CreateJob(ctx, store.JobInput{
		Name: stringAttr(request, "job-name", "Untitled"), UserName: stringAttr(request, "requesting-user-name", "unknown"),
		DocumentFormat: format, Copies: int64(copies), Bytes: 0,
	})
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	s.mu.Lock()
	s.openJobs[job.ID] = &queuedJob{id: job.ID, format: format, copies: copies, profile: profileToRaster(profile), settings: profileToSettings(profile)}
	s.mu.Unlock()
	return s.jobResponse(request, job, host)
}

func (s *Server) sendDocument(ctx context.Context, request *goipp.Message, document io.Reader, host string) *goipp.Message {
	id, ok := jobID(request)
	if !ok {
		return response(request, goipp.StatusErrorBadRequest, "job target is required")
	}
	data, err := io.ReadAll(io.LimitReader(document, maxRequestBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxRequestBytes {
		return response(request, goipp.StatusErrorRequestEntity, "document is empty or too large")
	}
	lastDocument := booleanAttr(request, "last-document", true)
	s.mu.Lock()
	queued := s.openJobs[int64(id)]
	if queued == nil {
		s.mu.Unlock()
		return response(request, goipp.StatusErrorNotFound, "open job not found")
	}
	if len(queued.document) != 0 {
		s.mu.Unlock()
		return response(request, goipp.StatusErrorTooManyDocuments, "only one document is supported")
	}
	format := stringAttr(request, "document-format", queued.format)
	if format != queued.format {
		s.mu.Unlock()
		return response(request, goipp.StatusErrorConflicting, "document format differs from Create-Job")
	}
	queued.document = data
	snapshot := *queued
	if lastDocument {
		delete(s.openJobs, queued.id)
	}
	s.mu.Unlock()
	if err := s.store.SetJobBytes(ctx, queued.id, int64(len(data))); err != nil {
		_ = s.store.AbortJob(ctx, queued.id, err)
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	if lastDocument {
		if err := s.enqueue(ctx, snapshot); err != nil {
			_ = s.store.AbortJob(ctx, queued.id, err)
			return response(request, goipp.StatusErrorTooManyJobs, err.Error())
		}
	}
	job, err := s.store.Job(ctx, queued.id)
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	return s.jobResponse(request, job, host)
}

func (s *Server) closeJob(ctx context.Context, request *goipp.Message, host string) *goipp.Message {
	id, ok := jobID(request)
	if !ok {
		return response(request, goipp.StatusErrorBadRequest, "job target is required")
	}
	s.mu.Lock()
	queued := s.openJobs[int64(id)]
	if queued != nil {
		delete(s.openJobs, int64(id))
	}
	s.mu.Unlock()
	if queued == nil || len(queued.document) == 0 {
		return response(request, goipp.StatusErrorNotPossible, "job has no document")
	}
	if err := s.enqueue(ctx, *queued); err != nil {
		_ = s.store.AbortJob(ctx, queued.id, err)
		return response(request, goipp.StatusErrorTooManyJobs, err.Error())
	}
	job, err := s.store.Job(ctx, queued.id)
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	return s.jobResponse(request, job, host)
}

func (s *Server) jobTemplate(ctx context.Context, request *goipp.Message) (string, int, *store.Profile, goipp.Status, string) {
	format := stringAttr(request, "document-format", raster.FormatPWG)
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return "", 0, nil, goipp.StatusErrorInternal, err.Error()
	}
	if format != raster.FormatPWG && format != raster.FormatJPEG && !(format == raster.FormatApple && settings.Airprint == 1) {
		return "", 0, nil, goipp.StatusErrorDocumentFormatNotSupported, "document format not supported"
	}
	copies := 1
	if value, ok := integerAttr(request, "copies"); ok {
		copies = value
	}
	if copies < 1 || copies > 999 {
		return "", 0, nil, goipp.StatusErrorAttributesOrValues, "copies must be between 1 and 999"
	}
	if sides := stringAttr(request, "sides", "one-sided"); sides != "one-sided" {
		return "", 0, nil, goipp.StatusErrorAttributesOrValues, "only one-sided printing is supported"
	}
	if color := stringAttr(request, "print-color-mode", "monochrome"); color != "monochrome" && color != "auto" {
		return "", 0, nil, goipp.StatusErrorAttributesOrValues, "only monochrome printing is supported"
	}
	profile, err := s.store.ActiveProfile(ctx)
	if err != nil {
		return "", 0, nil, goipp.StatusErrorNotAcceptingJobs, "no active label profile"
	}
	if media := stringAttr(request, "media", ""); media != "" && media != mediaName(profile) {
		return "", 0, nil, goipp.StatusErrorAttributesOrValues, "requested media is not loaded"
	}
	return format, copies, profile, goipp.StatusOk, ""
}

func (s *Server) enqueue(ctx context.Context, job queuedJob) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.queue <- job:
		return nil
	default:
		return errors.New("print queue is full")
	}
}

func (s *Server) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case queued := <-s.queue:
			job, err := s.store.Job(ctx, queued.id)
			if err != nil || job.State != "pending" {
				continue
			}
			err = s.process(ctx, queued)
			if err == nil {
				continue
			}
			current, lookupErr := s.store.Job(context.Background(), queued.id)
			if lookupErr == nil && current.State != "canceled" {
				_ = s.store.AbortJob(context.Background(), queued.id, err)
			}
			s.logger.Error("print job failed", "job", queued.id, "error", err)
		}
	}
}

func (s *Server) process(ctx context.Context, queued queuedJob) error {
	jobCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.current = queued.id
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.current = 0
		s.cancel = nil
		s.mu.Unlock()
	}()

	if err := s.waitForPrinter(jobCtx); err != nil {
		return err
	}
	if err := s.store.StartJob(ctx, queued.id); err != nil {
		return err
	}
	bitmaps, err := raster.Decode(bytes.NewReader(queued.document), queued.format, queued.profile)
	if err != nil {
		return err
	}
	for i := range bitmaps {
		bitmaps[i].Copies = queued.copies
	}
	if _, err := s.printer.Print(jobCtx, bitmaps, queued.settings); err != nil {
		return err
	}
	return s.store.CompleteJob(ctx, queued.id)
}

func (s *Server) waitForPrinter(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if s.printer.Status().Connected {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) getJobAttributes(ctx context.Context, request *goipp.Message, host string) *goipp.Message {
	id, ok := jobID(request)
	if !ok {
		return response(request, goipp.StatusErrorBadRequest, "job target is required")
	}
	job, err := s.store.Job(ctx, int64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return response(request, goipp.StatusErrorNotFound, "job not found")
	}
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	reply := response(request, goipp.StatusOk, "")
	reply.Job = filterJobAttributes(request, jobAttributes(job, printerURI(host), s.started), false)
	return reply
}

func (s *Server) getJobs(ctx context.Context, request *goipp.Message, host string) *goipp.Message {
	jobs, err := s.store.Jobs(ctx, 500)
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	which := stringAttr(request, "which-jobs", "not-completed")
	limit, hasLimit := integerAttr(request, "limit")
	if !hasLimit || limit < 1 {
		limit = 500
	}
	myJobs := booleanAttr(request, "my-jobs", false)
	user := stringAttr(request, "requesting-user-name", "")
	reply := response(request, goipp.StatusOk, "")
	reply.Groups = goipp.Groups{{Tag: goipp.TagOperationGroup, Attrs: reply.Operation}}
	count := 0
	for i := len(jobs) - 1; i >= 0 && count < limit; i-- {
		job := jobs[i]
		if !jobMatches(which, job.State) || myJobs && job.UserName != user {
			continue
		}
		attrs := filterJobAttributes(request, jobAttributes(job, printerURI(host), s.started), true)
		reply.Groups.Add(goipp.Group{Tag: goipp.TagJobGroup, Attrs: attrs})
		count++
	}
	return reply
}

func (s *Server) cancelJob(ctx context.Context, request *goipp.Message) *goipp.Message {
	id, ok := jobID(request)
	if !ok {
		return response(request, goipp.StatusErrorBadRequest, "job target is required")
	}
	if err := s.Cancel(ctx, int64(id)); errors.Is(err, sql.ErrNoRows) {
		return response(request, goipp.StatusErrorNotPossible, "job cannot be canceled")
	} else if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	return response(request, goipp.StatusOk, "")
}

func (s *Server) cancelMyJobs(ctx context.Context, request *goipp.Message) *goipp.Message {
	user := stringAttr(request, "requesting-user-name", "")
	if user == "" {
		return response(request, goipp.StatusErrorBadRequest, "requesting-user-name is required")
	}
	jobs, err := s.store.Jobs(ctx, 500)
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	for _, job := range jobs {
		if job.UserName == user && (job.State == "pending" || job.State == "processing") {
			_ = s.Cancel(ctx, job.ID)
		}
	}
	return response(request, goipp.StatusOk, "")
}

func (s *Server) jobResponse(request *goipp.Message, job *store.Job, host string) *goipp.Message {
	reply := response(request, goipp.StatusOk, "")
	reply.Job = jobAttributes(job, printerURI(host), s.started)
	return reply
}

func jobAttributes(job *store.Job, uri string, started time.Time) goipp.Attributes {
	state, reason := ippJobState(job.State)
	attrs := goipp.Attributes{}
	attrs.Add(goipp.MakeAttr("job-id", goipp.TagInteger, goipp.Integer(job.ID)))
	attrs.Add(goipp.MakeAttr("job-uri", goipp.TagURI, goipp.String(uri+"/jobs/"+strconv.FormatInt(job.ID, 10))))
	attrs.Add(goipp.MakeAttr("job-printer-uri", goipp.TagURI, goipp.String(uri)))
	attrs.Add(goipp.MakeAttr("job-name", goipp.TagName, goipp.String(job.Name)))
	attrs.Add(goipp.MakeAttr("job-originating-user-name", goipp.TagName, goipp.String(job.UserName)))
	attrs.Add(goipp.MakeAttr("job-state", goipp.TagEnum, goipp.Integer(state)))
	attrs.Add(goipp.MakeAttr("job-state-reasons", goipp.TagKeyword, goipp.String(reason)))
	attrs.Add(goipp.MakeAttr("job-state-message", goipp.TagText, goipp.String(nullString(job.Error))))
	attrs.Add(goipp.MakeAttr("job-k-octets", goipp.TagInteger, goipp.Integer((job.Bytes+1023)/1024)))
	attrs.Add(goipp.MakeAttr("copies", goipp.TagInteger, goipp.Integer(job.Copies)))
	attrs.Add(goipp.MakeAttr("document-format", goipp.TagMimeType, goipp.String(job.DocumentFormat)))
	attrs.Add(goipp.MakeAttr("number-of-intervening-jobs", goipp.TagInteger, goipp.Integer(0)))
	attrs.Add(goipp.MakeAttr("job-printer-up-time", goipp.TagInteger, goipp.Integer(time.Since(started)/time.Second)))
	attrs.Add(goipp.MakeAttr("time-at-creation", goipp.TagInteger, goipp.Integer(jobTime(started, job.CreatedAt))))
	if job.StartedAt.Valid {
		attrs.Add(goipp.MakeAttr("time-at-processing", goipp.TagInteger, goipp.Integer(jobTime(started, job.StartedAt.Int64))))
	}
	if job.CompletedAt.Valid {
		attrs.Add(goipp.MakeAttr("time-at-completed", goipp.TagInteger, goipp.Integer(jobTime(started, job.CompletedAt.Int64))))
	}
	return attrs
}

func jobTime(started time.Time, timestamp int64) int64 {
	return max(0, timestamp/1000-started.Unix())
}

func jobMatches(which, state string) bool {
	switch which {
	case "all":
		return true
	case "completed":
		return state == "completed" || state == "canceled" || state == "aborted"
	case "not-completed":
		return state == "pending" || state == "processing"
	default:
		return state == which
	}
}
func filterJobAttributes(request *goipp.Message, attrs goipp.Attributes, getJobs bool) goipp.Attributes {
	requested := stringAttrs(request, "requested-attributes")
	if len(requested) == 0 {
		if !getJobs {
			return attrs
		}
		requested = []string{"job-id", "job-uri"}
	}
	exact := make(map[string]bool, len(requested))
	all := false
	for _, name := range requested {
		switch name {
		case "none":
			return nil
		case "all":
			all = true
		default:
			exact[name] = true
		}
	}
	if all {
		return attrs
	}
	filtered := make(goipp.Attributes, 0, len(exact))
	for _, attribute := range attrs {
		if exact[attribute.Name] {
			filtered = append(filtered, attribute)
		}
	}
	return filtered
}

func ippJobState(state string) (int32, string) {
	switch state {
	case "pending":
		return 3, "none"
	case "processing":
		return 5, "job-printing"
	case "canceled":
		return 7, "job-canceled-by-user"
	case "aborted":
		return 8, "aborted-by-system"
	case "completed":
		return 9, "job-completed-successfully"
	default:
		return 8, "aborted-by-system"
	}
}

func profileToRaster(profile *store.Profile) raster.Profile {
	return raster.Profile{WidthUM: profile.WidthUm, HeightUM: profile.HeightUm}
}

func profileToSettings(profile *store.Profile) printer.Settings {
	return printer.Settings{Darkness: int(profile.Darkness), Speed: int(profile.Speed), PaperType: int(profile.PaperType)}
}

func printerURI(host string) string {
	if host == "" {
		host = "localhost:8631"
	}
	return "ipp://" + host + "/ipp/print"
}
func httpURI(host, path string) string {
	if host == "" {
		host = "localhost:8631"
	}
	return "http://" + host + path
}

func attr(request *goipp.Message, name string) (goipp.Value, bool) {
	groups := []goipp.Attributes{request.Operation, request.Job, request.Document}
	for _, attrs := range groups {
		for _, item := range attrs {
			if item.Name == name && len(item.Values) > 0 {
				return item.Values[0].V, true
			}
		}
	}
	return nil, false
}

func stringAttr(request *goipp.Message, name, fallback string) string {
	value, ok := attr(request, name)
	if !ok {
		return fallback
	}
	if text, ok := value.(goipp.String); ok {
		return string(text)
	}
	return fallback
}
func stringAttrs(request *goipp.Message, name string) []string {
	groups := []goipp.Attributes{request.Operation, request.Job, request.Document}
	var values []string
	for _, attrs := range groups {
		for _, item := range attrs {
			if item.Name != name {
				continue
			}
			for _, value := range item.Values {
				if text, ok := value.V.(goipp.String); ok {
					values = append(values, string(text))
				}
			}
		}
	}
	return values
}

func integerAttr(request *goipp.Message, name string) (int, bool) {
	value, ok := attr(request, name)
	if !ok {
		return 0, false
	}
	integer, ok := value.(goipp.Integer)
	return int(integer), ok
}

func hasAttrTag(request *goipp.Message, name string, tag goipp.Tag) bool {
	groups := []goipp.Attributes{request.Operation, request.Job, request.Document}
	for _, attrs := range groups {
		for _, item := range attrs {
			if item.Name == name && len(item.Values) > 0 && item.Values[0].T == tag {
				return true
			}
		}
	}
	return false
}

func jobID(request *goipp.Message) (int, bool) {
	if id, ok := integerAttr(request, "job-id"); ok && id > 0 {
		return id, true
	}
	value, ok := attr(request, "job-uri")
	if !ok {
		return 0, false
	}
	uri, ok := value.(goipp.String)
	if !ok {
		return 0, false
	}
	path := string(uri)
	index := strings.LastIndex(path, "/jobs/")
	if index < 0 {
		return 0, false
	}
	id, err := strconv.Atoi(path[index+len("/jobs/"):])
	return id, err == nil && id > 0
}

func booleanAttr(request *goipp.Message, name string, fallback bool) bool {
	value, ok := attr(request, name)
	if !ok {
		return fallback
	}
	boolean, ok := value.(goipp.Boolean)
	if !ok {
		return fallback
	}
	return bool(boolean)
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func mediaName(profile *store.Profile) string {
	return fmt.Sprintf("custom_label_%gx%gmm", float64(profile.WidthUm)/1000, float64(profile.HeightUm)/1000)
}
