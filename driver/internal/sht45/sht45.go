// Package sht45 drives the Sensirion SHT45 temperature/humidity sensor.
//
// The SHT45 has no registers: each operation is a one-byte command, followed
// after a fixed delay by a read of 16-bit words, each with a CRC-8.
package sht45

import (
	"errors"
	"fmt"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

// Addr is the SHT45-AD1B I2C address.
const Addr = 0x44

const (
	cmdMeasureHigh   = 0xFD
	cmdMeasureMedium = 0xF6
	cmdMeasureLow    = 0xE0
	cmdSerial        = 0x89
	cmdSoftReset     = 0x94
)

// Precision selects measurement repeatability; higher takes longer.
type Precision int

const (
	High Precision = iota
	Medium
	Low
)

// ErrCRC is returned when a word fails its checksum.
var ErrCRC = errors.New("sht45: CRC mismatch")

// Reading is one measurement.
type Reading struct {
	TempC    float64 // °C
	Humidity float64 // %RH, clamped to 0..100
}

// Dev is an SHT45 on an I2C bus.
type Dev struct {
	bus   i2c.Dev
	sleep func(time.Duration)
}

// New wraps an I2C device already bound to the SHT45's address.
func New(bus i2c.Dev) *Dev { return &Dev{bus: bus, sleep: time.Sleep} }

// Measure performs one temperature and humidity measurement.
func (d *Dev) Measure(p Precision) (Reading, error) {
	cmd, wait := byte(cmdMeasureHigh), 10*time.Millisecond
	switch p {
	case Medium:
		cmd, wait = cmdMeasureMedium, 5*time.Millisecond
	case Low:
		cmd, wait = cmdMeasureLow, 2*time.Millisecond
	}
	w, err := d.command(cmd, wait)
	if err != nil {
		return Reading{}, err
	}
	rh := -6 + 125*float64(w[1])/65535
	return Reading{
		TempC:    -45 + 175*float64(w[0])/65535,
		Humidity: min(max(rh, 0), 100),
	}, nil
}

// Serial returns the chip's 32-bit serial number.
func (d *Dev) Serial() (uint32, error) {
	w, err := d.command(cmdSerial, time.Millisecond)
	if err != nil {
		return 0, err
	}
	return uint32(w[0])<<16 | uint32(w[1]), nil
}

// Reset issues a soft reset.
func (d *Dev) Reset() error {
	if _, err := d.bus.Write([]byte{cmdSoftReset}); err != nil {
		return fmt.Errorf("sht45: reset: %w", err)
	}
	d.sleep(time.Millisecond)
	return nil
}

// command sends cmd, waits, and reads back two CRC-checked words.
func (d *Dev) command(cmd byte, wait time.Duration) ([2]uint16, error) {
	var w [2]uint16
	if _, err := d.bus.Write([]byte{cmd}); err != nil {
		return w, fmt.Errorf("sht45: command 0x%02X: %w", cmd, err)
	}
	d.sleep(wait)
	var b [6]byte
	if _, err := d.bus.Read(b[:]); err != nil {
		return w, fmt.Errorf("sht45: read 0x%02X: %w", cmd, err)
	}
	for i := range w {
		word := b[i*3 : i*3+2]
		if crc8(word) != b[i*3+2] {
			return w, ErrCRC
		}
		w[i] = uint16(word[0])<<8 | uint16(word[1])
	}
	return w, nil
}

// crc8 is Sensirion's CRC-8: polynomial 0x31, init 0xFF.
func crc8(data []byte) byte {
	crc := byte(0xFF)
	for _, b := range data {
		crc ^= b
		for range 8 {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x31
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
