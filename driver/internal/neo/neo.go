// Package neo reads a u-blox NEO GNSS receiver over its I2C (DDC) port.
//
// The DDC port exposes a byte count at registers 0xFD/0xFE and the receiver's
// output stream at 0xFF. This package only reads that stream and parses NMEA;
// it never configures the receiver, so the UART/PPS outputs used for timekeeping
// are unaffected.
package neo

import (
	"bytes"
	"fmt"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

// Addr is the u-blox DDC I2C address.
const Addr = 0x42

const (
	regCount  = 0xFD
	regStream = 0xFF

	maxChunk = 255 // bytes per I2C read
	maxLine  = 120 // longer "sentences" are discarded as garbage
)

// Dev is a u-blox receiver on an I2C bus.
type Dev struct {
	bus   i2c.Dev
	sleep func(time.Duration)
	poll  time.Duration
	buf   []byte
}

// New wraps an I2C device bound to the receiver's address.
func New(bus i2c.Dev) *Dev {
	return &Dev{bus: bus, sleep: time.Sleep, poll: 100 * time.Millisecond}
}

// Available returns the number of bytes waiting in the receiver's buffer.
func (d *Dev) Available() (int, error) {
	var b [2]byte
	if err := i2c.ReadReg(d.bus, regCount, b[:]); err != nil {
		return 0, fmt.Errorf("neo: read count: %w", err)
	}
	n := int(b[0])<<8 | int(b[1])
	if n == 0xFFFF {
		return 0, nil // no valid count
	}
	return n, nil
}

// ReadStream drains up to max pending bytes from the output stream. It returns
// an empty slice when nothing is pending.
func (d *Dev) ReadStream(max int) ([]byte, error) {
	n, err := d.Available()
	if err != nil {
		return nil, err
	}
	n = min(n, max)
	out := make([]byte, 0, n)
	for len(out) < n {
		chunk := make([]byte, min(n-len(out), maxChunk))
		if err := i2c.ReadReg(d.bus, regStream, chunk); err != nil {
			return out, fmt.Errorf("neo: read stream: %w", err)
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// Next blocks until the next NMEA sentence with a valid checksum arrives.
// Bytes outside sentences, such as idle 0xFF fill or UBX frames, are skipped.
func (d *Dev) Next() (Sentence, error) {
	for {
		if s, ok := d.take(); ok {
			return s, nil
		}
		n, err := d.fill()
		if err != nil {
			return Sentence{}, err
		}
		if n == 0 {
			d.sleep(d.poll)
		}
	}
}

// Poll drains whatever the receiver has pending without blocking and returns
// the complete sentences it contained. A sentence split across polls is
// returned by a later call.
func (d *Dev) Poll() ([]Sentence, error) {
	_, err := d.fill()
	var out []Sentence
	for {
		s, ok := d.take()
		if !ok {
			return out, err
		}
		out = append(out, s)
	}
}

// fill appends pending stream bytes to the buffer.
func (d *Dev) fill() (int, error) {
	b, err := d.ReadStream(4096)
	d.buf = append(d.buf, b...)
	return len(b), err
}

// take extracts the first valid sentence from the buffer, discarding junk.
func (d *Dev) take() (Sentence, bool) {
	for {
		start := bytes.IndexByte(d.buf, '$')
		if start < 0 {
			d.buf = d.buf[:0]
			return Sentence{}, false
		}
		d.buf = d.buf[start:]
		end := bytes.IndexByte(d.buf, '\n')
		if end < 0 {
			if len(d.buf) > maxLine {
				d.buf = d.buf[1:] // runaway; resync on the next '$'
				continue
			}
			return Sentence{}, false
		}
		line := string(bytes.TrimRight(d.buf[:end], "\r"))
		d.buf = d.buf[end+1:]
		if s, err := ParseSentence(line); err == nil {
			return s, true
		}
	}
}
