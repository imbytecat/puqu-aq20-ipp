package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/imbytecat/puqu-aq20-ipp/internal/admin"
	"github.com/imbytecat/puqu-aq20-ipp/internal/ble"
	ippserver "github.com/imbytecat/puqu-aq20-ipp/internal/ipp"
	"github.com/imbytecat/puqu-aq20-ipp/internal/printer"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
	"github.com/imbytecat/puqu-aq20-ipp/internal/web"
)

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the IPP bridge and local configuration UI",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runServe(cmd) },
	}
}

func runServe(cmd *cobra.Command) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dataPath, _ := cmd.Flags().GetString("data")
	return runDaemon(ctx, dataPath)
}

func runDaemon(ctx context.Context, dataPath string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(ctx, dataPath)
	if err != nil {
		return err
	}
	defer st.Close()

	manager := printer.NewManager()
	manager.StartAutoConnect(ctx, func(ctx context.Context) (ble.ConnectOptions, error) {
		device, err := st.SelectedDevice(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ble.ConnectOptions{}, errors.New("no Bluetooth printer selected")
			}
			return ble.ConnectOptions{}, err
		}
		notify := ""
		if device.NotifyUuid.Valid {
			notify = device.NotifyUuid.String
		}
		return ble.ConnectOptions{ID: device.NativeID, Address: device.Address, WriteUUID: device.WriteUuid, NotifyUUID: notify}, nil
	})
	defer func() {
		manager.Disconnect()
		ble.Shutdown()
	}()

	settings, err := st.Settings(ctx)
	if err != nil {
		return err
	}
	if err := store.ValidateSettings(store.SettingsUpdate{
		IPPName: settings.IppName, IPPListen: settings.IppListen, AdminListen: settings.AdminListen,
		Advertise: settings.Advertise == 1, AirPrint: settings.Airprint == 1,
	}); err != nil {
		return err
	}
	ipp := ippserver.New(st, manager, logger)
	ipp.Start(ctx)
	adminServer := admin.New(st, manager, ipp, web.Handler(), version)

	ippListener, err := net.Listen("tcp", settings.IppListen)
	if err != nil {
		return fmt.Errorf("listen for IPP on %s: %w", settings.IppListen, err)
	}
	defer ippListener.Close()
	adminListener, err := net.Listen("tcp", settings.AdminListen)
	if err != nil {
		return fmt.Errorf("listen for admin UI on %s: %w", settings.AdminListen, err)
	}
	defer adminListener.Close()

	ippHTTP := &http.Server{Handler: ipp.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	adminHTTP := &http.Server{Handler: adminServer.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 2)
	go func() { errCh <- serveHTTP(ippHTTP, ippListener) }()
	go func() { errCh <- serveHTTP(adminHTTP, adminListener) }()
	logger.Info("PUQU IPP bridge started", "ipp", settings.IppListen, "admin", "http://"+settings.AdminListen)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); _ = ippHTTP.Shutdown(shutdownCtx) }()
	go func() { defer wait.Done(); _ = adminHTTP.Shutdown(shutdownCtx) }()
	wait.Wait()
	return nil
}

func serveHTTP(server *http.Server, listener net.Listener) error {
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
