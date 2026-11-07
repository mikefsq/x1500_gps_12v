package pimountgps

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/bmp580"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/neo"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/sht45"
)

// fakeSHT returns an SHT45 fake that answers every command with raw words t, rh.
// fail, if set, makes it answer with a corrupt CRC.
func fakeSHT(t, rh uint16, fail *bool) *i2c.Fake {
	word := func(v uint16) []byte {
		b := []byte{byte(v >> 8), byte(v)}
		crc := byte(0xFF)
		for _, x := range b {
			crc ^= x
			for range 8 {
				if crc&0x80 != 0 {
					crc = crc<<1 ^ 0x31
				} else {
					crc <<= 1
				}
			}
		}
		return append(b, crc)
	}
	f := &i2c.Fake{}
	f.OnWrite = func(f *i2c.Fake, p []byte) {
		f.Reply = append(word(t), word(rh)...)
		if fail != nil && *fail {
			f.Reply[2] ^= 0xFF
		}
	}
	return f
}

// fakeBMP returns a BMP580 fake whose forced conversions yield 20 °C, 1000 hPa.
func fakeBMP() *i2c.Fake {
	f := &i2c.Fake{}
	f.Regs[0x01] = 0x50 // chip ID
	f.Regs[0x28] = 0x02 // NVM ready
	f.OnWrite = func(f *i2c.Fake, p []byte) {
		if len(p) == 2 && p[0] == 0x37 && p[1]&3 == 2 { // forced mode
			t, pr := uint32(20*65536), uint32(100000*64)
			copy(f.Regs[0x1D:], []byte{byte(t), byte(t >> 8), byte(t >> 16), byte(pr), byte(pr >> 8), byte(pr >> 16)})
			f.Regs[0x27] = 0x01
		}
	}
	return f
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{t: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)} }
func mustBMP(t *testing.T, f *i2c.Fake) *bmp580.Dev {
	t.Helper()
	d, err := bmp580.New(f, bmp580.X1, bmp580.X1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDewpoint(t *testing.T) {
	// Magnus reference: 20 °C at 50 %RH ≈ 9.26 °C.
	if got := Dewpoint(20, 50); math.Abs(got-9.26) > 0.01 {
		t.Fatalf("Dewpoint(20,50) = %.3f", got)
	}
	if got := Dewpoint(15, 100); math.Abs(got-15) > 1e-9 {
		t.Fatalf("Dewpoint(15,100) = %v, want 15", got)
	}
}

func TestRefresh(t *testing.T) {
	c := newClock()
	b := newBoard(DefaultConfig(), devices{
		sht: sht45.New(fakeSHT(0x6666, 0x8000, nil)), // 25 °C, ~56.5 %RH
		bmp: mustBMP(t, fakeBMP()),
	})
	b.now = c.now
	if !b.WeatherStale() {
		t.Fatal("stale before first read")
	}
	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	w := b.Weather()
	if math.Abs(w.Temperature-25) > 0.01 || math.Abs(w.Humidity-56.5) > 0.01 ||
		w.Pressure != 1000 || w.BaroTemperature != 20 {
		t.Fatalf("weather = %+v", w)
	}
	if math.Abs(w.Dewpoint-Dewpoint(w.Temperature, w.Humidity)) > 1e-9 {
		t.Fatalf("dewpoint = %v", w.Dewpoint)
	}
	if b.WeatherStale() {
		t.Fatal("stale right after read")
	}
	c.add(31 * time.Second)
	if !b.WeatherStale() {
		t.Fatal("not stale past WeatherMaxAge")
	}
}

func TestRefreshPartialFailureAges(t *testing.T) {
	c := newClock()
	fail := false
	b := newBoard(DefaultConfig(), devices{
		sht: sht45.New(fakeSHT(0x6666, 0x8000, &fail)),
		bmp: mustBMP(t, fakeBMP()),
	})
	b.now = c.now
	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	fail = true
	c.add(10 * time.Second)
	if err := b.Refresh(); !errors.Is(err, sht45.ErrCRC) {
		t.Fatalf("err = %v, want ErrCRC", err)
	}
	if age, ok := b.WeatherAge(); !ok || age != 10*time.Second {
		t.Fatalf("age = %v %v, want the failing sensor's 10s", age, ok)
	}
	if math.Abs(b.Weather().Temperature-25) > 0.01 {
		t.Fatal("failed read clobbered last good temperature")
	}
}

func TestBMPOnlyTemperature(t *testing.T) {
	b := newBoard(DefaultConfig(), devices{bmp: mustBMP(t, fakeBMP())})
	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	if w := b.Weather(); w.Temperature != 20 {
		t.Fatalf("temperature = %v, want BMP580's 20", w.Temperature)
	}
}

func TestNoDevices(t *testing.T) {
	b := newBoard(DefaultConfig(), devices{})
	if err := b.Refresh(); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("Refresh err = %v", err)
	}
	if _, err := b.Fix(); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("Fix err = %v", err)
	}
	if !b.WeatherStale() || !b.FixStale() {
		t.Fatal("absent subsystems must read stale")
	}
}

