// Package pimountgps is a Go driver for the pi_mount_gps CM5 board: with an I2c
// SHT45 temperature/humidity sensor, a BMP580 barometer, and a u-blox GNSS
// receiver. A background loop reads
// the sensors and drains the receiver, so Weather and Fix return instantly from
// cache — the shape an ASCOM Alpaca front-end polls.
package pimountgps

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/bmp580"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/neo"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/sht45"
)

// ErrNoDevice is returned when a subsystem's chip was not opened.
var ErrNoDevice = errors.New("pimountgps: subsystem not available")

// Fix is the receiver state assembled from its RMC, GGA and GSA sentences.
// u-blox sends GSA after GGA, so FixType and the dilutions in a published fix
// are from the previous epoch.
type Fix = neo.Fix

// Weather is the ambient conditions read from the board's sensors.
type Weather struct {
	Temperature     float64 // °C, SHT45 (BMP580 when there is no SHT45)
	Humidity        float64 // % relative, SHT45
	Dewpoint        float64 // °C, from raw SHT45 pair; preserved by heating compensation
	Pressure        float64 // hPa, BMP580 (station pressure, not sea level)
	BaroTemperature float64 // °C, BMP580
}

// devices is the set of chip drivers a Board operates; any may be nil.
type devices struct {
	sht *sht45.Dev
	bmp *bmp580.Dev
	gps *neo.Dev
	// reopenBMP re-initialises the BMP580 after a failure (e.g. a brownout
	// that reset its configuration). Nil when it cannot be reopened.
	reopenBMP func() (*bmp580.Dev, error)
}

// Board is a pi_mount_gps board.
type Board struct {
	cfg     Config
	now     func() time.Time
	closers []io.Closer

	sensorMu sync.Mutex // serialises SHT45/BMP580 transactions; guards dev.bmp
	dev      devices

	// Which chips are present
	hasSHT, hasBMP, hasGPS bool

	mu                         sync.Mutex
	rawSHTTemp, rawSHTHumidity float64
	weather                    Weather
	shtAt                      time.Time // last good SHT45 read
	bmpAt                      time.Time // last good BMP580 read
	weatherErr                 error
	fix                        Fix
	fixAt                      time.Time
	gpsErr                     error

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newBoard(cfg Config, dev devices) *Board {
	return &Board{
		cfg: cfg, dev: dev, now: time.Now,
		hasSHT: dev.sht != nil, hasBMP: dev.bmp != nil, hasGPS: dev.gps != nil,
	}
}

// start launches the background loop. Open calls it; tests drive Refresh and
// pollGPS directly instead.
func (b *Board) start() {
	b.stop = make(chan struct{})
	b.done = make(chan struct{})
	go b.loop()
}

func (b *Board) loop() {
	defer close(b.done)
	var weatherC <-chan time.Time
	if b.hasWeather() {
		b.Refresh()
		t := time.NewTicker(b.cfg.WeatherInterval)
		defer t.Stop()
		weatherC = t.C
	}
	var gpsC <-chan time.Time
	var epoch Fix // RMC/GGA accumulator, owned by this goroutine
	if b.hasGPS {
		t := time.NewTicker(b.cfg.GPSInterval)
		defer t.Stop()
		gpsC = t.C
	}
	for {
		select {
		case <-b.stop:
			return
		case <-weatherC:
			b.Refresh()
		case <-gpsC:
			b.pollGPS(&epoch)
		}
	}
}

// Close stops the background loop and releases the bus handles.
func (b *Board) Close() error {
	var first error
	b.closeOnce.Do(func() {
		if b.stop != nil {
			close(b.stop)
			<-b.done
		}
		for _, c := range b.closers {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	})
	return first
}

func (b *Board) hasWeather() bool { return b.hasSHT || b.hasBMP }

// HasHumidity reports whether the SHT45 was opened, so Weather's Humidity and
// Dewpoint are measurements rather than zero placeholders.
func (b *Board) HasHumidity() bool { return b.hasSHT }

// HasPressure reports whether the BMP580 was opened, so Weather's Pressure is a
// measurement rather than a zero placeholder.
func (b *Board) HasPressure() bool { return b.hasBMP }

// ===== Weather =====

// Refresh reads the SHT45 and BMP580 now and updates the cached Weather. The
// background loop calls it every Config.WeatherInterval; call it directly for a
// guaranteed-fresh sample (ASCOM ObservingConditions.Refresh). A sensor that
// fails keeps its previous values, and its age keeps growing.
func (b *Board) Refresh() error {
	if !b.hasWeather() {
		return ErrNoDevice
	}
	b.sensorMu.Lock()
	defer b.sensorMu.Unlock()

	var errs []error
	var sht sht45.Reading
	var bmp bmp580.Reading
	shtOK, bmpOK := false, false
	if b.dev.sht != nil {
		r, err := b.dev.sht.Measure(b.cfg.Precision)
		if err != nil {
			errs = append(errs, err)
		} else {
			if r.TempC-b.cfg.EnclosureHeatingC <= -243.12 {
				errs = append(errs, fmt.Errorf("enclosure heating correction exceeds humidity formula range"))
			} else {
				sht, shtOK = r, true
			}
		}
	}
	if b.dev.bmp != nil {
		r, err := b.dev.bmp.Measure()
		if err != nil {
			errs = append(errs, err)
			if b.dev.reopenBMP != nil {
				if d, rerr := b.dev.reopenBMP(); rerr == nil {
					b.dev.bmp = d
				}
			}
		} else {
			bmp, bmpOK = r, true
		}
	}
	err := errors.Join(errs...)

	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	w := &b.weather
	if shtOK {
		b.rawSHTTemp, b.rawSHTHumidity = sht.TempC, sht.Humidity
		b.compensateWeather()
		b.shtAt = now
	}
	if bmpOK {
		w.Pressure = bmp.PressurePa / 100
		w.BaroTemperature = bmp.TempC
		if !b.hasSHT {
			w.Temperature = bmp.TempC - b.cfg.EnclosureHeatingC
		}
		b.bmpAt = now
	}
	b.weatherErr = err
	return err
}

// Weather returns the last conditions read.
func (b *Board) Weather() Weather {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.weather
}

// WeatherErr returns the error from the most recent Refresh, or nil.
func (b *Board) WeatherErr() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.weatherErr
}

// WeatherAge reports how old the cached conditions are: the age of the older
// of the two sensors' last good reads, so one failing sensor shows as aging.
// ok is false if any opened sensor has never been read.
func (b *Board) WeatherAge() (age time.Duration, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.hasWeather() {
		return 0, false
	}
	now := b.now()
	for _, s := range []struct {
		present bool
		at      time.Time
	}{{b.hasSHT, b.shtAt}, {b.hasBMP, b.bmpAt}} {
		if !s.present {
			continue
		}
		if s.at.IsZero() {
			return 0, false
		}
		age = max(age, now.Sub(s.at))
	}
	return age, true
}

// WeatherStale reports whether the conditions are absent or older than
// Config.WeatherMaxAge. A front-end should surface this rather than serve
// old values as current.
func (b *Board) WeatherStale() bool {
	age, ok := b.WeatherAge()
	if !ok {
		return true
	}
	return b.cfg.WeatherMaxAge > 0 && age > b.cfg.WeatherMaxAge
}

// DewMargin returns temperature − dew point (°C).
func (b *Board) DewMargin() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.weather.Temperature - b.weather.Dewpoint
}

