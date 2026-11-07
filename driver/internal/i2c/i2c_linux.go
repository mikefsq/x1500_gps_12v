//go:build linux

package i2c

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// ioctl requests and flags from linux/i2c-dev.h and linux/i2c.h.
const (
	i2cSlave = 0x0703
	i2cRdwr  = 0x0707
	i2cMRd   = 0x0001
)

// i2cMsg mirrors struct i2c_msg.
type i2cMsg struct {
	addr  uint16
	flags uint16
	len   uint16
	buf   uintptr
}

// i2cRdwrData mirrors struct i2c_rdwr_ioctl_data.
type i2cRdwrData struct {
	msgs  uintptr
	nmsgs uint32
}

type linuxDev struct {
	f    *os.File
	addr uint16
}

// Open opens bus (e.g. "/dev/i2c-1") and binds it to the 7-bit address addr.
func Open(bus string, addr uint16) (Dev, error) {
	f, err := os.OpenFile(bus, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", bus, err)
	}
	if err := ioctl(f, i2cSlave, uintptr(addr)); err != nil {
		f.Close()
		return nil, fmt.Errorf("i2c set slave 0x%02x on %s: %w", addr, bus, err)
	}
	return &linuxDev{f: f, addr: addr}, nil
}

func (d *linuxDev) Write(p []byte) (int, error) { return d.f.Write(p) }
func (d *linuxDev) Read(p []byte) (int, error)  { return d.f.Read(p) }
func (d *linuxDev) Close() error                { return d.f.Close() }

func (d *linuxDev) Tx(w, r []byte) error {
	if len(w) == 0 || len(r) == 0 {
		return fmt.Errorf("i2c tx 0x%02x: empty buffer", d.addr)
	}
	msgs := [2]i2cMsg{
		{addr: d.addr, len: uint16(len(w)), buf: uintptr(unsafe.Pointer(&w[0]))},
		{addr: d.addr, flags: i2cMRd, len: uint16(len(r)), buf: uintptr(unsafe.Pointer(&r[0]))},
	}
	data := i2cRdwrData{msgs: uintptr(unsafe.Pointer(&msgs[0])), nmsgs: 2}
	err := ioctl(d.f, i2cRdwr, uintptr(unsafe.Pointer(&data)))
	runtime.KeepAlive(w)
	runtime.KeepAlive(r)
	runtime.KeepAlive(&msgs)
	if err != nil {
		return fmt.Errorf("i2c tx 0x%02x: %w", d.addr, err)
	}
	return nil
}

func ioctl(f *os.File, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, arg); errno != 0 {
		return errno
	}
	return nil
}
