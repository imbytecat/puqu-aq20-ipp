// Package ble is a device-agnostic BLE central adapter over tinygo.org/x/bluetooth,
// which uses each OS's native Bluetooth stack (BlueZ/D-Bus on Linux, CoreBluetooth on
// macOS, WinRT on Windows). No raw HCI, so no contention with the system Bluetooth
// daemon. It knows nothing about printers or the PUQU protocol — see internal/printer.
package ble

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// Device is a device discovered during a scan.
type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	RSSI    int    `json:"rssi"`
}

// ConnectOptions selects a device and optionally pins the GATT endpoints.
type ConnectOptions struct {
	NamePrefix       string
	ID               string
	Address          string
	WriteUUID        string
	NotifyUUID       string
	ChunkSize        int
	PacketIntervalMs int
	ScanTimeoutMs    int
}

// Characteristic is one GATT characteristic in the connected device's table.
type Characteristic struct {
	UUID       string   `json:"uuid"`
	Properties []string `json:"properties"`
}

// Service is one GATT service and its characteristics.
type Service struct {
	UUID            string           `json:"uuid"`
	Characteristics []Characteristic `json:"characteristics"`
}

// Info describes a live connection and the chosen write/notify endpoints.
type Info struct {
	Name            string  `json:"name"`
	ID              string  `json:"id"`
	Address         string  `json:"address"`
	MTU             *int    `json:"mtu"`
	WriteService    string  `json:"writeService"`
	WriteChar       string  `json:"writeChar"`
	NotifyChar      *string `json:"notifyChar"`
	WithoutResponse bool    `json:"withoutResponse"`
}

var (
	ErrStaleGatt = errors.New("stale GATT connection")

	adapter     = bluetooth.DefaultAdapter
	enableOnce  sync.Once
	enableErr   error
	scanMu      sync.Mutex
	connections sync.Map
)

func enable() error {
	enableOnce.Do(func() {
		enableErr = adapter.Enable()
		if enableErr == nil {
			adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
				if connected {
					return
				}
				if value, ok := connections.Load(device.Address.String()); ok {
					value.(*Conn).markDisconnected()
				}
			})
		}
	})
	return enableErr
}

func norm(u string) string { return strings.ToLower(strings.ReplaceAll(u, "-", "")) }

// BlueZ/tinygo report full 128-bit UUIDs; saved configs use the 16-bit short form
// (ae01). Compare on either representation.
const baseSuffix = "00001000800000805f9b34fb"

func shortUUID(u string) string {
	n := norm(u)
	if len(n) == 32 && strings.HasPrefix(n, "0000") && strings.HasSuffix(n, baseSuffix) {
		return n[4:8]
	}
	return n
}

func uuidEq(a, b string) bool { return norm(a) == norm(b) || shortUUID(a) == shortUUID(b) }

func nameOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func hasProp(props []string, want string) bool {
	for _, p := range props {
		if p == want {
			return true
		}
	}
	return false
}

func intOr(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func durationMs(v, def int) time.Duration {
	if v == 0 {
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(v) * time.Millisecond
}

// Scan lists nearby BLE devices for the given window, strongest signal first.
func Scan(window time.Duration) ([]Device, error) {
	if err := enable(); err != nil {
		return nil, err
	}
	scanMu.Lock()
	defer scanMu.Unlock()

	found := map[string]Device{}
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		_ = adapter.Scan(func(_ *bluetooth.Adapter, r bluetooth.ScanResult) {
			addr := r.Address.String()
			mu.Lock()
			found[addr] = Device{ID: addr, Name: nameOr(r.LocalName(), "(no name)"), Address: addr, RSSI: int(r.RSSI)}
			mu.Unlock()
		})
		close(done)
	}()
	time.Sleep(window)
	_ = adapter.StopScan()
	<-done

	mu.Lock()
	defer mu.Unlock()
	list := make([]Device, 0, len(found))
	for _, d := range found {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].RSSI > list[j].RSSI })
	return list, nil
}

func matchDevice(r bluetooth.ScanResult, opts ConnectOptions) bool {
	addr := r.Address.String()
	if opts.ID != "" && strings.EqualFold(addr, opts.ID) {
		return true
	}
	if opts.Address != "" && strings.EqualFold(addr, opts.Address) {
		return true
	}
	if opts.NamePrefix != "" {
		return strings.HasPrefix(strings.ToUpper(r.LocalName()), strings.ToUpper(opts.NamePrefix))
	}
	return false
}

