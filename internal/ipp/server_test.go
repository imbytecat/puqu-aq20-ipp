package ipp

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/OpenPrinting/goipp"

	"github.com/imbytecat/puqu-aq20-ipp/internal/printer"
	"github.com/imbytecat/puqu-aq20-ipp/internal/raster"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

type fakePrinter struct {
	mu   sync.Mutex
	jobs []printer.Job
}

func (f *fakePrinter) Print(_ context.Context, jobs []printer.Job, _ printer.Settings) (printer.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, jobs...)
	return printer.Result{Jobs: len(jobs)}, nil
}
func (f *fakePrinter) Cancel() error          { return nil }
func (f *fakePrinter) Status() printer.Status { return printer.Status{Connected: true} }

func TestPrintJobEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	device := &fakePrinter{}
	srv := New(st, device, nil)
	go srv.worker(ctx)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	request := baseRequest(goipp.OpPrintJob, 7)
	request.Operation.Add(goipp.MakeAttr("printer-uri", goipp.TagURI, goipp.String("ipp://localhost/ipp/print")))
	request.Operation.Add(goipp.MakeAttr("requesting-user-name", goipp.TagName, goipp.String("tester")))
	request.Operation.Add(goipp.MakeAttr("job-name", goipp.TagName, goipp.String("fixture")))
	request.Operation.Add(goipp.MakeAttr("document-format", goipp.TagMimeType, goipp.String(raster.FormatPWG)))
	encoded, err := request.EncodeBytes()
	if err != nil {
		t.Fatal(err)
	}
	body := append(encoded, whitePWGRaster(320, 240)...)
	response := postIPP(t, httpServer.URL+"/ipp/print", body)
	if goipp.Status(response.Code) != goipp.StatusOk {
		t.Fatalf("status = %s", goipp.Status(response.Code))
	}
	id, ok := integerFrom(response.Job, "job-id")
	if !ok {
		t.Fatal("response missing job-id")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := st.Job(ctx, int64(id))
		if err == nil && job.State == "completed" {
			device.mu.Lock()
			defer device.mu.Unlock()
			if len(device.jobs) != 1 || device.jobs[0].WidthBytes != 40 || device.jobs[0].HeightPx != 240 {
				t.Fatalf("printed jobs = %+v", device.jobs)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not complete")
}

func TestGetPrinterAttributes(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(st, &fakePrinter{}, nil)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()
	request := baseRequest(goipp.OpGetPrinterAttributes, 9)
	request.Operation.Add(goipp.MakeAttr("printer-uri", goipp.TagURI, goipp.String("ipp://localhost/ipp/print")))
	encoded, err := request.EncodeBytes()
	if err != nil {
		t.Fatal(err)
	}
	response := postIPP(t, httpServer.URL+"/ipp/print", encoded)
	if goipp.Status(response.Code) != goipp.StatusOk {
		t.Fatalf("status = %s", goipp.Status(response.Code))
	}
	if value := stringFrom(response.Printer, "document-format-default"); value != raster.FormatPWG {
		t.Fatalf("document format = %q", value)
	}
	if !containsString(response.Printer, "document-format-supported", raster.FormatJPEG) {
		t.Fatal("JPEG format is not advertised")
	}
	if value, ok := integerFrom(response.Printer, "printer-state"); !ok || value != 3 {
		t.Fatalf("printer-state = %d, ok=%v", value, ok)
	}
}
func TestRejectsZeroRequestID(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(st, &fakePrinter{}, nil)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()
	request := baseRequest(goipp.OpGetPrinterAttributes, 0)
	request.Operation.Add(goipp.MakeAttr("printer-uri", goipp.TagURI, goipp.String("ipp://localhost/ipp/print")))
	encoded, err := request.EncodeBytes()
	if err != nil {
		t.Fatal(err)
	}
	response := postIPP(t, httpServer.URL+"/ipp/print", encoded)
	if goipp.Status(response.Code) != goipp.StatusErrorBadRequest {
		t.Fatalf("status = %s", goipp.Status(response.Code))
	}
}
func TestAirPrintAttributesCanBeEnabled(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		IPPName: "PUQU", IPPListen: ":8631", AdminListen: "127.0.0.1:8080", Advertise: true, AirPrint: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := New(st, &fakePrinter{}, nil)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()
	request := baseRequest(goipp.OpGetPrinterAttributes, 11)
	request.Operation.Add(goipp.MakeAttr("printer-uri", goipp.TagURI, goipp.String("ipp://localhost/ipp/print")))
	encoded, err := request.EncodeBytes()
	if err != nil {
		t.Fatal(err)
	}
	response := postIPP(t, httpServer.URL+"/ipp/print", encoded)
	if !containsString(response.Printer, "document-format-supported", raster.FormatApple) ||
		!containsString(response.Printer, "urf-supported", "RS203") {
		t.Fatal("AirPrint attributes are not advertised")
	}
}

func TestRequestedPrinterAttributesAreFiltered(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(st, &fakePrinter{}, nil)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()
	request := baseRequest(goipp.OpGetPrinterAttributes, 12)
	request.Operation.Add(goipp.MakeAttr("printer-uri", goipp.TagURI, goipp.String("ipp://localhost/ipp/print")))
	request.Operation.Add(goipp.MakeAttr("requested-attributes", goipp.TagKeyword, goipp.String("none")))
	encoded, err := request.EncodeBytes()
	if err != nil {
		t.Fatal(err)
	}
	response := postIPP(t, httpServer.URL+"/ipp/print", encoded)
	if len(response.Printer) != 0 {
		t.Fatalf("printer attributes = %v", response.Printer)
	}
}

func baseRequest(operation goipp.Op, id uint32) *goipp.Message {
	request := goipp.NewRequest(goipp.DefaultVersion, operation, id)
	request.Operation.Add(goipp.MakeAttr("attributes-charset", goipp.TagCharset, goipp.String("utf-8")))
	request.Operation.Add(goipp.MakeAttr("attributes-natural-language", goipp.TagLanguage, goipp.String("en-us")))
	return request
}

func postIPP(t *testing.T, endpoint string, body []byte) *goipp.Message {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", goipp.ContentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("HTTP %d: %s", resp.StatusCode, data)
	}
	var message goipp.Message
	if err := message.Decode(resp.Body); err != nil {
		t.Fatal(err)
	}
	return &message
}

func whitePWGRaster(width, height int) []byte {
	header := make([]byte, 1796)
	binary.BigEndian.PutUint32(header[276:280], 203)
	binary.BigEndian.PutUint32(header[280:284], 203)
	binary.BigEndian.PutUint32(header[372:376], uint32(width))
	binary.BigEndian.PutUint32(header[376:380], uint32(height))
	binary.BigEndian.PutUint32(header[384:388], 1)
	binary.BigEndian.PutUint32(header[388:392], 1)
	binary.BigEndian.PutUint32(header[392:396], uint32((width+7)/8))
	binary.BigEndian.PutUint32(header[396:400], 0)
	binary.BigEndian.PutUint32(header[400:404], 18)
	data := append([]byte("RaS2"), header...)
	data = append(data, byte(height-1), byte((width+7)/8-1), 0xff)
	return data
}

func integerFrom(attrs goipp.Attributes, name string) (int, bool) {
	for _, attr := range attrs {
		if attr.Name == name && len(attr.Values) > 0 {
			value, ok := attr.Values[0].V.(goipp.Integer)
			return int(value), ok
		}
	}
	return 0, false
}

func stringFrom(attrs goipp.Attributes, name string) string {
	for _, attr := range attrs {
		if attr.Name == name && len(attr.Values) > 0 {
			if value, ok := attr.Values[0].V.(goipp.String); ok {
				return string(value)
			}
		}
	}
	return ""
}
func containsString(attrs goipp.Attributes, name, want string) bool {
	for _, attr := range attrs {
		if attr.Name != name {
			continue
		}
		for _, value := range attr.Values {
			if text, ok := value.V.(goipp.String); ok && string(text) == want {
				return true
			}
		}
	}
	return false
}
