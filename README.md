# ArmoredWitness Trusted OS

## Introduction

TODO

## Supported hardware

The following table summarizes currently supported SoCs and boards.

| SoC          | Board                                                               | SoC package                                                              | Board package                                                                    |
|--------------|---------------------------------------------------------------------|--------------------------------------------------------------------------|----------------------------------------------------------------------------------|
| NXP i.MX6UL  | [USB armory Mk II LAN](https://github.com/usbarmory/usbarmory/wiki) | [imx6ul](https://github.com/usbarmory/tamago/tree/master/soc/nxp/imx6ul) | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory) |
| NXP i.MX6ULL | [USB armory Mk II](https://github.com/usbarmory/usbarmory/wiki)     | [imx6ul](https://github.com/usbarmory/tamago/tree/master/soc/nxp/imx6ul) | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory) |

## Purpose

This trusted OS is a [TamaGo](https://github.com/usbarmory/tamago) unikernel
intended to run on the board(s) listed above in the TrustZone Secure World
system mode, to be used in conjuction with the counterpart
[witness trusted applet](https://github.com/transparency-dev/armored-witness-applet)
 unikernel running in the Secure World user mode.

The GoTEE [syscall](https://github.com/usbarmory/GoTEE/blob/master/syscall/syscall.go)
interface is implemented for communication between the Trusted OS and Trusted
Applet.

The trusted OS can be also executed under QEMU emulation, including networking
support (requires a `tap0` device routing the Trusted Applet IP address).

> :warning: emulated runs perform partial tests due to lack of full hardware
> support by QEMU.

```bash
make DEBUG=1 make qemu
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



## Trusted OS signing

For an overview of the firmware authentication process please see
<https://github.com/transparency-dev/armored-witness/tree/main/docs/firmware_auth.md>.

To maintain the chain of trust the Trusted OS must be signed and logged.
To this end, two [note](https://pkg.go.dev/golang.org/x/mod/sumdb/note) signing keys
must be generated.

```bash
$ go run github.com/transparency-dev/serverless-log/cmd/generate_keys@HEAD \
  --key_name="DEV-TrustedOS-1" \
  --out_priv=armored-witness-os-1.sec \
  --out_pub=armored-witness-os-1.pub
$ go run github.com/transparency-dev/serverless-log/cmd/generate_keys@HEAD \
  --key_name="DEV-TrustedOS-2" \
  --out_priv=armored-witness-os-2.sec \
  --out_pub=armored-witness-os-2.pub
```

The corresponding public key files will be built into the bootloader to verify the OS.

## Trusted Applet authentication

To maintain the chain of trust the OS performs trusted applet authentication
before executing it. This includes verifying signatures and Firmware Transparency
artefacts produced when the applet was built.

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

### Building the OS

Ensure the following environment variables are set:

| Variable            | Description
|---------------------|------------
| `OS_PRIVATE_KEY1`   | Path to OS firmware signing key 1. Used by the Makefile to sign the OS.
| `OS_PRIVATE_KEY2`   | Path to OS firmware signing key 2. Used by the Makefile to sign the OS.
| `APPLET_PUBLIC_KEY` | Path to applet firmware verification key. Embedded into the OS to verify the applet at run-time.
| `LOG_PUBLIC_KEY`    | Path to log verification key. Embedded into the OS to verify at run-time that the applet is correctly logged.
| `LOG_ORIGIN`        | FT log origin string. Embedded into the OS to verify applet firmware transparency.
| `LOG_PRIVATE_KEY`   | Path to log signing key. Used by Makefile to add the new OS firmware to the local dev log.
| `DEV_LOG_DIR`       | Path to directory in which to store the dev FT log files.

The OS firmware image can then be built, signed, and logged with the following command:

```bash
# The trusted_os target builds the firmware image, and log_os target adds it
# to the local firmware transparency log.
make trusted_os log_os
```

The final executable, `trusted_os.elf` is created in the `bin` subdirectory, and
should be used for loading through `armored-witness-boot`.

Firmware transparency artefacts will be written into `${DEV_LOG_DIR}`.

### Development builds

To aid in development, it is also possible to build the OS with the Trusted Applet
directly embedded within it:

```bash
make trusted_os_embedded_applet
```

The resulting `bin/trusted_os.elf` may be seral booted directly to the device with
the `imx_boot` tool, or similar.
Note that since this OS image is not being loaded via the bootloader, it does not need
to be added to the FT log.

### Encrypted RAM support

Only on i.MX6UL P/Ns, the `BEE` environment variable must be set to match
`armored-witness-boot` compilation options in case AES CTR encryption for all
external RAM, using TamaGo [bee package](https://pkg.go.dev/github.com/usbarmory/tamago/soc/nxp/bee),
is configured at boot.

The following targets are available:

| `TARGET`    | Board            | Executing and debugging                                                                                  |
|-------------|------------------|----------------------------------------------------------------------------------------------------------|
| `usbarmory` | UA-MKII-LAN      | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory)                         |

The targets support native (see relevant documentation links in the table above)
as well as emulated execution (e.g. `make qemu`).

## Debugging

An optional Serial over USB console can be used to access Trusted OS and
Trusted Applet logs, it can be enabled when compiling with the `DEBUG`
environment variable set:

```bash
make DEBUG=1 trusted_os
```

The Serial over USB console can be accessed from a Linux host as follows:

```bash
picocom -b 115200 -eb /dev/ttyACM0 --imap lfcrlf
```

### QEMU

The Trusted OS image can be executed under emulation as follows:

```bash
make qemu
```

The emulation run network connectivity should be configured as follows (Linux
example with tap0):

```bash
ip addr add 10.0.0.2/24 dev tap0
ip link set tap0 up
ip tuntap add dev tap0 mode tap group <your user group>
```

The emulated target can be debugged with GDB using `make qemu-gdb`, this will
make qemu waiting for a GDB connection that can be launched as follows:

```bash
arm-none-eabi-gdb -ex "target remote 127.0.0.1:1234" example
```

Breakpoints can be set in the usual way:

```none
b ecdsa.GenerateKey
continue
```

## Trusted Applet installation

Installing the various firmware images onto the device can be accomplished using the
[provision](https://github.com/transparency-dev/armored-witness/tree/main/cmd/provision)
tool.

## LED status

The [USB armory Mk II](https://github.com/usbarmory/usbarmory/wiki) LEDs
are used, in sequence, as follows:

| Boot sequence                   | Blue | White |
|---------------------------------|------|-------|
| 0. initialization               | off  | off   |
| 1. trusted applet verified      | off  | on    |
| 2. trusted applet execution     | on   | on    |
