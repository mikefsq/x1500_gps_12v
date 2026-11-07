// Command sht45 reads temperature and humidity from the SHT45.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/mikefsq/x1500_gps_12v/driver/internal/i2c"
	"github.com/mikefsq/x1500_gps_12v/driver/internal/sht45"
)

func main() {
	bus := flag.String("bus", "/dev/i2c-1", "I2C bus device")
	addr := flag.Uint("addr", sht45.Addr, "I2C address")
	prec := flag.String("p", "high", "precision: high, medium or low")
	n := flag.Int("n", 1, "number of readings (0 = forever)")
	every := flag.Duration("i", time.Second, "interval between readings")
	serial := flag.Bool("serial", false, "print the serial number")
	reset := flag.Bool("reset", false, "soft-reset before reading")
	flag.Parse()

	p, ok := map[string]sht45.Precision{"high": sht45.High, "medium": sht45.Medium, "low": sht45.Low}[*prec]
	if !ok {
		log.Fatalf("unknown precision %q", *prec)
	}

	dev, err := i2c.Open(*bus, uint16(*addr))
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()
	s := sht45.New(dev)

	if *reset {
		if err := s.Reset(); err != nil {
			log.Fatal(err)
		}
	}
	if *serial {
		sn, err := s.Serial()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("serial %08X\n", sn)
	}
	for i := 0; *n == 0 || i < *n; i++ {
		if i > 0 {
			time.Sleep(*every)
		}
		r, err := s.Measure(p)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s  %.2f °C  %.2f %%RH\n", time.Now().Format(time.TimeOnly), r.TempC, r.Humidity)
	}
}
