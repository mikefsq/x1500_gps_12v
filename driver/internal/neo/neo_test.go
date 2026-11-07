package neo

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

const (
	rmc = "$GNRMC,194214.00,A,3757.57219,N,12233.50818,W,0.148,,011026,,,D,V*0A"
	gga = "$GNGGA,194214.00,3757.57219,N,12233.50818,W,2,12,0.79,29.1,M,-29.6,M,,0000*44"
)

// newReceiver returns a fake DDC port whose stream yields data once, then
// reports nothing pending.
func newReceiver(data []byte) *i2c.Fake {
	f := &i2c.Fake{}
	pending := data
	f.OnWrite = func(f *i2c.Fake, p []byte) {
		switch p[0] {
		case regCount:
			f.Reply = []byte{byte(len(pending) >> 8), byte(len(pending))}
		case regStream:
			f.Reply = pending
			pending = nil
		}
	}
	return f
}

func newDev(f *i2c.Fake) *Dev {
	d := New(f)
	d.sleep = func(time.Duration) {}
	return d
}

func TestParseSentence(t *testing.T) {
	s, err := ParseSentence(rmc)
	if err != nil {
		t.Fatal(err)
	}
	if s.Talker != "GN" || s.Type != "RMC" || len(s.Fields) != 13 {
		t.Fatalf("sentence = %+v", s)
	}
	bad := rmc[:len(rmc)-2] + "0B"
	if _, err := ParseSentence(bad); !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
	if _, err := ParseSentence("garbage"); !errors.Is(err, ErrSentence) {
		t.Fatalf("err = %v, want ErrSentence", err)
	}
}

func TestFix(t *testing.T) {
	var f Fix
	for _, line := range []string{rmc, gga} {
		s, err := ParseSentence(line)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := f.Update(s); !ok || err != nil {
			t.Fatalf("Update(%s) = %v, %v", s.Type, ok, err)
		}
	}
	want := time.Date(2026, 10, 1, 19, 42, 14, 0, time.UTC)
	if !f.Time.Equal(want) || !f.Valid {
		t.Fatalf("time/valid = %v %v", f.Time, f.Valid)
	}
	if math.Abs(f.Lat-(37+57.57219/60)) > 1e-9 || math.Abs(f.Lon+(122+33.50818/60)) > 1e-9 {
		t.Fatalf("pos = %v, %v", f.Lat, f.Lon)
	}
	if f.Quality != 2 || f.Satellites != 12 || f.HDOP != 0.79 || f.AltM != 29.1 || f.GeoidSepM != -29.6 {
		t.Fatalf("fix = %+v", f)
	}
}

func TestNextSkipsJunk(t *testing.T) {
	stream := []byte("\xff\xff\xb5\x62\x01\x07junk\r\n" + rmc[:20] + "\r\n" + rmc + "\r\n" + gga + "\r\n")
	d := newDev(newReceiver(stream))
	for _, want := range []string{"RMC", "GGA"} {
		s, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		if s.Type != want {
			t.Fatalf("got %s, want %s", s.Type, want)
		}
	}
}

func TestAvailableNoCount(t *testing.T) {
	f := &i2c.Fake{}
	f.Regs[regCount], f.Regs[regCount+1] = 0xFF, 0xFF
	n, err := newDev(f).Available()
	if err != nil || n != 0 {
		t.Fatalf("Available = %d, %v", n, err)
	}
}

func TestPollSplitSentence(t *testing.T) {
	f := &i2c.Fake{}
	chunks := [][]byte{[]byte(rmc[:30]), []byte(rmc[30:] + "\r\n" + gga + "\r\n"), nil}
	var cur []byte
	f.OnWrite = func(f *i2c.Fake, p []byte) {
		switch p[0] {
		case regCount:
			cur, chunks = chunks[0], chunks[1:]
			f.Reply = []byte{byte(len(cur) >> 8), byte(len(cur))}
		case regStream:
			f.Reply = cur
		}
	}
	d := newDev(f)
	var got []string
	for range 3 {
		ss, err := d.Poll()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range ss {
			got = append(got, s.Type)
		}
	}
	if len(got) != 2 || got[0] != "RMC" || got[1] != "GGA" {
		t.Fatalf("got %v", got)
	}
}

func TestFixGSA(t *testing.T) {
	var f Fix
	s, err := ParseSentence("$GNGSA,A,3,06,31,16,04,26,03,46,,,,,,1.43,0.79,1.19,1*02")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := f.Update(s); !ok || err != nil {
		t.Fatalf("Update = %v, %v", ok, err)
	}
	if f.FixType != "3" || f.PDOP != 1.43 || f.HDOP != 0.79 || f.VDOP != 1.19 {
		t.Fatalf("fix = %+v", f)
	}
}
