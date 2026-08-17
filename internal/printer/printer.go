// Package printer serializes official PUQU USB raster pages onto one physical link.
package printer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/imbytecat/puqu-ipp-bridge/internal/puqu"
)

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

type LinkInfo struct {
	Transport string `json:"transport"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	Address   string `json:"address"`
	MTU       *int   `json:"mtu"`
}

type Link interface {
	Write(data []byte) error
	OnDisconnect(cb func())
	Info() LinkInfo
	IsConnected() bool
	Disconnect() error
}

type Printer struct {
	link        Link
	printMu     sync.Mutex
	writeMu     sync.Mutex
	stateMu     sync.Mutex
	printing    bool
	settleDelay time.Duration
}

func New(link Link, onDisconnect func()) *Printer {
	p := &Printer{link: link, settleDelay: time.Second}
	if onDisconnect != nil {
		link.OnDisconnect(onDisconnect)
	}
	return p
}

func (p *Printer) Info() LinkInfo  { return p.link.Info() }
func (p *Printer) Connected() bool { return p.link.IsConnected() }
func (p *Printer) Busy() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.printing
}
func (p *Printer) Disconnect() error { return p.link.Disconnect() }
func (p *Printer) Cancel() error     { return p.Disconnect() }

func (p *Printer) Print(ctx context.Context, jobs []Job, _ Settings) (Result, error) {
	p.printMu.Lock()
	defer p.printMu.Unlock()

	if !p.Connected() {
		return Result{}, ErrLinkDown
	}
	for _, job := range jobs {
		if job.WidthBytes < 1 || job.WidthBytes > 72 || job.HeightPx < 1 || job.HeightPx > 65535 {
			return Result{}, errors.New("invalid bitmap dimensions")
		}
		if len(job.Data) != job.WidthBytes*job.HeightPx {
			return Result{}, fmt.Errorf("bitmap length %d does not match %dx%d", len(job.Data), job.WidthBytes, job.HeightPx)
		}
	}
	p.setPrinting(true)
	defer p.setPrinting(false)

	result := Result{Jobs: len(jobs)}
	for _, job := range jobs {
		header := puqu.PrintHeader(job.WidthBytes, job.HeightPx)
		page := make([]byte, len(header)+len(job.Data))
		copy(page, header)
		copy(page[len(header):], job.Data)
		for range max(job.Copies, 1) {
			if err := p.write(ctx, page); err != nil {
				return result, err
			}
			if err := sleep(ctx, p.settleDelay); err != nil {
				return result, p.cancelOnContext(err)
			}
			result.Bytes += len(job.Data)
		}
	}
	return result, nil
}

func (p *Printer) write(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.Connected() {
		return ErrLinkDown
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.link.Write(data)
}

func (p *Printer) setPrinting(printing bool) {
	p.stateMu.Lock()
	p.printing = printing
	p.stateMu.Unlock()
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
