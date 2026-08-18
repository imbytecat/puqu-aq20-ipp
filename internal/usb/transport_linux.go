//go:build linux

package usb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/imbytecat/puqu-ipp-bridge/internal/printer"
)

const (
	interfaceNumber = 2
	bulkOutEndpoint = 0x02
	bulkInEndpoint  = 0x82
	bulkChunkSize   = 512
)

const disconnectClaimIfDriver = 0x01

type discoveredDevice struct {
	Device
	path string
}

type bulkTransfer struct {
	Endpoint uint32
	Length   uint32
	Timeout  uint32
	Data     uintptr
}

type disconnectClaim struct {
	Interface uint32
	Flags     uint32
	Driver    [256]byte
}

type ioctlCaller func(fd, request uintptr, data unsafe.Pointer) error

var (
	claimInterfaceRequest   = ioctlRequest(ioctlRead, 'U', 15, unsafe.Sizeof(uint32(0)))
	releaseInterfaceRequest = ioctlRequest(ioctlRead, 'U', 16, unsafe.Sizeof(uint32(0)))
	disconnectClaimRequest  = ioctlRequest(ioctlRead, 'U', 27, unsafe.Sizeof(disconnectClaim{}))
	bulkRequest             = ioctlRequest(ioctlRead|ioctlWrite, 'U', 2, unsafe.Sizeof(bulkTransfer{}))
)

const (
	ioctlWrite = 1
	ioctlRead  = 2
)

func ioctlRequest(direction, kind, number, size uintptr) uintptr {
	return direction<<30 | size<<16 | kind<<8 | number
}

func claimPrinterInterface(fd uintptr, call ioctlCaller) error {
	iface := uint32(interfaceNumber)
	err := call(fd, claimInterfaceRequest, unsafe.Pointer(&iface))
	if err == nil || !errors.Is(err, unix.EBUSY) {
		return err
	}
	claim := disconnectClaim{Interface: interfaceNumber, Flags: disconnectClaimIfDriver}
	copy(claim.Driver[:], "usblp")
	return call(fd, disconnectClaimRequest, unsafe.Pointer(&claim))
}

func Scan(ctx context.Context) ([]Device, error) {
	found, err := scan(ctx, "/sys/bus/usb/devices")
	if err != nil {
		return nil, err
	}
	devices := make([]Device, len(found))
	for i := range found {
		devices[i] = found[i].Device
	}
	return devices, nil
}

