package ipp

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brutella/dnssd"

	"github.com/imbytecat/puqu-aq20-ipp/internal/raster"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

type discovery struct {
	store  *store.Store
	logger *slog.Logger
	reload chan struct{}
}

func newDiscovery(st *store.Store, logger *slog.Logger) *discovery {
	return &discovery{store: st, logger: logger, reload: make(chan struct{}, 1)}
}

func (d *discovery) Reload() {
	select {
	case d.reload <- struct{}{}:
	default:
	}
}

func (d *discovery) Run(ctx context.Context) {
	var lastInterfaces string
	for {
		settings, err := d.store.Settings(ctx)
		if err != nil {
			d.logger.Error("load discovery settings", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}
		if settings.Advertise == 0 {
			if !wait(ctx, d.reload, 0) {
				return
			}
			continue
		}

		port, err := listenPort(settings.IppListen)
		if err != nil {
			d.logger.Error("invalid IPP listen address", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}
		profile, err := d.store.ActiveProfile(ctx)
		if err != nil {
			d.logger.Error("load active profile for discovery", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}
		responder, err := dnssd.NewResponder()
		if err != nil {
			d.logger.Error("create DNS-SD responder", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}
		pdl := raster.FormatPWG
		if settings.Airprint == 1 {
			pdl += "," + raster.FormatApple
		}
		text := map[string]string{
			"txtvers": "1", "qtotal": "1", "rp": "ipp/print", "ty": "PUQU AQ20",
			"product": "(PUQU AQ20 IPP Bridge)", "pdl": pdl, "Color": "F", "Duplex": "F",
			"Copies": "T", "UUID": settings.PrinterUuid, "note": fmtProfile(profile),
		}
		if settings.Airprint == 1 {
			text["URF"] = "W8,SRGB24,RS203,DM1"
		}
		service, err := dnssd.NewService(dnssd.Config{Name: settings.IppName, Type: "_ipp._tcp", Port: port, Text: text})
		if err == nil {
			_, err = responder.Add(service)
		}
		if err != nil {
			d.logger.Error("register DNS-SD printer", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}

		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- responder.Respond(runCtx) }()
		lastInterfaces = interfaceSignature()
		ticker := time.NewTicker(5 * time.Second)
		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				cancel()
				ticker.Stop()
				return
			case <-d.reload:
				restart = true
			case err := <-done:
				if err != nil && ctx.Err() == nil {
					d.logger.Error("DNS-SD responder stopped", "error", err)
				}
				restart = true
			case <-ticker.C:
				now := interfaceSignature()
				if now != lastInterfaces {
					lastInterfaces = now
					restart = true
				}
			}
		}
		ticker.Stop()
		cancel()
	}
}

func listenPort(address string) (int, error) {
	if strings.HasPrefix(address, ":") {
		return strconv.Atoi(strings.TrimPrefix(address, ":"))
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(port)
}

func interfaceSignature() string {
	interfaces, _ := net.Interfaces()
	var parts []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			parts = append(parts, iface.Name+"="+address.String())
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func wait(ctx context.Context, reload <-chan struct{}, delay time.Duration) bool {
	if delay == 0 {
		select {
		case <-ctx.Done():
			return false
		case <-reload:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-reload:
		return true
	case <-timer.C:
		return true
	}
}
