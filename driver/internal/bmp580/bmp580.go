// Package bmp580 drives the Bosch BMP580 barometric pressure sensor.
//
// The driver uses forced mode: each Measure triggers one conversion, waits for
// data-ready, and reads temperature and pressure in a single burst.
package bmp580

import (
	"errors"
	"fmt"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

// Addr is the BMP580 I2C address with SDO tied high.
const Addr = 0x46

const (
	regChipID    = 0x01
	regIntSource = 0x15
	regTempXLSB  = 0x1D // temp XLSB..MSB, then pressure XLSB..MSB
	regIntStatus = 0x27
	regStatus    = 0x28
	regOSRConfig = 0x36
	regODRConfig = 0x37
	regCmd       = 0x7E

	chipID       = 0x50
	cmdSoftReset = 0xB6

	statusNVMReady = 1 << 1
	statusNVMErr   = 1 << 2
	intDataReady   = 1 << 0
	osrPressEn     = 1 << 6
	odrDeepDis     = 1 << 7
	odrModeMask    = 0x03
	modeForced     = 0x02
)

// Oversampling is a 2^n oversampling rate for one channel.
type Oversampling byte

const (
	X1 Oversampling = iota
	X2
	X4
	X8
	X16
	X32
	X64
	X128
)

var (
	// ErrChipID is returned when the chip-ID register is not a BMP580's.
	ErrChipID = errors.New("bmp580: unexpected chip ID")
	// ErrNVM is returned when the chip reports its NVM is not ready or failed.
	ErrNVM = errors.New("bmp580: NVM not ready")
	// ErrTimeout is returned when a conversion does not complete.
	ErrTimeout = errors.New("bmp580: measurement timed out")
)

// Reading is one measurement.
type Reading struct {
	TempC      float64 // °C
	PressurePa float64 // Pa
}

// Dev is a BMP580 on an I2C bus.
type Dev struct {
	bus     i2c.Dev
	sleep   func(time.Duration)
	timeout time.Duration
}

// New wraps an I2C device bound to the BMP580's address and checks that the
// chip is present and ready. It enables the data-ready status flag and sets
// oversampling to the given rates.
func New(bus i2c.Dev, osrTemp, osrPress Oversampling) (*Dev, error) {
	d := &Dev{bus: bus, sleep: time.Sleep, timeout: 500 * time.Millisecond}
	if err := d.init(osrTemp, osrPress); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Dev) init(osrTemp, osrPress Oversampling) error {
	id, err := d.reg(regChipID)
	if err != nil {
		return err
	}
	if id != chipID {
		return fmt.Errorf("%w: 0x%02X", ErrChipID, id)
	}
	st, err := d.reg(regStatus)
	if err != nil {
		return err
	}
	if st&statusNVMReady == 0 || st&statusNVMErr != 0 {
		return fmt.Errorf("%w: status 0x%02X", ErrNVM, st)
	}
	// Reading INT_STATUS clears the power-on flag.
	if _, err := d.reg(regIntStatus); err != nil {
		return err
	}
	if err := d.setReg(regIntSource, intDataReady); err != nil {
		return err
	}
	osr := osrPressEn | byte(osrPress&7)<<3 | byte(osrTemp&7)
	return d.setReg(regOSRConfig, osr)
}

// Measure triggers one forced-mode conversion and returns the result.
func (d *Dev) Measure() (Reading, error) {
	odr, err := d.reg(regODRConfig)
	if err != nil {
		return Reading{}, err
	}
	if err := d.setReg(regODRConfig, odr&^odrModeMask|odrDeepDis|modeForced); err != nil {
		return Reading{}, err
	}
	deadline := time.Now().Add(d.timeout)
	for {
		st, err := d.reg(regIntStatus)
		if err != nil {
			return Reading{}, err
		}
		if st&intDataReady != 0 {
			break
		}
		if time.Now().After(deadline) {
			return Reading{}, ErrTimeout
		}
		d.sleep(2 * time.Millisecond)
	}
	var b [6]byte
	if err := i2c.ReadReg(d.bus, regTempXLSB, b[:]); err != nil {
		return Reading{}, fmt.Errorf("bmp580: read data: %w", err)
	}
	rawT := int32(uint32(b[2])<<24|uint32(b[1])<<16|uint32(b[0])<<8) >> 8 // sign-extend 24 bits
	rawP := uint32(b[5])<<16 | uint32(b[4])<<8 | uint32(b[3])
	return Reading{
		TempC:      float64(rawT) / 65536,
		PressurePa: float64(rawP) / 64,
	}, nil
}

// Reset issues a soft reset; call New again afterwards.
func (d *Dev) Reset() error {
	if err := d.setReg(regCmd, cmdSoftReset); err != nil {
		return err
	}
	d.sleep(2 * time.Millisecond)
	return nil
}

func (d *Dev) reg(r byte) (byte, error) {
	var b [1]byte
	if err := i2c.ReadReg(d.bus, r, b[:]); err != nil {
		return 0, fmt.Errorf("bmp580: read reg 0x%02X: %w", r, err)
	}
	return b[0], nil
}

func (d *Dev) setReg(r, v byte) error {
	if err := i2c.WriteReg(d.bus, r, v); err != nil {
		return fmt.Errorf("bmp580: write reg 0x%02X: %w", r, err)
	}
	return nil
}
