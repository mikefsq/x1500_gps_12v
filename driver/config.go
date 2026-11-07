package pimountgps

import (
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/bmp580"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/neo"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/sht45"
)

// Precision is the SHT45 measurement repeatability.
type Precision = sht45.Precision

const (
	PrecisionHigh   = sht45.High
	PrecisionMedium = sht45.Medium
	PrecisionLow    = sht45.Low
)

// Oversampling is a BMP580 2^n oversampling rate (0 = x1 … 7 = x128).
type Oversampling = bmp580.Oversampling

// Subsystem selects which parts of the board OpenSubsystems brings up, so
// separate Alpaca devices can each open only what they serve.
type Subsystem int

const (
	SubsystemWeather Subsystem = 1 << iota // SHT45 + BMP580
	SubsystemGPS                           // u-blox receiver
	SubsystemsAll    = SubsystemWeather | SubsystemGPS
)

// Config is the board wiring and polling behaviour.
type Config struct {
	// EnclosureHeatingC is heat added by the enclosure in degrees Celsius.
	// Positive values are subtracted from ambient Temperature, with SHT45 RH
	// adjusted at constant vapor pressure. Zero disables compensation.
	// BaroTemperature remains the raw BMP580 chip temperature.
	EnclosureHeatingC float64

	Bus        string // I2C bus device
	SHT45Addr  uint16
	BMP580Addr uint16
	GPSAddr    uint16

	Precision         Precision    // SHT45 repeatability
	TempOversampling  Oversampling // BMP580 temperature
	PressOversampling Oversampling // BMP580 pressure

	// WeatherInterval is how often the background loop reads the SHT45 and
	// BMP580. GPSInterval is how often it drains the receiver; keep it under
	// the receiver's one-second epoch so its I2C buffer never fills.
	WeatherInterval time.Duration
	GPSInterval     time.Duration

	// WeatherMaxAge and FixMaxAge bound how old a reading may be before
	// WeatherStale / FixStale report it. Zero disables the age check.
	WeatherMaxAge time.Duration
	FixMaxAge     time.Duration
}

// DefaultConfig returns the board's wiring with conservative polling.
func DefaultConfig() Config {
	return Config{
		Bus:               "/dev/i2c-1",
		SHT45Addr:         sht45.Addr,
		BMP580Addr:        bmp580.Addr,
		GPSAddr:           neo.Addr,
		Precision:         PrecisionHigh,
		TempOversampling:  bmp580.X2,
		PressOversampling: bmp580.X16,
		WeatherInterval:   2 * time.Second,
		GPSInterval:       250 * time.Millisecond,
		WeatherMaxAge:     30 * time.Second,
		FixMaxAge:         5 * time.Second,
	}
}
