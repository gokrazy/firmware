# gokrazy firmware repository

This repository holds bootloader firmware files for the Raspberry Pi
3, used by the [gokrazy](https://github.com/gokrazy/gokrazy) project.

The files in this repository are picked up automatically by
`gokr-packer`, so you don’t need to interact with this repository
unless you want to update the firmware to a custom version.

## Updating the firmware

First, follow the [gokrazy installation instructions](https://github.com/gokrazy/gokrazy).

Install the firmware-related gokrazy tools:
```
go install github.com/gokrazy/firmware/cmd/...
```

And download the new firmware:
```
gokr-update-firmware
```

The new firmware files are stored in
`$GOPATH/src/github.com/gokrazy/firmware` so that it will be picked up
by `gokr-packer`.