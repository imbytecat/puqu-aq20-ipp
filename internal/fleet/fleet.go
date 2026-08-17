// Package fleet maps configured logical printers to independent printer managers.
package fleet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/imbytecat/puqu-ipp-bridge/internal/printer"
	"github.com/imbytecat/puqu-ipp-bridge/internal/store"
	"github.com/imbytecat/puqu-ipp-bridge/internal/usb"
)

type Driver struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`
}

func Drivers() []Driver {
	return []Driver{{ID: store.DriverPUQUAQ20, Name: "PUQU AQ20 / PQ / TQ / Q", Transport: "USB"}}
}

type runtime struct {
	manager     *printer.Manager
	cancel      context.CancelFunc
	fingerprint string
	enabled     bool
}

type Fleet struct {
	store *store.Store

	mu       sync.RWMutex
	root     context.Context
	runtimes map[int64]*runtime
}

func New(st *store.Store) *Fleet {
	return &Fleet{store: st, runtimes: make(map[int64]*runtime)}
}

func (f *Fleet) Start(ctx context.Context) error {
	f.mu.Lock()
	f.root = ctx
	f.mu.Unlock()
	return f.Reload(ctx)
}

func (f *Fleet) Reload(ctx context.Context) error {
	configs, err := f.store.Printers(ctx)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	next := make(map[int64]*runtime, len(configs))
	for _, config := range configs {
		deviceUpdated := int64(0)
		if config.DeviceID.Valid {
			device, err := f.store.Device(ctx, config.DeviceID.Int64)
			if err != nil {
				return err
			}
			deviceUpdated = device.UpdatedAt
		}
		fingerprint := fmt.Sprintf("%s:%t:%d:%d", config.Driver, config.Enabled == 1, nullableID(config.DeviceID), deviceUpdated)
		if current := f.runtimes[config.ID]; current != nil && current.fingerprint == fingerprint {
			next[config.ID] = current
			continue
		}
		if current := f.runtimes[config.ID]; current != nil {
			stop(current)
		}
		next[config.ID] = f.startLocked(config.ID, config.Enabled == 1, fingerprint)
	}
	for id, current := range f.runtimes {
		if next[id] == nil {
			stop(current)
		}
	}
	f.runtimes = next
	return nil
}

func (f *Fleet) startLocked(id int64, enabled bool, fingerprint string) *runtime {
	r := &runtime{fingerprint: fingerprint, enabled: enabled}
	if !enabled || f.root == nil {
		return r
	}
	ctx, cancel := context.WithCancel(f.root)
	r.cancel = cancel
	r.manager = printer.NewManager()
	r.manager.StartAutoConnect(ctx, func(ctx context.Context) (printer.Link, error) {
		config, err := f.store.Printer(ctx, id)
		if err != nil {
			return nil, err
		}
		if config.Enabled != 1 {
			return nil, errors.New("printer is disabled")
		}
		if config.Driver != store.DriverPUQUAQ20 {
			return nil, fmt.Errorf("unsupported printer driver %q", config.Driver)
		}
		if !config.DeviceID.Valid {
			return nil, errors.New("no device assigned")
		}
		device, err := f.store.Device(ctx, config.DeviceID.Int64)
		if err != nil {
			return nil, err
		}
		if device.Transport != store.TransportUSB {
			return nil, fmt.Errorf("device %q is not a USB printer", device.Name)
		}
		return usb.Connect(usb.ConnectOptions{ID: device.NativeID})
	})
	return r
}

func stop(r *runtime) {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.manager != nil {
		r.manager.Disconnect()
	}
}

func (f *Fleet) ScanUSB(ctx context.Context) ([]usb.Device, error) {
	return usb.Scan(ctx)
}

func (f *Fleet) Reconnect(id int64) error {
	r, err := f.runtime(id)
	if err != nil {
		return err
	}
	r.manager.Reconnect()
	return nil
}

func (f *Fleet) Print(ctx context.Context, id int64, jobs []printer.Job, settings printer.Settings) (printer.Result, error) {
	r, err := f.runtime(id)
	if err != nil {
		return printer.Result{}, err
	}
	return r.manager.Print(ctx, jobs, settings)
}

func (f *Fleet) Cancel(id int64) error {
	r, err := f.runtime(id)
	if err != nil {
		return err
	}
	return r.manager.Cancel()
}

func (f *Fleet) Status(id int64) printer.Status {
	f.mu.RLock()
	r := f.runtimes[id]
	f.mu.RUnlock()
	if r == nil {
		return printer.Status{LastError: "printer runtime unavailable"}
	}
	if !r.enabled {
		return printer.Status{LastError: "printer is disabled"}
	}
	if r.manager == nil {
		return printer.Status{LastError: "printer runtime not started"}
	}
	return r.manager.Status()
}

func (f *Fleet) Shutdown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runtimes {
		stop(r)
	}
	f.runtimes = make(map[int64]*runtime)
}

func (f *Fleet) runtime(id int64) (*runtime, error) {
	f.mu.RLock()
	r := f.runtimes[id]
	f.mu.RUnlock()
	if r == nil || r.manager == nil {
		return nil, printer.ErrNotConnected
	}
	return r, nil
}

func nullableID(value sql.NullInt64) int64 {
	if value.Valid {
		return value.Int64
	}
	return 0
}
