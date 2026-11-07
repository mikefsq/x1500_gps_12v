package sht45

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

func word(v uint16) []byte {
	b := []byte{byte(v >> 8), byte(v)}
	return append(b, crc8(b))
}

func newFake(reply []byte) (*Dev, *i2c.Fake) {
	f := &i2c.Fake{Reply: reply}
	d := New(f)
	d.sleep = func(time.Duration) {}
	return d, f
}

func TestCRC8Datasheet(t *testing.T) {
	if got := crc8([]byte{0xBE, 0xEF}); got != 0x92 {
		t.Fatalf("crc8(BEEF) = %#x, want 0x92", got)
	}
}

func TestMeasure(t *testing.T) {
	// 0x6666 -> -45 + 175*0.4 = 25.0 °C; 0x8000 -> -6 + 125*0.50001 ≈ 56.5 %RH.
	d, f := newFake(append(word(0x6666), word(0x8000)...))
	r, err := d.Measure(High)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Writes) != 1 || f.Writes[0][0] != cmdMeasureHigh {
		t.Fatalf("writes = %x", f.Writes)
	}
	if math.Abs(r.TempC-25.0) > 0.01 || math.Abs(r.Humidity-56.5) > 0.01 {
		t.Fatalf("reading = %+v", r)
	}
}

func TestMeasureClampsHumidity(t *testing.T) {
	d, _ := newFake(append(word(0x6666), word(0xFFFF)...))
	r, err := d.Measure(Low)
	if err != nil {
		t.Fatal(err)
	}
	if r.Humidity != 100 {
		t.Fatalf("humidity = %v, want 100", r.Humidity)
	}
}

func TestMeasureBadCRC(t *testing.T) {
	b := append(word(0x6666), word(0x8000)...)
	b[2] ^= 0xFF
	d, _ := newFake(b)
	if _, err := d.Measure(High); !errors.Is(err, ErrCRC) {
		t.Fatalf("err = %v, want ErrCRC", err)
	}
}

func TestSerial(t *testing.T) {
	d, f := newFake(append(word(0x1234), word(0x5678)...))
	s, err := d.Serial()
	if err != nil {
		t.Fatal(err)
	}
	if s != 0x12345678 || f.Writes[0][0] != cmdSerial {
		t.Fatalf("serial = %#x, writes = %x", s, f.Writes)
	}
}
