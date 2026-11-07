package pimountgps

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/bmp580"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/neo"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/sht45"
)

// Open connects to every subsystem on the board. See OpenSubsystems.
func Open(cfg Config) (board *Board, warnings []error, err error) {
	return OpenSubsystems(cfg, SubsystemsAll)
}

// OpenSubsystems connects to the named subsystems and starts the background
// loop. Each chip is opened best-effort: one that fails is reported in
// warnings and its Board methods behave as absent. It returns an error for invalid configuration or if
// nothing requested came up. Open only succeeds on Linux.
func OpenSubsystems(cfg Config, subs Subsystem) (board *Board, warnings []error, err error) {
	if math.IsNaN(cfg.EnclosureHeatingC) || math.IsInf(cfg.EnclosureHeatingC, 0) || cfg.EnclosureHeatingC < 0 {
		return nil, nil, errors.New("enclosure heating must be a finite, non-negative temperature in Celsius")
	}
	var dev devices
	var closers []io.Closer
	open := func(addr uint16) i2c.Dev {
		d, err := i2c.Open(cfg.Bus, addr)
		if err != nil {
			warnings = append(warnings, err)
			return nil
		}
		closers = append(closers, d)
		return d
	}

	if subs&SubsystemWeather != 0 {
		if d := open(cfg.SHT45Addr); d != nil {
			s := sht45.New(d)
			// Probe with a read so an absent chip is reported now, not later.
			if _, err := s.Serial(); err != nil {
				warnings = append(warnings, fmt.Errorf("sht45: %w", err))
			} else {
				dev.sht = s
			}
		}
		if d := open(cfg.BMP580Addr); d != nil {
			reopen := func() (*bmp580.Dev, error) {
				return bmp580.New(d, cfg.TempOversampling, cfg.PressOversampling)
			}
			if b, err := reopen(); err != nil {
				warnings = append(warnings, err)
			} else {
				dev.bmp, dev.reopenBMP = b, reopen
			}
		}
	}
	if subs&SubsystemGPS != 0 {
		if d := open(cfg.GPSAddr); d != nil {
			g := neo.New(d)
			if _, err := g.Available(); err != nil {
				warnings = append(warnings, err)
			} else {
				dev.gps = g
			}
		}
	}

	if dev.sht == nil && dev.bmp == nil && dev.gps == nil {
		for _, c := range closers {
			c.Close()
		}
		return nil, warnings, errors.Join(append([]error{ErrNoDevice}, warnings...)...)
	}
	b := newBoard(cfg, dev)
	b.closers = closers
	b.start()
	return b, warnings, nil
}