func findDevice(opts ConnectOptions, timeout time.Duration) (bluetooth.ScanResult, bool) {
	scanMu.Lock()
	defer scanMu.Unlock()

	var result bluetooth.ScanResult
	matched := false
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		_ = adapter.Scan(func(a *bluetooth.Adapter, r bluetooth.ScanResult) {
			if !matchDevice(r, opts) {
				return
			}
			mu.Lock()
			if !matched {
				result, matched = r, true
			}
			mu.Unlock()
			_ = a.StopScan()
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		_ = adapter.StopScan()
		<-done
	}
	mu.Lock()
	defer mu.Unlock()
	return result, matched
}

type charRef struct {
	serviceUUID string
	uuid        string
	props       []string
	char        bluetooth.DeviceCharacteristic
}

func pickByUUID(refs []charRef, uuid string) *charRef {
	if uuid == "" {
		return nil
	}
	for i := range refs {
		if uuidEq(refs[i].uuid, uuid) {
			return &refs[i]
		}
	}
	return nil
}

func pickByProp(refs []charRef, prop string) *charRef {
	for i := range refs {
		if hasProp(refs[i].props, prop) {
			return &refs[i]
		}
	}
	return nil
}

// pickWritableFallback is used only when properties aren't available (non-Linux):
// take the first characteristic outside the generic GAP/GATT services.
func pickWritableFallback(refs []charRef) *charRef {
	for i := range refs {
		s := shortUUID(refs[i].serviceUUID)
		if s == "1800" || s == "1801" {
			continue
		}
		return &refs[i]
	}
	if len(refs) > 0 {
		return &refs[0]
	}
	return nil
}

// Connect finds the device, connects over the native stack, and auto-picks the
// write/notify characteristics (pinned UUIDs win, else by property, else fallback).
func Connect(opts ConnectOptions) (*Conn, error) {
	if opts.NamePrefix == "" && opts.ID == "" && opts.Address == "" {
		return nil, errors.New("connect needs one of: namePrefix, id, address")
	}
	if err := enable(); err != nil {
		return nil, err
	}

	result, ok := findDevice(opts, durationMs(opts.ScanTimeoutMs, 8000))
	if !ok {
		return nil, errors.New("device not found while scanning; is it powered on and in range?")
	}
	name := nameOr(result.LocalName(), "")
	addr := result.Address.String()

	dev, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, err
	}

	services, err := dev.DiscoverServices(nil)
	if err != nil {
		_ = dev.Disconnect()
		return nil, err
	}

	flags := charFlags(addr) // Linux: uuid(lower) -> properties; nil elsewhere

	var refs []charRef
	var gatt []Service
	for _, s := range services {
		suid := s.UUID().String()
		chars, err := s.DiscoverCharacteristics(nil)
		if err != nil {
			continue
		}
		gs := Service{UUID: suid}
		for _, c := range chars {
			cu := c.UUID().String()
			props := flags[strings.ToLower(cu)]
			refs = append(refs, charRef{serviceUUID: suid, uuid: cu, props: props, char: c})
			gs.Characteristics = append(gs.Characteristics, Characteristic{UUID: cu, Properties: props})
		}
		gatt = append(gatt, gs)
	}

	writeRef := pickByUUID(refs, opts.WriteUUID)
	if writeRef == nil {
		writeRef = pickByProp(refs, "writeWithoutResponse")
	}
	if writeRef == nil {
		writeRef = pickByProp(refs, "write")
	}
	if writeRef == nil {
		writeRef = pickWritableFallback(refs)
	}
	if writeRef == nil {
		_ = dev.Disconnect()
		return nil, errors.New("no writable characteristic found; run `discover` to inspect the GATT table")
	}

	notifyRef := pickByUUID(refs, opts.NotifyUUID)
	if notifyRef == nil {
		notifyRef = pickByProp(refs, "notify")
	}
	if notifyRef == nil {
		notifyRef = pickByProp(refs, "indicate")
	}

	withoutResponse := hasProp(writeRef.props, "writeWithoutResponse")
	if len(writeRef.props) == 0 {
		withoutResponse = true // no flags (non-Linux): PUQU and most printers use write-without-response
	}

	c := &Conn{
		dev:              dev,
		name:             name,
		address:          addr,
		writeChar:        writeRef.char,
		writeServiceUUID: writeRef.serviceUUID,
		writeCharUUID:    writeRef.uuid,
		withoutResponse:  withoutResponse,
		gatt:             gatt,
		chunkSize:        intOr(opts.ChunkSize, 180),
		packetInterval:   durationMs(opts.PacketIntervalMs, 6),
	}

	if mtu, err := writeRef.char.GetMTU(); err == nil && mtu > 3 {
		m := int(mtu)
		c.mtu = &m
		if m-3 < c.chunkSize {
			c.chunkSize = m - 3
		}
	}

	if notifyRef != nil {
		nc := notifyRef.char
		uuid := notifyRef.uuid
		c.notifyChar = &nc
		c.notifyCharUUID = &uuid
		_ = nc.EnableNotifications(c.dispatch)
	} else {
		for i := range refs {
			if refs[i].uuid == writeRef.uuid {
				continue
			}
			nc := refs[i].char
			if err := nc.EnableNotifications(c.dispatch); err == nil {
				uuid := refs[i].uuid
				c.notifyChar = &nc
				c.notifyCharUUID = &uuid
				break
			}
		}
	}

	connections.Store(c.address, c)
	return c, nil
}

