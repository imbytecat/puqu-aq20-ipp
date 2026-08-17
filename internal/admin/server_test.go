package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/imbytecat/puqu-aq20-ipp/internal/config"
	"github.com/imbytecat/puqu-aq20-ipp/internal/fleet"
	"github.com/imbytecat/puqu-aq20-ipp/internal/ipp"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

func TestAdminRoutesPersistMultiplePrinters(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	printerFleet := fleet.New(st)
	runtimeConfig := config.Defaults()
	ippGateway := ipp.NewGateway(st, printerFleet, nil)
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<div id="root"></div>`)) })
	handler := New(st, printerFleet, ippGateway, runtimeConfig, ui, "test").Handler()

	response := serve(handler, http.MethodGet, "/api/status", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":"test"`) || !strings.Contains(response.Body.String(), `"ippListen":":8631"`) {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}

	response = serve(handler, http.MethodPost, "/api/devices", []byte(`{
		"nativeId":"dev-1","name":"Q20-test","address":"dev-1"
	}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"writeUuid":"ae01"`) {
		t.Fatalf("save device: %d %s", response.Code, response.Body.String())
	}
	var device deviceDTO
	if err := json.Unmarshal(response.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	printers, err := st.Printers(ctx)
	if err != nil || len(printers) != 1 {
		t.Fatalf("printers = %+v, %v", printers, err)
	}
	response = serve(handler, http.MethodPost, "/api/printers", []byte(`{
		"name":"Warehouse Labels","slug":"warehouse-labels","driver":"puqu-aq20",
		"deviceId":`+strconv64(device.ID)+`,"profileId":`+strconv64(printers[0].ProfileID)+`,
		"enabled":true
	}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"slug":"warehouse-labels"`) {
		t.Fatalf("create printer: %d %s", response.Code, response.Body.String())
	}
	configured, err := st.PrinterBySlug(ctx, "warehouse-labels")
	if err != nil || !configured.DeviceID.Valid || configured.DeviceID.Int64 != device.ID {
		t.Fatalf("printer = %+v, err=%v", configured, err)
	}

}

func TestAdminServesUIAndRejectsRemoteBitmapEndpoint(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	printerFleet := fleet.New(st)
	runtimeConfig := config.Defaults()
	ippGateway := ipp.NewGateway(st, printerFleet, nil)
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<div id="root"></div>`)) })
	handler := New(st, printerFleet, ippGateway, runtimeConfig, ui, "test").Handler()

	response := serve(handler, http.MethodGet, "/printer", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `id="root"`) {
		t.Fatalf("UI: %d %s", response.Code, response.Body.String())
	}
	response = serve(handler, http.MethodPost, "/api/print", []byte(`{}`))
	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy print endpoint status = %d", response.Code)
	}
}

func strconv64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func serve(handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}
