# ArmoredWitness Trusted OS


## Introduction

TODO

## Supported hardware

The following table summarizes currently supported SoCs and boards.

| SoC          | Board                                                                                                                                                                                | SoC package                                                               | Board package                                                                        |
|--------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| NXP i.MX6UL  | [USB armory Mk II LAN](https://github.com/usbarmory/usbarmory/wiki)                                                                                                                  | [imx6ul](https://github.com/usbarmory/tamago/tree/master/soc/nxp/imx6ul)  | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory)      |

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

```
make trusted_applet && make DEBUG=1 trusted_os && make qemu
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

Trusted OS signing
==================

To maintain the chain of trust the Trusted OS must be signed, to this end the
`OS_PRIVATE_KEY1` and `OS_PRIVATE_KEY2` environment variables must be set to the path
of either [signify](https://man.openbsd.org/signify) or
[minisign](https://jedisct1.github.io/minisign/) siging keys, while compiling.

Example key generation (signify):

```
signify -G -p armored-witness-os-1.pub -s armored-witness-os-1.sec
```

Example key generation (minisign):

```
minisign -G -p armored-witness-os-1.pub -s armored-witness-os-1.sec
```

Trusted Applet authentication
=============================

To maintain the chain of trust the OS performes trusted applet authentication
before loading it, to this end the `APPLET_PUBLIC_KEY` environment variable
must be set to the path of either
[signify](https://man.openbsd.org/signify) or
[minisign](https://jedisct1.github.io/minisign/) keys, while compiling.

Example key generation (signify):

```
signify -G -p armory-witness.pub -s armory-witness.sec
```

Example key generation (minisign):

```
minisign -G -p armory-witness.pub -s armory-witness.sec
```

Building the compiler
=====================

Build the [TamaGo compiler](https://github.com/usbarmory/tamago-go)
(or use the [latest binary release](https://github.com/usbarmory/tamago-go/releases/latest)):

```
wget https://github.com/usbarmory/tamago-go/archive/refs/tags/latest.zip
unzip latest.zip
cd tamago-go-latest/src && ./all.bash
cd ../bin && export TAMAGO=`pwd`/go
```

Building and executing on ARM targets
=====================================

Build the example trusted applet and kernel executables as follows:

```
make trusted_applet && make trusted_os
```

Final executables are created in the `bin` subdirectory, `trusted_os.elf`
should be used for loading through `armory-witness-boot`.

The following targets are available:

| `TARGET`    | Board            | Executing and debugging                                                                                  |
|-------------|------------------|----------------------------------------------------------------------------------------------------------|
| `usbarmory` | UA-MKII-LAN      | [usbarmory/mk2](https://github.com/usbarmory/tamago/tree/master/board/usbarmory)                         |

The targets support native (see relevant documentation links in the table above)
as well as emulated execution (e.g. `make qemu`).

Debugging
---------

An optional Serial over USB console can be used to access Trusted OS and
Trusted Applet logs, it can be enabled when compiling with the `DEBUG`
environment variable set:

```
make trusted_applet && make DEBUG=1 trusted_os
```

The Serial over USB console can be accessed from a Linux host as follows:

```
picocom -b 115200 -eb /dev/ttyACM0 --imap lfcrlf
```

Trusted Applet installation
===========================

TODO

LED status
==========

The [USB armory Mk II](https://github.com/usbarmory/usbarmory/wiki) LEDs
are used, in sequence, as follows:

| Boot sequence                   | Blue | White |
|---------------------------------|------|-------|
| 0. initialization               | off  | off   |
| 1. trusted applet verified      | off  | on    |
| 2. trusted applet execution     | on   | on    |
