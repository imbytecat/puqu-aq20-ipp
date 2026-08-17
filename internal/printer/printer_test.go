package printer

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imbytecat/puqu-ipp-bridge/internal/ble"
)

type fakeLink struct {
	mu           sync.Mutex
	writes       [][]byte
	connected    bool
	onData       func([]byte)
	onDisconnect func()
	writeErr     error
	writeAttempt chan struct{}
}

func newFakeLink() *fakeLink { return &fakeLink{connected: true} }

func (f *fakeLink) Write(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeAttempt != nil {
		close(f.writeAttempt)
		f.writeAttempt = nil
	}
	if f.writeErr != nil {
		err := f.writeErr
		f.writeErr = nil
		return err
	}
	f.writes = append(f.writes, append([]byte(nil), data...))
	return nil
}

func (f *fakeLink) OnData(cb func([]byte)) { f.onData = cb }
func (f *fakeLink) OnDisconnect(cb func()) { f.onDisconnect = cb }
func (f *fakeLink) Info() ble.Info         { return ble.Info{Name: "fake"} }
func (f *fakeLink) Gatt() []ble.Service    { return nil }
func (f *fakeLink) IsConnected() bool      { return f.connected }
func (f *fakeLink) Disconnect() error      { f.connected = false; return nil }
func (f *fakeLink) recorded() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.writes...)
}

type blockingLink struct {
	*fakeLink
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (f *blockingLink) Write(data []byte) error {
	if f.calls.Add(1) == 1 {
		close(f.entered)
		<-f.release
	}
	return f.fakeLink.Write(data)
}

func TestPrintSequence(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	data := []byte{0xff, 0x00, 0xff}
	result, err := p.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 3, Data: data, Copies: 1}}, Settings{
		Darkness: 5, Speed: 3, PaperType: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Jobs != 1 || result.Bytes != len(data) {
		t.Fatalf("result = %+v", result)
	}
	want := [][]byte{
		{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x0a},
		{0x3a, 0x5a, 0x53, 0x21, 0, 0, 0, 0xca},
		{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x3a},
		{0x3a, 1, 3, 0, 3, 0, 0, 0x15},
		data,
		{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x0a},
	}
	writes := fake.recorded()
	if len(writes) < len(want) {
		t.Fatalf("writes = %d, want at least %d", len(writes), len(want))
	}
	for i := range want {
		if !bytes.Equal(writes[i], want[i]) {
			t.Errorf("write[%d] = % x, want % x", i, writes[i], want[i])
		}
	}
}
func TestPrintStalePreflightIsRetryable(t *testing.T) {
	fake := newFakeLink()
	fake.writeErr = ble.ErrStaleGatt
	p := New(fake, nil)

	_, err := p.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}}, Settings{})
	if !errors.Is(err, ErrRetryableLink) {
		t.Fatalf("error = %v, want ErrRetryableLink", err)
	}
	if writes := fake.recorded(); len(writes) != 0 {
		t.Fatalf("preflight failure wrote printer data: % x", writes)
	}
}

func TestManagerRetriesOnReplacementConnection(t *testing.T) {
	stale := newFakeLink()
	stale.writeErr = ble.ErrStaleGatt
	stale.writeAttempt = make(chan struct{})
	attempt := stale.writeAttempt
	fresh := newFakeLink()
	manager := NewManager()
	manager.mu.Lock()
	manager.current = New(stale, nil)
	manager.mu.Unlock()

	go func() {
		<-attempt
		manager.mu.Lock()
		manager.current = New(fresh, nil)
		manager.notifyChangedLocked()
		manager.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := manager.Print(ctx, []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}}, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Jobs != 1 || result.Bytes != 1 {
		t.Fatalf("result = %+v", result)
	}
	writes := fresh.recorded()
	if len(writes) == 0 || !bytes.Equal(writes[0], []byte{0x3a, 0x5a, 0, 0, 0, 0, 0, 0x0a}) {
		t.Fatalf("replacement writes = % x", writes)
	}
}
func TestCancelDoesNotInterleaveProtocolWrites(t *testing.T) {
	link := &blockingLink{fakeLink: newFakeLink(), entered: make(chan struct{}), release: make(chan struct{})}
	p := New(link, nil)
	printDone := make(chan error, 1)
	go func() {
		_, err := p.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}}, Settings{})
		printDone <- err
	}()
	<-link.entered

	cancelDone := make(chan error, 1)
	go func() { cancelDone <- p.Cancel() }()
	select {
	case err := <-cancelDone:
		close(link.release)
		<-printDone
		t.Fatalf("cancel write interleaved with active command: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(link.release)
	if err := <-cancelDone; err != nil {
		t.Fatal(err)
	}
	if err := <-printDone; err != nil {
		t.Fatal(err)
	}
}

func TestLateBusyNotificationDoesNotStickAfterPrint(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	if _, err := p.Print(context.Background(), []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}}, Settings{}); err != nil {
		t.Fatal(err)
	}
	fake.onData([]byte{0x3a, 0x08, 0, 0, 0, 0, 0, 0})
	if p.Busy() {
		t.Fatal("late busy notification left idle printer marked busy")
	}
}

func TestPrintRejectsWrongBitmapLength(t *testing.T) {
	p := New(newFakeLink(), nil)
	_, err := p.Print(context.Background(), []Job{{WidthBytes: 2, HeightPx: 2, Data: []byte{1}}}, Settings{})
	if err == nil {
		t.Fatal("expected bitmap length error")
	}
}

func TestCanceledPrintSendsCancelFrame(t *testing.T) {
	fake := newFakeLink()
	p := New(fake, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Print(ctx, []Job{{WidthBytes: 1, HeightPx: 1, Data: []byte{0xff}}}, Settings{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	writes := fake.recorded()
	if len(writes) != 1 || !bytes.Equal(writes[0], []byte{0x3a, 0x5a, 0x33, 0, 0, 0, 0, 0x3a}) {
		t.Fatalf("writes = % x, want cancel frame", writes)
	}
}
