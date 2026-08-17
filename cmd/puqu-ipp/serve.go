package main

import (
	"context"
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
	"github.com/imbytecat/puqu-aq20-ipp/internal/config"
	"github.com/imbytecat/puqu-aq20-ipp/internal/fleet"
	ippserver "github.com/imbytecat/puqu-aq20-ipp/internal/ipp"
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
	runtimeConfig, err := commandConfig(cmd)
	if err != nil {
		return err
	}
	return runDaemon(ctx, runtimeConfig)
}

func commandConfig(cmd *cobra.Command) (config.Config, error) {
	configFile, _ := cmd.Flags().GetString("config")
	return config.Load(config.LoadOptions{
		Path:        configFile,
		RequireFile: cmd.Flags().Changed("config"),
		Flags:       cmd.Flags(),
	})
}

func runDaemon(ctx context.Context, runtimeConfig config.Config) error {
	if err := runtimeConfig.Validate(); err != nil {
		return err
	}
	if runtimeConfig.DataPath == "" {
		dataPath, err := store.DefaultPath()
		if err != nil {
			return err
		}
		runtimeConfig.DataPath = dataPath
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: runtimeConfig.SlogLevel()}))
	st, err := store.Open(ctx, runtimeConfig.DataPath)
	if err != nil {
		return err
	}
	defer st.Close()

	printerFleet := fleet.New(st)
	if err := printerFleet.Start(ctx); err != nil {
		return err
	}
	defer func() {
		printerFleet.Shutdown()
		ble.Shutdown()
	}()

	ipp := ippserver.NewGateway(st, printerFleet, logger)
	if err := ipp.Start(ctx); err != nil {
		return err
	}
	adminServer := admin.New(st, printerFleet, ipp, runtimeConfig, web.Handler(), version)

	ippListener, err := net.Listen("tcp", runtimeConfig.IPPListen)
	if err != nil {
		return fmt.Errorf("listen for IPP on %s: %w", runtimeConfig.IPPListen, err)
	}
	defer ippListener.Close()
	adminListener, err := net.Listen("tcp", runtimeConfig.AdminListen)
	if err != nil {
		return fmt.Errorf("listen for admin UI on %s: %w", runtimeConfig.AdminListen, err)
	}
	defer adminListener.Close()

	ippHTTP := &http.Server{Handler: ipp.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	adminHTTP := &http.Server{Handler: adminServer.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 2)
	go func() { errCh <- serveHTTP(ippHTTP, ippListener) }()
	go func() { errCh <- serveHTTP(adminHTTP, adminListener) }()
	logger.Info("PUQU IPP bridge started", "ipp", runtimeConfig.IPPListen, "admin", "http://"+runtimeConfig.AdminListen, "config", runtimeConfig.ConfigFile, "database", runtimeConfig.DataPath)

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
