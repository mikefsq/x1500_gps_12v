# GPS and weather driver library

Go library and diagnostic tools for the x1500_gps_12v board, running on the
Raspberry Pi. It reads SHT45 humidity/temperature, BMP580-family pressure, and
u-blox GPS over I²C. Weather and GPS can be opened independently.

Requires Go 1.24.4 or later. From this directory:

```sh
go test ./...
go build -o bin/board ./cmd/board
```

Other diagnostic commands are in `cmd/sht45`, `cmd/bmp580`, and `cmd/neo`.
Hardware access requires Linux and permission to access `/dev/i2c-1`.
See the [Pi setup instructions](../README.md#raspberry-pi-setup).

The module path is `github.com/mikefsq/x1500_gps_12v/driver`.
The Alpaca adapter is `goalpaca-devices/pi_mount_gps`, which prefers gpsd
and uses this library's I²C GPS subsystem for fallback.

## License

Copyright (c) 2026 mikefsq.

The software in this directory is licensed under
[GNU AGPL version 3 only](LICENSE) (`AGPL-3.0-only`).