func scan(ctx context.Context, root string) ([]discoveredDevice, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var devices []discoveredDevice
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir := filepath.Join(root, entry.Name())
		vendor, err := readHex(filepath.Join(dir, "idVendor"))
		if err != nil || vendor != VendorID {
			continue
		}
		product, err := readHex(filepath.Join(dir, "idProduct"))
		if err != nil || product != ProductID {
			continue
		}
		bus, err := readTrimmed(filepath.Join(dir, "busnum"))
		if err != nil {
			continue
		}
		dev, err := readTrimmed(filepath.Join(dir, "devnum"))
		if err != nil {
			continue
		}
		serial, _ := readTrimmed(filepath.Join(dir, "serial"))
		id := serial
		if id == "" {
			id = "port:" + entry.Name()
		}
		name, _ := readTrimmed(filepath.Join(dir, "product"))
		if name == "" {
			name = "PUQU Label Printer"
		}
		busNumber, err := strconv.Atoi(bus)
		if err != nil {
			continue
		}
		deviceNumber, err := strconv.Atoi(dev)
		if err != nil {
			continue
		}
		path := fmt.Sprintf("/dev/bus/usb/%03d/%03d", busNumber, deviceNumber)
		devices = append(devices, discoveredDevice{
			Device: Device{ID: id, Name: name, Address: path},
			path:   path,
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	return devices, nil
}

func readHex(path string) (int, error) {
	value, err := readTrimmed(path)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(value, 16, 16)
	return int(parsed), err
}

func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func Connect(opts ConnectOptions) (*Conn, error) {
	if strings.TrimSpace(opts.ID) == "" {
		return nil, errors.New("USB connection needs a device id")
	}
	devices, err := scan(context.Background(), "/sys/bus/usb/devices")
	if err != nil {
		return nil, err
	}
	var selected *discoveredDevice
	for i := range devices {
		if devices[i].ID == opts.ID {
			selected = &devices[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("USB printer %q not found", opts.ID)
	}
	file, err := os.OpenFile(selected.path, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("open %s: permission denied; run the service as root or grant udev access to USB 8888:0026", selected.path)
		}
		return nil, err
	}
	if err := claimPrinterInterface(file.Fd(), ioctl); err != nil {
		file.Close()
		return nil, fmt.Errorf("claim USB printer interface %d: %w", interfaceNumber, err)
	}
	mtu := 64
	conn := &Conn{
		file: file,
		info: printer.LinkInfo{
			Transport: "usb", Name: selected.Name, ID: selected.ID, Address: selected.Address, MTU: &mtu,
		},
		done: make(chan struct{}),
	}
	conn.connected.Store(true)
	go conn.monitorLoop()
	return conn, nil
}

type Conn struct {
	file *os.File
	info printer.LinkInfo
	done chan struct{}

	connected atomic.Bool
	closeOnce sync.Once
	writeMu   sync.Mutex
	handlerMu sync.Mutex
	handlers  []func()
}

func (c *Conn) Write(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if !c.IsConnected() {
		return errors.New("USB printer is disconnected")
	}
	for offset := 0; offset < len(data); offset += bulkChunkSize {
		end := min(offset+bulkChunkSize, len(data))
		written, err := bulk(c.file.Fd(), bulkOutEndpoint, data[offset:end], 3000)
		if err != nil {
			if isDisconnectError(err) {
				c.disconnect()
			}
			return fmt.Errorf("USB bulk write: %w", err)
		}
		if written != end-offset {
			return io.ErrShortWrite
		}
		if end < len(data) {
			time.Sleep(4 * time.Millisecond)
		}
	}
	if err := c.readReplyLocked(); err != nil {
		c.disconnect()
		return fmt.Errorf("USB bulk reply: %w", err)
	}
	return nil
}

func (c *Conn) OnDisconnect(cb func()) {
	c.handlerMu.Lock()
	if c.IsConnected() {
		c.handlers = append(c.handlers, cb)
		c.handlerMu.Unlock()
		return
	}
	c.handlerMu.Unlock()
	cb()
}

func (c *Conn) Info() printer.LinkInfo { return c.info }
func (c *Conn) IsConnected() bool      { return c.connected.Load() }
func (c *Conn) Disconnect() error      { c.disconnect(); return nil }

func (c *Conn) monitorLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	err := monitor(c.done, ticker.C, func() error {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		if !c.IsConnected() {
			return nil
		}
		return c.readReplyLocked()
	})
	if err != nil {
		c.disconnect()
	}
}

func (c *Conn) readReplyLocked() error {
	var buffer [64]byte
	_, err := bulk(c.file.Fd(), bulkInEndpoint, buffer[:], 500)
	if errors.Is(err, unix.ETIMEDOUT) || errors.Is(err, unix.EINTR) {
		return nil
	}
	return err
}

func monitor(done <-chan struct{}, ticks <-chan time.Time, check func() error) error {
	for {
		select {
		case <-done:
			return nil
		case <-ticks:
			if err := check(); err != nil {
				return err
			}
		}
	}
}

func (c *Conn) disconnect() {
	c.closeOnce.Do(func() {
		c.connected.Store(false)
		close(c.done)
		iface := uint32(interfaceNumber)
		_ = ioctl(c.file.Fd(), releaseInterfaceRequest, unsafe.Pointer(&iface))
		_ = c.file.Close()
		c.handlerMu.Lock()
		handlers := append([]func(){}, c.handlers...)
		c.handlers = nil
		c.handlerMu.Unlock()
		for _, handler := range handlers {
			handler()
		}
	})
}

func bulk(fd uintptr, endpoint uint32, data []byte, timeout uint32) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	transfer := bulkTransfer{
		Endpoint: endpoint,
		Length:   uint32(len(data)),
		Timeout:  timeout,
		Data:     uintptr(unsafe.Pointer(unsafe.SliceData(data))),
	}
	result, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, bulkRequest, uintptr(unsafe.Pointer(&transfer)))
	runtime.KeepAlive(data)
	if errno != 0 {
		return 0, errno
	}
	return int(result), nil
}

func ioctl(fd, request uintptr, data unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(data))
	if errno != 0 {
		return errno
	}
	return nil
}

func isDisconnectError(err error) bool {
	return errors.Is(err, unix.ENODEV) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESHUTDOWN) || errors.Is(err, unix.EBADF)
}
