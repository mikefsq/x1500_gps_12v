// Command bmp580 reads temperature and pressure from the BMP580.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/bmp580"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
)

func main() {
	bus := flag.String("bus", "/dev/i2c-1", "I2C bus device")
	addr := flag.Uint("addr", bmp580.Addr, "I2C address")
	osrT := flag.Uint("osrt", 1, "temperature oversampling exponent 0..7 (2^n)")
	osrP := flag.Uint("osrp", 4, "pressure oversampling exponent 0..7 (2^n)")
	n := flag.Int("n", 1, "number of readings (0 = forever)")
	every := flag.Duration("i", time.Second, "interval between readings")
	reset := flag.Bool("reset", false, "soft-reset before reading")
	flag.Parse()

	if *osrT > 7 || *osrP > 7 {
		log.Fatal("oversampling exponent must be 0..7")
	}

	dev, err := i2c.Open(*bus, uint16(*addr))
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()

	b, err := bmp580.New(dev, bmp580.Oversampling(*osrT), bmp580.Oversampling(*osrP))
	if err != nil {
		log.Fatal(err)
	}
	if *reset {
		if err := b.Reset(); err != nil {
			log.Fatal(err)
		}
		if b, err = bmp580.New(dev, bmp580.Oversampling(*osrT), bmp580.Oversampling(*osrP)); err != nil {
			log.Fatal(err)
		}
	}
	for i := 0; *n == 0 || i < *n; i++ {
		if i > 0 {
			time.Sleep(*every)
		}
		r, err := b.Measure()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s  %.2f °C  %.2f hPa\n", time.Now().Format(time.TimeOnly), r.TempC, r.PressurePa/100)
	}
}
