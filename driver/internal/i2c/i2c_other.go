//go:build !linux

package i2c

// Open always fails off linux.
func Open(bus string, addr uint16) (Dev, error) { return nil, ErrUnsupported }
