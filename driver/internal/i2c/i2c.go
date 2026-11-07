// Package i2c is the bus seam shared by the board's I2C chip drivers. Drivers
// talk to a Dev, never to a device node directly, so they are unit-testable
// over the in-memory Fake.
package i2c

import "errors"

// ErrUnsupported is returned by Open on a GOOS without a real backend.
var ErrUnsupported = errors.New("i2c: hardware access is only supported on linux")

// Dev is a byte-level channel to one 7-bit I2C address. The address is bound
// at open time, so reads and writes carry only the chip's payload bytes.
type Dev interface {
	// Write sends p to the device (a command, or a register byte plus data).
	Write(p []byte) (int, error)
	// Read reads len(p) bytes from the device.
	Read(p []byte) (int, error)
	// Tx writes w then reads into r as one transaction, joined by a repeated
	// start instead of a stop.
	Tx(w, r []byte) error
	Close() error
}

// ReadReg reads len(p) bytes starting at register reg.
func ReadReg(d Dev, reg byte, p []byte) error {
	return d.Tx([]byte{reg}, p)
}

// WriteReg writes data starting at register reg.
func WriteReg(d Dev, reg byte, data ...byte) error {
	_, err := d.Write(append([]byte{reg}, data...))
	return err
}