// Dewpoint estimates the dew point (°C) from temperature (°C) and relative
// humidity (%) using the Magnus formula.
func Dewpoint(tempC, humidity float64) float64 {
	const a, b = 17.62, 243.12
	if humidity <= 0 {
		humidity = 0.001
	}
	gamma := math.Log(humidity/100) + a*tempC/(b+tempC)
	return b * gamma / (a - gamma)
}

// ===== GPS =====

// pollGPS drains the receiver and publishes a fix at the end of each epoch.
// u-blox emits RMC before GGA, so a GGA completes the epoch.
func (b *Board) pollGPS(epoch *Fix) {
	ss, err := b.dev.gps.Poll()
	for _, s := range ss {
		if _, uerr := epoch.Update(s); uerr != nil || s.Type != "GGA" || epoch.Time.IsZero() {
			continue
		}
		b.mu.Lock()
		b.fix = *epoch
		b.fixAt = b.now()
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.gpsErr = err
	b.mu.Unlock()
}

// Fix returns the latest receiver fix. Check FixStale before trusting it.
func (b *Board) Fix() (Fix, error) {
	if !b.hasGPS {
		return Fix{}, ErrNoDevice
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fix, nil
}

// GPSErr returns the error from the most recent receiver poll, or nil.
func (b *Board) GPSErr() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gpsErr
}

// FixAge reports how long ago the last fix was published. ok is false if none
// has been.
func (b *Board) FixAge() (age time.Duration, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fixAt.IsZero() {
		return 0, false
	}
	return b.now().Sub(b.fixAt), true
}

// FixStale reports whether there is no usable position: no fix yet, one older
// than Config.FixMaxAge, or the receiver reporting no valid position.
func (b *Board) FixStale() bool {
	age, ok := b.FixAge()
	if !ok || (b.cfg.FixMaxAge > 0 && age > b.cfg.FixMaxAge) {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.fix.Valid || b.fix.Quality == 0
}

// SetEnclosureHeating changes the correction and immediately updates cached
// weather from the original readings, without waiting for or forcing an I2C read.
func (b *Board) SetEnclosureHeating(heat float64) error {
	if math.IsNaN(heat) || math.IsInf(heat, 0) || heat < 0 {
		return errors.New("enclosure heating must be finite and non-negative")
	}
	b.sensorMu.Lock()
	defer b.sensorMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hasSHT && !b.shtAt.IsZero() && b.rawSHTTemp-heat <= -243.12 {
		return errors.New("enclosure heating correction exceeds humidity formula range")
	}
	b.cfg.EnclosureHeatingC = heat
	if b.hasSHT && !b.shtAt.IsZero() {
		b.compensateWeather()
	}
	if !b.hasSHT && !b.bmpAt.IsZero() {
		b.weather.Temperature = b.weather.BaroTemperature - heat
	}
	return nil
}

// compensateWeather requires mu and uses the uncorrected SHT pair.
func (b *Board) compensateWeather() {
	w := &b.weather
	t, rh := b.rawSHTTemp, b.rawSHTHumidity
	w.Temperature = t - b.cfg.EnclosureHeatingC
	w.Humidity = rh
	if b.cfg.EnclosureHeatingC != 0 {
		const a, c = 17.62, 243.12
		w.Humidity = min(100, rh*math.Exp(a*t/(c+t)-a*w.Temperature/(c+w.Temperature)))
	}
	w.Dewpoint = Dewpoint(t, rh)
}
