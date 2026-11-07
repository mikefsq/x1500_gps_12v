// Command board opens the whole board and prints the cached weather and fix,
// exercising the same API an Alpaca driver uses.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	pimountgps "github.com/mikefsq/x1500_gps_12v/driver"
)

func main() {
	n := flag.Int("n", 3, "number of reports (0 = forever)")
	every := flag.Duration("i", 2*time.Second, "interval between reports")
	heating := flag.Float64("temp-offset", 0, "enclosure heat in °C (positive is subtracted from temperature; RH is compensated)")
	flag.Parse()

	cfg := pimountgps.DefaultConfig()
	cfg.EnclosureHeatingC = *heating
	b, warnings, err := pimountgps.Open(cfg)
	for _, w := range warnings {
		log.Print("warning: ", w)
	}
	if err != nil {
		log.Fatal(err)
	}
	defer b.Close()

	for i := 0; *n == 0 || i < *n; i++ {
		time.Sleep(*every)
		w := b.Weather()
		wAge, _ := b.WeatherAge()
		fmt.Printf("weather  %.2f °C  %.1f %%RH  dew %.2f °C  %.2f hPa  age %s stale=%t\n",
			w.Temperature, w.Humidity, w.Dewpoint, w.Pressure, wAge.Round(time.Millisecond), b.WeatherStale())
		if err := b.WeatherErr(); err != nil {
			fmt.Println("  weather error:", err)
		}
		if f, err := b.Fix(); err == nil {
			fAge, _ := b.FixAge()
			fmt.Printf("fix      %s  q=%d sats=%d  %.6f %.6f  alt %.1f m  age %s stale=%t\n",
				f.Time.Format(time.TimeOnly), f.Quality, f.Satellites, f.Lat, f.Lon, f.AltM,
				fAge.Round(time.Millisecond), b.FixStale())
		}
		if err := b.GPSErr(); err != nil {
			fmt.Println("  gps error:", err)
		}
	}
}
