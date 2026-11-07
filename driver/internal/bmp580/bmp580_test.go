package bmp580

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

// newChip returns a fake BMP580 at power-on that completes a forced conversion
// immediately, producing the given raw readings.
func newChip(rawT int32, rawP uint32) *i2c.Fake {
	f := &i2c.Fake{}
	f.Regs[regChipID] = chipID
	f.Regs[regStatus] = statusNVMReady
	f.Regs[regODRConfig] = 0x70
	f.OnWrite = func(f *i2c.Fake, p []byte) {
		if len(p) == 2 && p[0] == regODRConfig && p[1]&odrModeMask == modeForced {
			t, pr := uint32(rawT), rawP
			copy(f.Regs[regTempXLSB:], []byte{byte(t), byte(t >> 8), byte(t >> 16), byte(pr), byte(pr >> 8), byte(pr >> 16)})
			f.Regs[regIntStatus] = intDataReady
			f.Regs[regODRConfig] &^= odrModeMask // back to standby
		}
	}
	return f
}

func newDev(t *testing.T, f *i2c.Fake) *Dev {
	t.Helper()
	d, err := New(f, X2, X16)
	if err != nil {
		t.Fatal(err)
	}
	d.sleep = func(time.Duration) {}
	return d
}

func TestNewConfigures(t *testing.T) {
	f := newChip(0, 0)
	newDev(t, f)
	if got := f.Regs[regOSRConfig]; got != osrPressEn|byte(X16)<<3|byte(X2) {
		t.Fatalf("OSR_CONFIG = %#x", got)
	}
	if f.Regs[regIntSource] != intDataReady {
		t.Fatalf("INT_SOURCE = %#x", f.Regs[regIntSource])
	}
}

func TestNewChecksChip(t *testing.T) {
	f := newChip(0, 0)
	f.Regs[regChipID] = 0x58
	if _, err := New(f, X1, X1); !errors.Is(err, ErrChipID) {
		t.Fatalf("err = %v, want ErrChipID", err)
	}
	f = newChip(0, 0)
	f.Regs[regStatus] = statusNVMReady | statusNVMErr
	if _, err := New(f, X1, X1); !errors.Is(err, ErrNVM) {
		t.Fatalf("err = %v, want ErrNVM", err)
	}
}

func TestMeasure(t *testing.T) {
	// 21.5 °C, 101325 Pa.
	f := newChip(int32(21.5*65536), 101325*64)
	d := newDev(t, f)
	r, err := d.Measure()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.TempC-21.5) > 1e-6 || math.Abs(r.PressurePa-101325) > 1e-6 {
		t.Fatalf("reading = %+v", r)
	}
	if got := f.Regs[regODRConfig]; got&^odrModeMask != 0x70|odrDeepDis {
		t.Fatalf("ODR_CONFIG = %#x, ODR bits not preserved", got)
	}
}

func TestMeasureNegativeTemp(t *testing.T) {
	d := newDev(t, newChip(int32(-10.25*65536), 90000*64))
	r, err := d.Measure()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.TempC+10.25) > 1e-6 {
		t.Fatalf("temp = %v, want -10.25", r.TempC)
	}
}

func TestMeasureTimeout(t *testing.T) {
	f := newChip(0, 0)
	d := newDev(t, f)
	f.OnWrite = nil // never signals data-ready
	d.timeout = 0
	if _, err := d.Measure(); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}
