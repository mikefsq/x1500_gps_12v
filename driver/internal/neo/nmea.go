package neo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrSentence is returned for a line that is not a well-formed sentence.
	ErrSentence = errors.New("neo: malformed NMEA sentence")
	// ErrChecksum is returned when a sentence fails its checksum.
	ErrChecksum = errors.New("neo: NMEA checksum mismatch")
)

// Sentence is one checksum-verified NMEA sentence.
type Sentence struct {
	Raw    string   // the full line, without CR/LF
	Talker string   // e.g. "GN", "GP"
	Type   string   // e.g. "RMC", "GGA"
	Fields []string // fields after the address field
}

// ParseSentence splits and checksum-verifies a "$TTSSS,...*HH" line.
func ParseSentence(line string) (Sentence, error) {
	if len(line) < 9 || line[0] != '$' {
		return Sentence{}, ErrSentence
	}
	star := strings.LastIndexByte(line, '*')
	if star < 0 || star+3 != len(line) {
		return Sentence{}, ErrSentence
	}
	want, err := strconv.ParseUint(line[star+1:], 16, 8)
	if err != nil {
		return Sentence{}, ErrSentence
	}
	var sum byte
	for i := 1; i < star; i++ {
		sum ^= line[i]
	}
	if sum != byte(want) {
		return Sentence{}, ErrChecksum
	}
	parts := strings.Split(line[1:star], ",")
	if len(parts[0]) != 5 {
		return Sentence{}, ErrSentence
	}
	return Sentence{Raw: line, Talker: parts[0][:2], Type: parts[0][2:], Fields: parts[1:]}, nil
}

// Fix is the receiver state assembled from RMC, GGA and GSA sentences.
type Fix struct {
	Time       time.Time // UTC; date comes from RMC
	Valid      bool      // RMC status A
	Lat, Lon   float64   // degrees, north and east positive
	Quality    int       // GGA fix quality: 0 none, 1 GPS, 2 DGPS, ...
	FixType    string    // GSA fix type as sent: "1" none, "2" 2D, "3" 3D; "" before any GSA
	Satellites int       // satellites used
	PDOP       float64   // from GSA
	HDOP       float64   // from GGA, refreshed by GSA
	VDOP       float64   // from GSA
	AltM       float64   // altitude above mean sea level, m
	GeoidSepM  float64   // geoid separation, m
	SpeedKnots float64
	CourseDeg  float64
}

// Update applies an RMC, GGA or GSA sentence to f. It reports whether s was
// one of those types; other sentences are ignored.
func (f *Fix) Update(s Sentence) (bool, error) {
	switch s.Type {
	case "RMC":
		return true, f.rmc(s.Fields)
	case "GGA":
		return true, f.gga(s.Fields)
	case "GSA":
		return true, f.gsa(s.Fields)
	}
	return false, nil
}

// gsa takes the fix type and dilutions. A multi-constellation receiver sends
// one GSA per constellation, each carrying the same combined values.
func (f *Fix) gsa(fl []string) error {
	if len(fl) < 17 {
		return fmt.Errorf("%w: short GSA", ErrSentence)
	}
	f.FixType = fl[1]
	f.PDOP = num(fl[14])
	f.HDOP = num(fl[15])
	f.VDOP = num(fl[16])
	return nil
}

func (f *Fix) rmc(fl []string) error {
	if len(fl) < 9 {
		return fmt.Errorf("%w: short RMC", ErrSentence)
	}
	f.Valid = fl[1] == "A"
	if fl[0] != "" && fl[8] != "" {
		t, err := time.Parse("020106150405.99", fl[8]+fl[0])
		if err != nil {
			return fmt.Errorf("%w: RMC time: %v", ErrSentence, err)
		}
		f.Time = t
	}
	var err error
	if f.Lat, f.Lon, err = latLon(fl[2], fl[3], fl[4], fl[5]); err != nil {
		return err
	}
	f.SpeedKnots = num(fl[6])
	f.CourseDeg = num(fl[7])
	return nil
}

func (f *Fix) gga(fl []string) error {
	if len(fl) < 11 {
		return fmt.Errorf("%w: short GGA", ErrSentence)
	}
	var err error
	if f.Lat, f.Lon, err = latLon(fl[1], fl[2], fl[3], fl[4]); err != nil {
		return err
	}
	f.Quality = int(num(fl[5]))
	f.Satellites = int(num(fl[6]))
	f.HDOP = num(fl[7])
	f.AltM = num(fl[8])
	f.GeoidSepM = num(fl[10])
	return nil
}

// latLon converts NMEA ddmm.mmmm / dddmm.mmmm fields to signed degrees.
func latLon(lat, ns, lon, ew string) (float64, float64, error) {
	if lat == "" || lon == "" {
		return 0, 0, nil
	}
	la, err1 := dm(lat)
	lo, err2 := dm(lon)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("%w: position", ErrSentence)
	}
	if ns == "S" {
		la = -la
	}
	if ew == "W" {
		lo = -lo
	}
	return la, lo, nil
}

func dm(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	deg := float64(int(v / 100))
	return deg + (v-deg*100)/60, nil
}

// num parses a numeric field, treating empty or invalid as 0.
func num(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
