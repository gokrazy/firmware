# gokrazy firmware repository

This repository holds bootloader firmware files for the Raspberry Pi 3, Pi 4,
Pi 5 and Pi Zero 2 W used by the [gokrazy](https://gokrazy.org/) project.

The files in this repository are picked up automatically by the `gok` tool, so
you don’t need to interact with this repository unless you want to update the
firmware to a custom version.

## Updating the firmware

First, follow the [gokrazy installation instructions](https://gokrazy.org/quickstart/).

Clone the firmware git repository:
```
git clone https://github.com/gokrazy/firmware
cd firmware
```

Install the firmware-related gokrazy tools:
```
go install ./cmd/...
```

And download the new firmware:
```
gokr-update-firmware
```

The new firmware files are stored in the working directory. Use `gok add .` to
ensure the next `gok` build will pick up your changed files.
