package printer

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeLink struct {
	mu           sync.Mutex
	writes       [][]byte
	attempts     int
	connected    bool
	onDisconnect func()
	writeErr     error
}

func newFakeLink() *fakeLink { return &fakeLink{connected: true} }

func (f *fakeLink) Write(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if !f.connected {
		return ErrLinkDown
	}
	if f.writeErr != nil {
		err := f.writeErr
		f.writeErr = nil
		return err
	}
	f.writes = append(f.writes, append([]byte(nil), data...))
	return nil
}

func (f *fakeLink) OnDisconnect(cb func()) { f.onDisconnect = cb }
func (f *fakeLink) Info() LinkInfo         { return LinkInfo{Name: "fake"} }
func (f *fakeLink) IsConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}
func (f *fakeLink) Disconnect() error {
	f.mu.Lock()
	f.connected = false
	cb := f.onDisconnect
	f.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}
func (f *fakeLink) recorded() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.writes...)
}
func (f *fakeLink) writeAttempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

func TestPrintSequenceUsesOfficialUSBPageFrame(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	p.settleDelay = 0
	data := []byte{0xff, 0x00, 0xff}
	result, err := p.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 3, Data: data, Copies: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Jobs != 1 || result.Bytes != len(data) {
		t.Fatalf("result = %+v", result)
	}
	writes := fake.recorded()
	want := append([]byte{0x2a, 0x76, 0x30, 0x02, 1, 0, 3, 0}, data...)
	if len(writes) != 1 || !bytes.Equal(writes[0], want) {
		t.Fatalf("writes = % x, want % x", writes, want)
	}
}

func TestPrintCopiesRepeatWholePageFrame(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	p.settleDelay = 0
	result, err := p.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0x80}, Copies: 2}})
	if err != nil {
		t.Fatal(err)
	}
	writes := fake.recorded()
	if len(writes) != 2 || !bytes.Equal(writes[0], writes[1]) || result.Bytes != 2 {
		t.Fatalf("writes = % x, result = %+v", writes, result)
	}
}

func TestManagerDoesNotReplayFailedPage(t *testing.T) {
	fake := newFakeLink()
	fake.writeErr = errors.New("write failed")
	manager := NewManager()
	manager.mu.Lock()
	manager.current = New(fake, nil)
	manager.current.settleDelay = 0
	manager.mu.Unlock()

	_, err := manager.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}})
	if err == nil {
		t.Fatal("expected write error")
	}
	if attempts := fake.writeAttempts(); attempts != 1 {
		t.Fatalf("write attempts = %d, want 1", attempts)
	}
}

func TestCancelDisconnectsLink(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	if err := p.Cancel(); err != nil {
		t.Fatal(err)
	}
	if p.Connected() {
		t.Fatal("printer remained connected after cancel")
	}
}

func TestPrintRejectsInvalidBitmap(t *testing.T) {
	p := New(newFakeLink(), nil)
	if _, err := p.Print(context.Background(), []Job{{WidthBytes: 2, HeightPx: 2, Data: []byte{1}}}); err == nil {
		t.Fatal("expected bitmap length error")
	}
	if _, err := p.Print(context.Background(), []Job{{WidthBytes: 73, HeightPx: 1, Data: make([]byte, 73)}}); err == nil {
		t.Fatal("expected official 203 dpi width limit error")
	}
}

func TestCanceledContextWritesNothing(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	p.settleDelay = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Print(ctx, []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if attempts := fake.writeAttempts(); attempts != 0 {
		t.Fatalf("write attempts = %d, want 0", attempts)
	}
}
