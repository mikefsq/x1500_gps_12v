// Command neo reads NMEA from the u-blox receiver over I2C and prints fixes.
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/neo"
)

func main() {
	bus := flag.String("bus", "/dev/i2c-1", "I2C bus device")
	addr := flag.Uint("addr", neo.Addr, "I2C address")
	n := flag.Int("n", 1, "number of fixes (0 = forever); with -raw, number of sentences")
	raw := flag.Bool("raw", false, "print raw NMEA sentences")
	flag.Parse()

	dev, err := i2c.Open(*bus, uint16(*addr))
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()
	g := neo.New(dev)

	var fix neo.Fix
	for i := 0; *n == 0 || i < *n; {
		s, err := g.Next()
		if err != nil {
			log.Fatal(err)
		}
		if *raw {
			fmt.Println(s.Raw)
			i++
			continue
		}
		if _, err := fix.Update(s); err != nil {
			log.Print(err)
			continue
		}
		if s.Type != "GGA" || fix.Time.IsZero() {
			continue // print once per epoch, after RMC and GGA
		}
		fmt.Printf("%s  valid=%t q=%d sats=%d hdop=%.2f  %.6f %.6f  alt %.1f m\n",
			fix.Time.Format("2006-01-02 15:04:05Z"), fix.Valid, fix.Quality, fix.Satellites,
			fix.HDOP, fix.Lat, fix.Lon, fix.AltM)
		i++
	}
}