func TestPollGPS(t *testing.T) {
	stream := "$GNRMC,194214.00,A,3757.57219,N,12233.50818,W,0.148,,011026,,,D,V*0A\r\n" +
		"$GNGGA,194214.00,3757.57219,N,12233.50818,W,2,12,0.79,29.1,M,-29.6,M,,0000*44\r\n"
	f := &i2c.Fake{}
	pending := []byte(stream)
	f.OnWrite = func(f *i2c.Fake, p []byte) {
		switch p[0] {
		case 0xFD:
			f.Reply = []byte{byte(len(pending) >> 8), byte(len(pending))}
		case 0xFF:
			f.Reply, pending = pending, nil
		}
	}
	c := newClock()
	b := newBoard(DefaultConfig(), devices{gps: neo.New(f)})
	b.now = c.now
	if !b.FixStale() {
		t.Fatal("stale before any fix")
	}
	var epoch Fix
	b.pollGPS(&epoch)
	fix, err := b.Fix()
	if err != nil {
		t.Fatal(err)
	}
	if !fix.Valid || fix.Quality != 2 || fix.Satellites != 12 || fix.AltM != 29.1 {
		t.Fatalf("fix = %+v", fix)
	}
	if b.FixStale() {
		t.Fatal("fresh fix reads stale")
	}
	c.add(6 * time.Second)
	if !b.FixStale() {
		t.Fatal("not stale past FixMaxAge")
	}
}

func TestLoopAndClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WeatherInterval = time.Millisecond
	b := newBoard(cfg, devices{sht: sht45.New(fakeSHT(0x6666, 0x8000, nil))})
	b.start()
	deadline := time.Now().Add(time.Second)
	for b.WeatherStale() {
		if time.Now().After(deadline) {
			t.Fatal("loop never read the sensor")
		}
		time.Sleep(time.Millisecond)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
}

func TestHasSensors(t *testing.T) {
	b := newBoard(DefaultConfig(), devices{bmp: mustBMP(t, fakeBMP())})
	if b.HasHumidity() || !b.HasPressure() {
		t.Fatalf("HasHumidity=%v HasPressure=%v", b.HasHumidity(), b.HasPressure())
	}
}

func TestEnclosureHeatingCompensation(t *testing.T) {
	rawRH := -6 + 125*float64(0x8000)/65535
	rawDew := Dewpoint(25, rawRH)
	for _, heat := range []float64{0, 5, 15} {
		cfg := DefaultConfig()
		cfg.EnclosureHeatingC = heat
		b := newBoard(cfg, devices{sht: sht45.New(fakeSHT(0x6666, 0x8000, nil)), bmp: mustBMP(t, fakeBMP())})
		for i := 0; i < 2; i++ { // Repeated reads must not compound the correction.
			if err := b.Refresh(); err != nil {
				t.Fatal(err)
			}
			w := b.Weather()
			if math.Abs(w.Temperature-(25-heat)) > 1e-9 || w.Pressure != 1000 || w.BaroTemperature != 20 {
				t.Fatalf("heat %g: %+v", heat, w)
			}
			if math.Abs(w.Dewpoint-rawDew) > 1e-9 || math.Abs(b.DewMargin()-(25-heat-rawDew)) > 1e-9 {
				t.Fatalf("dew point or margin: %+v", w)
			}
			if heat == 0 && w.Humidity != rawRH {
				t.Fatal("zero offset changed RH")
			}
			if heat == 5 {
				if w.Humidity < 76 || w.Humidity > 77 || math.Abs(Dewpoint(w.Temperature, w.Humidity)-rawDew) > 1e-9 {
					t.Fatalf("compensated RH: %g", w.Humidity)
				}
			}
			if heat == 15 && w.Humidity != 100 {
				t.Fatalf("RH must cap at saturation: %g", w.Humidity)
			}
		}
	}
	cfg := DefaultConfig()
	cfg.EnclosureHeatingC = 5
	b := newBoard(cfg, devices{bmp: mustBMP(t, fakeBMP())})
	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	if w := b.Weather(); w.Temperature != 15 || w.BaroTemperature != 20 || w.Humidity != 0 || w.Dewpoint != 0 {
		t.Fatalf("BMP-only: %+v", w)
	}
}

func TestInvalidEnclosureHeating(t *testing.T) {
	for _, heat := range []float64{-1, math.NaN(), math.Inf(1)} {
		cfg := DefaultConfig()
		cfg.EnclosureHeatingC = heat
		if b, _, err := OpenSubsystems(cfg, 0); err == nil || b != nil {
			t.Fatalf("accepted invalid heating %g", heat)
		}
	}
}

func TestLiveEnclosureHeating(t *testing.T) {
	b := newBoard(DefaultConfig(), devices{sht: sht45.New(fakeSHT(0x6666, 0x8000, nil))})
	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	raw := b.Weather()
	for _, heat := range []float64{5, 2, 0} {
		if err := b.SetEnclosureHeating(heat); err != nil {
			t.Fatal(err)
		}
		w := b.Weather()
		if math.Abs(w.Temperature-(raw.Temperature-heat)) > 1e-9 || w.Dewpoint != raw.Dewpoint {
			t.Fatalf("live correction: %+v", w)
		}
	}
	if b.Weather() != raw {
		t.Fatal("reset did not restore raw readings")
	}
	if err := b.SetEnclosureHeating(-1); err == nil || b.Weather() != raw {
		t.Fatal("invalid change altered readings")
	}
}
