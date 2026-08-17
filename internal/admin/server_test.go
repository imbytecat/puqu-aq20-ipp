package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imbytecat/puqu-aq20-ipp/internal/ipp"
	"github.com/imbytecat/puqu-aq20-ipp/internal/printer"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

func TestAdminRoutesPersistConfiguration(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager := printer.NewManager()
	ippServer := ipp.New(st, manager, nil)
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<div id="root"></div>`)) })
	handler := New(st, manager, ippServer, ui, "test").Handler()

	response := serve(handler, http.MethodGet, "/api/status", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":"test"`) {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}

	response = serve(handler, http.MethodPut, "/api/devices", []byte(`{
		"nativeId":"dev-1","name":"Q20-test","address":"dev-1","selected":true
	}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"writeUuid":"ae01"`) {
		t.Fatalf("save device: %d %s", response.Code, response.Body.String())
	}
	selected, err := st.SelectedDevice(ctx)
	if err != nil || selected.NativeID != "dev-1" {
		t.Fatalf("selected = %+v, err=%v", selected, err)
	}

	response = serve(handler, http.MethodPut, "/api/settings", []byte(`{
		"ippName":"Warehouse Labels","ippListen":":8631","adminListen":"127.0.0.1:8080","advertise":true,"airPrint":true
	}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"restartRequired":true`) {
		t.Fatalf("settings: %d %s", response.Code, response.Body.String())
	}
	settings, err := st.Settings(ctx)
	if err != nil || settings.IppName != "Warehouse Labels" || settings.Airprint != 1 {
		t.Fatalf("settings = %+v, err=%v", settings, err)
	}
}

func TestAdminServesUIAndRejectsRemoteBitmapEndpoint(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager := printer.NewManager()
	ippServer := ipp.New(st, manager, nil)
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<div id="root"></div>`)) })
	handler := New(st, manager, ippServer, ui, "test").Handler()

	response := serve(handler, http.MethodGet, "/printer", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `id="root"`) {
		t.Fatalf("UI: %d %s", response.Code, response.Body.String())
	}
	response = serve(handler, http.MethodPost, "/api/print", []byte(`{}`))
	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy print endpoint status = %d", response.Code)
	}
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