// Shutdown stops any in-flight scan. tinygo has no session to tear down.
func Shutdown() { _ = adapter.StopScan() }

// Conn is a live GATT link to one device: paced writes, notify fan-out, disconnect
// detection. It carries no protocol semantics.
type Conn struct {
	dev              bluetooth.Device
	name             string
	address          string
	writeChar        bluetooth.DeviceCharacteristic
	writeServiceUUID string
	writeCharUUID    string
	notifyChar       *bluetooth.DeviceCharacteristic
	notifyCharUUID   *string
	withoutResponse  bool
	gatt             []Service
	mtu              *int
	chunkSize        int
	packetInterval   time.Duration

	mu           sync.Mutex
	disconnected bool
	dataHandlers []func([]byte)
	discHandlers []func()
}

func (c *Conn) dispatch(buf []byte) {
	c.mu.Lock()
	handlers := append([]func([]byte){}, c.dataHandlers...)
	c.mu.Unlock()
	for _, h := range handlers {
		h(buf)
	}
}

// OnData registers a handler for notify frames.
func (c *Conn) OnData(cb func([]byte)) {
	c.mu.Lock()
	c.dataHandlers = append(c.dataHandlers, cb)
	c.mu.Unlock()
}

// OnDisconnect registers a handler fired once when the link drops.
func (c *Conn) OnDisconnect(cb func()) {
	c.mu.Lock()
	c.discHandlers = append(c.discHandlers, cb)
	c.mu.Unlock()
}

func (c *Conn) markDisconnected() {
	c.mu.Lock()
	if c.disconnected {
		c.mu.Unlock()
		return
	}
	c.disconnected = true
	connections.Delete(c.address)
	handlers := append([]func(){}, c.discHandlers...)
	c.mu.Unlock()
	for _, h := range handlers {
		h()
	}
}

// IsConnected reports whether the link is still up.
func (c *Conn) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.disconnected
}

func (c *Conn) displayName() string {
	if c.name != "" {
		return c.name
	}
	return c.address
}

// Gatt returns the discovered GATT table.
func (c *Conn) Gatt() []Service { return c.gatt }

// Info reports the connection and chosen endpoints.
func (c *Conn) Info() Info {
	return Info{
		Name:            c.displayName(),
		ID:              c.address,
		Address:         c.address,
		MTU:             c.mtu,
		WriteService:    c.writeServiceUUID,
		WriteChar:       c.writeCharUUID,
		NotifyChar:      c.notifyCharUUID,
		WithoutResponse: c.withoutResponse,
	}
}

// Write sends raw bytes, chunked to the MTU and paced to avoid overrunning the buffer.
func (c *Conn) Write(data []byte) error {
	if !c.IsConnected() {
		return errors.New("device is disconnected")
	}
	for off := 0; off < len(data); off += c.chunkSize {
		if !c.IsConnected() {
			return errors.New("device disconnected during write")
		}
		end := off + c.chunkSize
		if end > len(data) {
			end = len(data)
		}
		slice := data[off:end]
		var err error
		if c.withoutResponse {
			_, err = c.writeChar.WriteWithoutResponse(slice)
		} else {
			_, err = c.writeChar.Write(slice)
		}
		if err != nil {
			return c.handleWriteError(err)
		}
		if c.packetInterval > 0 {
			time.Sleep(c.packetInterval)
		}
	}
	return nil
}
func (c *Conn) handleWriteError(err error) error {
	if !isStaleGattError(err) {
		return err
	}
	c.markDisconnected()
	return fmt.Errorf("%w: %v", ErrStaleGatt, err)
}

// Disconnect closes the link.
func (c *Conn) Disconnect() error {
	c.markDisconnected()
	return c.dev.Disconnect()
}
