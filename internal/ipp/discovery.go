package ipp

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"

	"github.com/imbytecat/puqu-aq20-ipp/internal/raster"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

type discovery struct {
	store       *store.Store
	logger      *slog.Logger
	ippListen   string
	adminListen string
	reload      chan struct{}
}

func newDiscovery(st *store.Store, logger *slog.Logger, ippListen, adminListen string) *discovery {
	return &discovery{store: st, logger: logger, ippListen: ippListen, adminListen: adminListen, reload: make(chan struct{}, 1)}
}

func (d *discovery) Reload() {
	select {
	case d.reload <- struct{}{}:
	default:
	}
}

func (d *discovery) Run(ctx context.Context) {
	for {
		port, err := listenPort(d.ippListen)
		if err != nil {
			d.logger.Error("invalid IPP listen address", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}
		printers, err := d.store.Printers(ctx)
		if err != nil {
			d.logger.Error("load printers for discovery", "error", err)
			if !wait(ctx, d.reload, 5*time.Second) {
				return
			}
			continue
		}

		var responders []*zeroconf.Server
		for _, configured := range printers {
			if configured.Enabled != 1 || configured.Advertise != 1 {
				continue
			}
			profile, err := d.store.Profile(ctx, configured.ProfileID)
			if err != nil {
				d.logger.Error("load printer profile for discovery", "printer", configured.ID, "error", err)
				continue
			}
			formats := raster.FormatPWG + "," + raster.FormatJPEG
			serviceType := "_ipp._tcp,_print"
			urf := ""
			if configured.Airprint == 1 {
				formats += "," + raster.FormatApple
				serviceType += ",_universal"
				urf = "W8,SRGB24,RS203,DM1"
			}
			text := []string{
				"txtvers=1", "qtotal=1", "rp=ipp/" + configured.Slug, "ty=PUQU AQ20",
				"product=(PUQU AQ20 IPP Bridge)", "pdl=" + formats, "Color=F", "Duplex=F", "Copies=T",
				"UUID=" + configured.Uuid, "note=" + fmtProfile(profile), "adminurl=" + advertisedHTTPURL(d.adminListen),
			}
			if urf != "" {
				text = append(text, "URF="+urf)
			}
			responder, err := zeroconf.Register(advertisedName(configured), serviceType, "local.", port, text, nil)
			if err != nil {
				d.logger.Error("register DNS-SD printer", "printer", configured.ID, "error", err)
				continue
			}
			responders = append(responders, responder)
		}

		interfaces := interfaceSignature()
		ticker := time.NewTicker(5 * time.Second)
		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				shutdownResponders(responders)
				ticker.Stop()
				return
			case <-d.reload:
				restart = true
			case <-ticker.C:
				if current := interfaceSignature(); current != interfaces {
					interfaces = current
					restart = true
				}
			}
		}
		ticker.Stop()
		shutdownResponders(responders)
	}
}

func shutdownResponders(responders []*zeroconf.Server) {
	for _, responder := range responders {
		responder.Shutdown()
	}
}

func advertisedName(configured *store.Printer) string {
	name := configured.Name
	for _, char := range name {
		if char > 127 {
			name = "PUQU " + configured.Slug
			break
		}
	}
	suffix := configured.Uuid
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	return name + " (" + suffix + ")"
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

func advertisedHTTPURL(listen string) string {
	host, _ := os.Hostname()
	host = strings.TrimSuffix(host, ".")
	if !strings.HasSuffix(strings.ToLower(host), ".local") {
		host += ".local"
	}
	port, err := listenPort(listen)
	if err != nil {
		port = 8631
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
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
