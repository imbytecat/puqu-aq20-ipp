package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

const serviceRunName = "service-run"

type serviceProgram struct {
	data   string
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
	go func() { p.done <- runDaemon(ctx, p.data) }()
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
			data, _ := cmd.Flags().GetString("data")
			svc, err := newService(data)
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
			data, _ := cmd.Flags().GetString("data")
			svc, err := newService(data)
			if err != nil {
				return err
			}
			return svc.Run()
		},
	}
}

func newService(data string) (service.Service, error) {
	arguments := []string{serviceRunName}
	if data != "" {
		arguments = append(arguments, "--data", data)
	}
	return service.New(&serviceProgram{data: data}, &service.Config{
		Name: "puqu-aq20-ipp", DisplayName: "PUQU AQ20 IPP Bridge",
		Description: "Exposes a PUQU AQ20 Bluetooth label printer through IPP Everywhere.",
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
