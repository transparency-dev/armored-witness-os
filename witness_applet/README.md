# ArmoredWitness Applet

This repo contains code for a GoTEE Trusted Applet which implements
a witness. It's intended to be used with the Trusted OS found at
https://github.com/transparency-dev/armored-witness-os.

## Introduction

TODO.

## Supported hardware

The following table summarizes currently supported SoCs and boards.

| SoC          | Board                                                               | SoC package                                                              | Board package                                                                    |
|--------------|---------------------------------------------------------------------|--------------------------------------------------------------------------|----------------------------------------------------------------------------------|
| NXP i.MX6UL  | [USB armory Mk II LAN](https://github.com/usbarmory/usbarmory/wiki) | [imx6ul](https://github.com/usbarmory/tamago/tree/master/soc/nxp/imx6ul) | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory) |
| NXP i.MX6ULL | [USB armory Mk II](https://github.com/usbarmory/usbarmory/wiki)     | [imx6ul](https://github.com/usbarmory/tamago/tree/master/soc/nxp/imx6ul) | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory) |

The GoTEE [syscall](https://github.com/usbarmory/GoTEE/blob/master/syscall/syscall.go)
interface is implemented for communication between the Trusted OS and Trusted
Applet.

When launched, the witness applet is reachable via SSH through the first
Ethernet port.

```none
$ ssh ta@10.0.0.1

date            (time in RFC339 format)?                 # show/change runtime date and time
dns             <fqdn>                                   # resolve domain (requires routing)
exit, quit                                               # close session
hab             <hex SRK hash>                           # secure boot activation (*irreversible*)
help                                                     # this help
led             (white|blue|yellow|green) (on|off)       # LED control
mmc             <hex offset> <size>                      # MMC card read
reboot                                                   # reset device
stack                                                    # stack trace of current goroutine
stackall                                                 # stack trace of all goroutines
status                                                   # status information

>
```

The witness can be also executed under QEMU emulation, including networking
support (requires a `tap0` device routing the Trusted Applet IP address),
through `armored-witness-os`.

> :warning: emulated runs perform partial tests due to lack of full hardware
> support by QEMU.

```none
make DEBUG=1 trusted_os && make qemu
...
00:00:00 tamago/arm • TEE security monitor (Secure World system/monitor)
00:00:00 SM applet verification
00:00:01 SM applet verified
00:00:01 SM loaded applet addr:0x90000000 entry:0x9007751c size:14228514
00:00:01 SM starting mode:USR sp:0xa0000000 pc:0x9007751c ns:false
00:00:02 tamago/arm • TEE user applet
00:00:02 TA MAC:1a:55:89:a2:69:41 IP:10.0.0.1 GW:10.0.0.2 DNS:8.8.8.8:53
00:00:02 TA requesting SM status
00:00:02 ----------------------------------------------------------- Trusted OS ----
00:00:02 Secure Boot ............: false
00:00:02 Runtime ................: tamago/arm
00:00:02 Link ...................: false
00:00:02 TA starting ssh server (SHA256:eeMIwwN/zw1ov1BvO6sW3wtYi463sq+oLgKhmAew1WE) at 10.0.0.1:22
```

## Trusted Applet authentication

To maintain the chain of trust the Trusted Applet must be signed and logged.
To this end, two [note](https://pkg.go.dev/golang.org/x/mod/sumdb/note) signing keys
must be generated.

```bash
$ go run github.com/transparency-dev/serverless-log/cmd/generate_keys@HEAD \
  --key_name="DEV-TrustedApplet" \
  --out_priv=armored-witness-applet.sec \
  --out_pub=armored-witness-applet.pub
```

The corresponding public key files will be built into the Trusted OS to verify the Applet.

## Firmware transparency

All ArmoredWitness firmware artefacts need to be added to a firmware transparency log.

The provided `Makefile` has support for maintaining a local firmware transparency
log on disk. This is intended to be used for development only.

In order to use this functionality, a log key pair can be generated with the
following command:

```bash
$ go run github.com/transparency-dev/serverless-log/cmd/generate_keys@HEAD \
  --key_name="DEV-Log" \
  --out_priv=armored-witness-log.sec \
  --out_pub=armored-witness-log.pub
```

## Building and executing on ARM targets

Download and install the
[latest TamaGo binary release](https://github.com/usbarmory/tamago-go/releases/latest).

### Building the applet

Ensure the following environment variables are set:

| Variable                | Description
|-------------------------|------------
| `APPLET_PRIVATE_KEY1`   | Path to Trusted Applet firmware signing key. Used by the Makefile to sign the applet.
| `LOG_PRIVATE_KEY`       | Path to log signing key. Used by Makefile to add the new applet firmware to the local dev log.
| `LOG_ORIGIN`            | FT log origin string. Used by Makefile to update the local dev log.
| `DEV_LOG_DIR`           | Path to directory in which to store the dev FT log files.

The applet firmware image can then be built, signed, and logged with the following command:

```bash
make trusted_applet log_applet
```

Final executables are created in the `bin` subdirectory, `trusted_applet.elf`
should be used for loading through `armored-witness-os`.

Firmware transparency artefacts will be written into `${DEV_LOG_DIR}`.

### Encrypted RAM support

Only on i.MX6UL P/Ns, the `BEE` environment variable must be set to match
`armored-witness-boot` and `armored-witness-os` compilation options in case AES
CTR encryption for all external RAM, using TamaGo
[bee package](https://pkg.go.dev/github.com/usbarmory/tamago/soc/nxp/bee),
is configured at boot.

The following targets are available:

| `TARGET`    | Board            | Executing and debugging                                                                                  |
|-------------|------------------|----------------------------------------------------------------------------------------------------------|
| `usbarmory` | UA-MKII-LAN      | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory)                         |

The targets support native (see relevant documentation links in the table above)
as well as emulated execution (e.g. `make qemu`).

### Debugging

An optional Serial over USB console can be used to access Trusted OS and
Trusted Applet logs, it can be enabled when compiling with the `DEBUG`
environment variable set:

```bash 
make DEBUG=1 trusted_applet log_applet
```

The Serial over USB console can be accessed from a Linux host as follows:

```bash
picocom -b 115200 -eb /dev/ttyACM0 --imap lfcrlf
```

## Trusted Applet installation

Installing the various firmware images onto the device can be accomplished using the
[provision tool](https://github.com/transparency-dev/armored-witness/tree/main/cmd/provision).

## LED status

The [USB armory Mk II](https://github.com/usbarmory/usbarmory/wiki) LEDs
are used, in sequence, as follows:

| Boot sequence                   | Blue | White |
|---------------------------------|------|-------|
| 0. initialization               | off  | off   |
| 1. trusted applet verified      | off  | on    |
| 2. trusted applet execution     | on   | on    |

