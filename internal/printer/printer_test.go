package printer

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/imbytecat/puqu-aq20-ipp/internal/ble"
)

type fakeLink struct {
	mu           sync.Mutex
	writes       [][]byte
	connected    bool
	onData       func([]byte)
	onDisconnect func()
}

func newFakeLink() *fakeLink { return &fakeLink{connected: true} }

func (f *fakeLink) Write(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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
