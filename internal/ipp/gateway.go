package ipp

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/imbytecat/puqu-aq20-ipp/internal/printer"
	"github.com/imbytecat/puqu-aq20-ipp/internal/store"
)

type PrinterFleet interface {
	Print(context.Context, int64, []printer.Job, printer.Settings) (printer.Result, error)
	Cancel(int64) error
	Status(int64) printer.Status
}

type fleetPrinter struct {
	fleet PrinterFleet
	id    int64
}

func (p fleetPrinter) Print(ctx context.Context, jobs []printer.Job, settings printer.Settings) (printer.Result, error) {
	return p.fleet.Print(ctx, p.id, jobs, settings)
}
func (p fleetPrinter) Cancel() error          { return p.fleet.Cancel(p.id) }
func (p fleetPrinter) Status() printer.Status { return p.fleet.Status(p.id) }

type queueRuntime struct {
	server *Server
	cancel context.CancelFunc
}

type Gateway struct {
	store     *store.Store
	fleet     PrinterFleet
	logger    *slog.Logger
	discovery *discovery

	mu     sync.RWMutex
	root   context.Context
	queues map[int64]*queueRuntime
}

func NewGateway(st *store.Store, fleet PrinterFleet, ippListen, adminListen string, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{
		store: st, fleet: fleet, logger: logger, discovery: newDiscovery(st, logger, ippListen, adminListen),
		queues: make(map[int64]*queueRuntime),
	}
}

func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	g.root = ctx
	g.mu.Unlock()
	if err := g.Reload(ctx); err != nil {
		return err
	}
	go g.discovery.Run(ctx)
	return nil
}

func (g *Gateway) Reload(ctx context.Context) error {
	printers, err := g.store.Printers(ctx)
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	next := make(map[int64]*queueRuntime, len(printers))
	for _, configured := range printers {
		if current := g.queues[configured.ID]; current != nil {
			if current.cancel == nil && g.root != nil {
				queueCtx, cancel := context.WithCancel(g.root)
				current.cancel = cancel
				current.server.Start(queueCtx)
			}
			next[configured.ID] = current
			continue
		}
		server := New(g.store, fleetPrinter{fleet: g.fleet, id: configured.ID}, configured.ID, configured.Slug, g.logger)
		runtime := &queueRuntime{server: server}
		if g.root != nil {
			queueCtx, cancel := context.WithCancel(g.root)
			runtime.cancel = cancel
			server.Start(queueCtx)
		}
		next[configured.ID] = runtime
	}
	for id, current := range g.queues {
		if next[id] == nil && current.cancel != nil {
			current.cancel()
		}
	}
	g.queues = next
	g.discovery.Reload()
	return nil
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ipp/{printer}", g.handle)
	mux.HandleFunc("GET /icon.svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = io.WriteString(w, printerIcon)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, printerPage)
	})
	return mux
}

func (g *Gateway) handle(w http.ResponseWriter, r *http.Request) {
	configured, err := g.store.PrinterBySlug(r.Context(), r.PathValue("printer"))
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "printer lookup failed", http.StatusInternalServerError)
		return
	}
	g.mu.RLock()
	runtime := g.queues[configured.ID]
	g.mu.RUnlock()
	if runtime == nil {
		http.Error(w, "printer runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	runtime.server.handle(w, r)
}

func (g *Gateway) QueueDepth(printerID int64) int {
	g.mu.RLock()
	runtime := g.queues[printerID]
	g.mu.RUnlock()
	if runtime == nil {
		return 0
	}
	return runtime.server.QueueDepth()
}

func (g *Gateway) Cancel(ctx context.Context, id int64) error {
	job, err := g.store.Job(ctx, id)
	if err != nil {
		return err
	}
	g.mu.RLock()
	runtime := g.queues[job.PrinterID]
	g.mu.RUnlock()
	if runtime == nil {
		return sql.ErrNoRows
	}
	return runtime.server.Cancel(ctx, id)
}

func (g *Gateway) ReloadDiscovery() { g.discovery.Reload() }
