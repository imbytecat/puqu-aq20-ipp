package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/imbytecat/puqu-ipp-bridge/internal/config"
	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

const serviceRunName = "service-run"

type serviceProgram struct {
	config config.Config
	cancel context.CancelFunc
	done   chan error
	mu     sync.Mutex
}

func (p *serviceProgram) Start(service.Service) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)
	go func() { p.done <- runDaemon(ctx, p.config) }()
	return nil
}

func (p *serviceProgram) Stop(service.Service) error {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		return <-done
	}
	return nil
}

func serviceCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "service <install|uninstall|start|stop|restart|status>",
		Short:     "Manage the background system service",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"install", "uninstall", "start", "stop", "restart", "status"},
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeConfig, err := commandConfig(cmd)
			if err != nil {
				return err
			}
			if args[0] == "install" && runtimeConfig.HasEphemeralOverrides() {
				return fmt.Errorf("service install only persists %s; move environment or CLI overrides into that file", runtimeConfig.ConfigFile)
			}
			svc, err := newService(runtimeConfig)
			if err != nil {
				return err
			}
			if args[0] == "status" {
				status, err := svc.Status()
				if err != nil {
					return err
				}
				fmt.Println(statusText(status))
				return nil
			}
			return service.Control(svc, args[0])
		},
	}
}

func serviceRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    serviceRunName,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeConfig, err := commandConfig(cmd)
			if err != nil {
				return err
			}
			svc, err := newService(runtimeConfig)
			if err != nil {
				return err
			}
			return svc.Run()
		},
	}
}

func newService(runtimeConfig config.Config) (service.Service, error) {
	arguments := []string{serviceRunName}
	if runtimeConfig.ConfigFileLoaded {
		arguments = append(arguments, "--config", runtimeConfig.ConfigFile)
	}
	return service.New(&serviceProgram{config: runtimeConfig}, &service.Config{
		Name: "puqu-ipp", DisplayName: "PUQU IPP Bridge",
		Description: "Exposes PUQU AQ20 USB label printers through direct IPP queues.",
		Arguments:   arguments,
	})
}

func statusText(status service.Status) string {
	switch status {
	case service.StatusRunning:
		return "running"
	case service.StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}
