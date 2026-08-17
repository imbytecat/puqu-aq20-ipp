// Package printer applies PUQU semantics to a device-agnostic BLE link.
package printer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/imbytecat/puqu-ipp-bridge/internal/ble"
	"github.com/imbytecat/puqu-ipp-bridge/internal/puqu"
)

var ErrPrintTimeout = errors.New("printer did not become idle before timeout")

// Job is one rasterized label ready to print (1bpp, MSB-first, 1=black).
type Job struct {
	WidthBytes int
	HeightPx   int
	Data       []byte
	Copies     int
}

type Settings struct {
	Darkness  int
	Speed     int
	PaperType int
}

type Result struct {
	Jobs  int
	Bytes int
}

// Link is the only seam between PUQU printing and the BLE implementation.
type Link interface {
	Write(data []byte) error
	OnData(cb func([]byte))
	OnDisconnect(cb func())
	Info() ble.Info
	Gatt() []ble.Service
	IsConnected() bool
	Disconnect() error
}

type Printer struct {
	link     Link
	printMu  sync.Mutex
	writeMu  sync.Mutex
	stateMu  sync.Mutex
	printing bool
	busy     bool
}

func New(link Link, onDisconnect func()) *Printer {
	p := &Printer{link: link}
	link.OnData(func(data []byte) {
		if status, ok := puqu.ParseStatus(data); ok {
			p.stateMu.Lock()
			if p.printing {
				p.busy = status.Busy
			} else {
				p.busy = false
			}
			p.stateMu.Unlock()
		}
	})
	if onDisconnect != nil {
		link.OnDisconnect(onDisconnect)
	}
	return p
}

func (p *Printer) Info() ble.Info      { return p.link.Info() }
func (p *Printer) Gatt() []ble.Service { return p.link.Gatt() }
func (p *Printer) Connected() bool     { return p.link.IsConnected() }
func (p *Printer) Busy() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.printing
}
func (p *Printer) Disconnect() error { return p.link.Disconnect() }
func (p *Printer) Cancel() error     { return p.writeLink(puqu.Cancel()) }

func (p *Printer) Print(ctx context.Context, jobs []Job, settings Settings) (Result, error) {
	p.printMu.Lock()
	defer p.printMu.Unlock()

	if !p.Connected() {
		return Result{}, ErrLinkDown
	}
	for _, job := range jobs {
		if job.WidthBytes < 1 || job.WidthBytes > 255 || job.HeightPx < 1 || job.HeightPx > 65535 {
			return Result{}, errors.New("invalid bitmap dimensions")
		}
		if len(job.Data) != job.WidthBytes*job.HeightPx {
			return Result{}, fmt.Errorf("bitmap length %d does not match %dx%d", len(job.Data), job.WidthBytes, job.HeightPx)
		}
	}
	p.setPrinting(true)
	defer p.setPrinting(false)

	if err := p.write(ctx, puqu.ReadState()); err != nil {
		if errors.Is(err, ble.ErrStaleGatt) {
			return Result{}, fmt.Errorf("%w: %v", ErrRetryableLink, err)
		}
		return Result{}, err
	}

	frame := puqu.DeviceDetails(puqu.DeviceSettings{
		Darkness: settings.Darkness, Speed: settings.Speed, PaperType: settings.PaperType, Temporary: true,
	})
	if err := p.write(ctx, frame); err != nil {
		return Result{}, err
	}
	if err := sleep(ctx, 40*time.Millisecond); err != nil {
		return Result{}, p.cancelOnContext(err)
	}
	if err := p.write(ctx, puqu.Wake()); err != nil {
		return Result{}, err
	}
	if err := sleep(ctx, 60*time.Millisecond); err != nil {
		return Result{}, p.cancelOnContext(err)
	}

	result := Result{Jobs: len(jobs)}
	for _, job := range jobs {
		copies := max(job.Copies, 1)
		header := puqu.PrintHeader(job.WidthBytes, job.HeightPx, len(job.Data))
		for range copies {
			if err := p.write(ctx, header); err != nil {
				return result, err
			}
			if err := sleep(ctx, 10*time.Millisecond); err != nil {
				return result, p.cancelOnContext(err)
			}
			if err := p.write(ctx, job.Data); err != nil {
				return result, err
			}
			if err := p.waitUntilIdle(ctx, 15*time.Second); err != nil {
				return result, p.cancelOnContext(err)
			}
			result.Bytes += len(job.Data)
		}
	}
	return result, nil
}

func (p *Printer) write(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		_ = p.writeLink(puqu.Cancel())
		return err
	}
	if !p.Connected() {
		return ErrLinkDown
	}
	return p.writeLink(data)
}

func (p *Printer) waitUntilIdle(ctx context.Context, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	if err := sleep(ctx, 150*time.Millisecond); err != nil {
		return err
	}
	for {
		if err := p.write(ctx, puqu.ReadState()); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrPrintTimeout
		case <-time.After(200 * time.Millisecond):
			if !p.deviceBusy() {
				return nil
			}
		}
	}
}
func (p *Printer) writeLink(data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.link.Write(data)
}

func (p *Printer) setPrinting(printing bool) {
	p.stateMu.Lock()
	p.printing = printing
	p.busy = false
	p.stateMu.Unlock()
}

func (p *Printer) deviceBusy() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.busy
}

func (p *Printer) cancelOnContext(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		_ = p.Cancel()
	}
	return err
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
