package printer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/imbytecat/puqu-aq20-ipp/internal/ble"
)

var (
	ErrNotConnected = errors.New("printer is not connected")
	ErrLinkDown     = errors.New("printer link is down")
)

type Status struct {
	Connected  bool          `json:"connected"`
	Connecting bool          `json:"connecting"`
	Busy       bool          `json:"busy"`
	LastError  string        `json:"lastError,omitempty"`
	Info       *ble.Info     `json:"info,omitempty"`
	Gatt       []ble.Service `json:"gatt,omitempty"`
}

type OptionsLoader func(context.Context) (ble.ConnectOptions, error)

type Manager struct {
	mu         sync.Mutex
	connectMu  sync.Mutex
	current    *Printer
	connecting bool
	lastError  string
	reconnect  chan struct{}
}

func NewManager() *Manager {
	return &Manager{reconnect: make(chan struct{}, 1)}
}

func (m *Manager) StartAutoConnect(ctx context.Context, load OptionsLoader) {
	go func() {
		m.requestReconnect()
		for {
			select {
			case <-ctx.Done():
				m.Disconnect()
				return
			case <-m.reconnect:
			}

			if m.Status().Connected {
				continue
			}
			opts, err := load(ctx)
			if err == nil {
				_, _, err = m.Connect(ctx, opts)
			}
			if err != nil {
				m.setError(err)
				timer := time.NewTimer(5 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					m.Disconnect()
					return
				case <-timer.C:
					m.requestReconnect()
				}
			}
		}
	}()
}

func (m *Manager) Scan(ctx context.Context, window time.Duration) ([]ble.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ble.Scan(window)
}

func (m *Manager) Connect(ctx context.Context, opts ble.ConnectOptions) (ble.Info, []ble.Service, error) {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ble.Info{}, nil, err
	}

	m.mu.Lock()
	old := m.current
	m.current = nil
	m.connecting = true
	m.lastError = ""
	m.mu.Unlock()
	if old != nil {
		_ = old.Disconnect()
		if err := sleep(ctx, 300*time.Millisecond); err != nil {
			m.finishConnect(err)
			return ble.Info{}, nil, err
		}
	}

	conn, err := ble.Connect(opts)
	if err != nil {
		m.finishConnect(err)
		return ble.Info{}, nil, err
	}

	var next *Printer
	next = New(conn, func() {
		m.mu.Lock()
		if m.current == next {
			m.current = nil
			m.lastError = ErrLinkDown.Error()
		}
		m.mu.Unlock()
		m.requestReconnect()
	})

	m.mu.Lock()
	m.current = next
	m.connecting = false
	m.lastError = ""
	m.mu.Unlock()
	return next.Info(), next.Gatt(), nil
}

func (m *Manager) finishConnect(err error) {
	m.mu.Lock()
	m.connecting = false
	if err != nil {
		m.lastError = err.Error()
	}
	m.mu.Unlock()
}

func (m *Manager) setError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	m.lastError = err.Error()
	m.mu.Unlock()
}

func (m *Manager) Reconnect() {
	m.Disconnect()
	m.requestReconnect()
}

func (m *Manager) requestReconnect() {
	select {
	case m.reconnect <- struct{}{}:
	default:
	}
}

func (m *Manager) Disconnect() {
	m.mu.Lock()
	p := m.current
	m.current = nil
	m.connecting = false
	m.mu.Unlock()
	if p != nil {
		_ = p.Disconnect()
	}
}

func (m *Manager) Print(ctx context.Context, jobs []Job, settings Settings) (Result, error) {
	m.mu.Lock()
	p := m.current
	m.mu.Unlock()
	if p == nil {
		return Result{}, ErrNotConnected
	}
	if !p.Connected() {
		m.Disconnect()
		m.requestReconnect()
		return Result{}, ErrLinkDown
	}
	result, err := p.Print(ctx, jobs, settings)
	if err != nil {
		m.setError(err)
	}
	return result, err
}

func (m *Manager) Cancel() error {
	m.mu.Lock()
	p := m.current
	m.mu.Unlock()
	if p == nil {
		return ErrNotConnected
	}
	return p.Cancel()
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	p := m.current
	status := Status{Connecting: m.connecting, LastError: m.lastError}
	m.mu.Unlock()
	if p == nil || !p.Connected() {
		return status
	}
	info := p.Info()
	status.Connected = true
	status.Busy = p.Busy()
	status.Info = &info
	status.Gatt = p.Gatt()
	return status
}
