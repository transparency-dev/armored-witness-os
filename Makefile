# http://github.com/usbarmory/armory-witness
#
# Copyright (c) WithSecure Corporation
# https://foundry.withsecure.com
#
# Use of this source code is governed by the license
# that can be found in the LICENSE file.

BUILD_USER ?= $(shell whoami)
BUILD_HOST ?= $(shell hostname)
BUILD_DATE ?= $(shell /bin/date -u "+%Y-%m-%d %H:%M:%S")
BUILD_EPOCH := $(shell /bin/date -u "+%s")
BUILD_TAGS = linkramsize,linkramstart,disable_fr_auth,linkprintk
BUILD = ${BUILD_USER}@${BUILD_HOST} on ${BUILD_DATE}
REV = $(shell git rev-parse --short HEAD 2> /dev/null)

PROTOC ?= /usr/bin/protoc

SHELL = /bin/bash

ifeq ("${DEBUG}","1")
	BUILD_TAGS := ${BUILD_TAGS},debug
endif

SIGN = $(shell type -p signify || type -p signify-openbsd || type -p minisign)
SIGN_PWD ?= "armory-witness"

APP := ""
TEXT_START := 0x80010000 # ramStart (defined in mem.go under relevant tamago/soc package) + 0x10000

GOENV := GO_EXTLINK_ENABLED=0 CGO_ENABLED=0 GOOS=tamago GOARM=7 GOARCH=arm
ENTRY_POINT := _rt0_arm_tamago
QEMU ?= qemu-system-arm -machine mcimx6ul-evk -cpu cortex-a7 -m 512M \
        -nographic -monitor none -serial null -serial stdio \
        -net nic,model=imx.enet,netdev=net0 -netdev tap,id=net0,ifname=tap0,script=no,downscript=no \
        -semihosting

ARCH = "arm"
RUST_LINKER = "arm-none-eabi-ld"
RUST_TARGET = "armv7a-none-eabi"

GOFLAGS = -tags ${BUILD_TAGS} -trimpath -ldflags "-s -w -T ${TEXT_START} -E ${ENTRY_POINT} -R 0x1000 -X 'main.Build=${BUILD}' -X 'main.Revision=${REV}' -X 'main.Version=${BUILD_EPOCH}' -X 'main.PublicKey=$(shell test ${PUBLIC_KEY} && cat ${PUBLIC_KEY} | tail -n 1)'"
RUSTFLAGS = -C linker=${RUST_LINKER} -C link-args="--Ttext=$(TEXT_START)" --target ${RUST_TARGET}

.PHONY: clean qemu qemu-gdb

#### primary targets ####

all: trusted_applet witnessctl

elf: $(APP).elf

trusted_os: APP=trusted_os
trusted_os: DIR=$(CURDIR)/trusted_os
trusted_os: TEXT_START=0x80010000
trusted_os: check_os_env elf imx
	echo "signing Trusted OS"
	@if [ "${SIGN_PWD}" != "" ]; then \
		echo -e "${SIGN_PWD}\n" | ${SIGN} -S -s ${PRIVATE_KEY1} -m ${CURDIR}/bin/trusted_os.elf -x ${CURDIR}/bin/trusted_os.sig1; \
		echo -e "${SIGN_PWD}\n" | ${SIGN} -S -s ${PRIVATE_KEY2} -m ${CURDIR}/bin/trusted_os.elf -x ${CURDIR}/bin/trusted_os.sig2; \
	else \
		${SIGN} -S -s ${PRIVATE_KEY1} -m ${CURDIR}/bin/trusted_os.elf -x ${CURDIR}/bin/trusted_os.sig1; \
		${SIGN} -S -s ${PRIVATE_KEY2} -m ${CURDIR}/bin/trusted_os.elf -x ${CURDIR}/bin/trusted_os.sig2; \
	fi

trusted_applet: APP=trusted_applet
trusted_applet: DIR=$(CURDIR)/trusted_applet
trusted_applet: TEXT_START=0x90010000
trusted_applet: check_applet_env elf
	mkdir -p $(CURDIR)/trusted_os/assets
	echo "signing Trusted Applet"
	@if [ "${SIGN_PWD}" != "" ]; then \
		echo -e "${SIGN_PWD}\n" | ${SIGN} -S -s ${PRIVATE_KEY} -m ${CURDIR}/bin/trusted_applet.elf -x ${CURDIR}/trusted_os/assets/trusted_applet.sig; \
	else \
		${SIGN} -S -s ${PRIVATE_KEY} -m ${CURDIR}/bin/trusted_applet.elf -x ${CURDIR}/trusted_os/assets/trusted_applet.sig; \
	fi
	cp $(CURDIR)/bin/trusted_applet.elf $(CURDIR)/trusted_os/assets

witnessctl: check_tamago proto
	@echo "building armory-witness control tool"
	@cd $(CURDIR)/cmd/witnessctl && GOPATH="${BUILD_GOPATH}" ${TAMAGO} build -v \
		-ldflags "-s -w -X 'main.Build=${BUILD}' -X 'main.Revision=${REV}'" \
		-o $(CURDIR)/bin/witnessctl

#### ARM targets ####

imx: $(APP).imx

proto:
	@echo "generating protobuf classes"
	-rm -f $(CURDIR)/api/*.pb.go
	PATH=$(shell go env GOPATH | awk -F":" '{print $$1"/bin"}') ${PROTOC} --proto_path=$(CURDIR)/api --go_out=$(CURDIR)/api api.proto

$(APP).bin: CROSS_COMPILE=arm-none-eabi-
$(APP).bin: $(APP).elf
	$(CROSS_COMPILE)objcopy -j .text -j .rodata -j .shstrtab -j .typelink \
	    -j .itablink -j .gopclntab -j .go.buildinfo -j .noptrdata -j .data \
	    -j .bss --set-section-flags .bss=alloc,load,contents \
	    -j .noptrbss --set-section-flags .noptrbss=alloc,load,contents \
	    $(CURDIR)/bin/$(APP).elf -O binary $(CURDIR)/bin/$(APP).bin

$(APP).imx: $(APP).bin $(APP).dcd
	echo "## disabling TZASC bypass in DCD for pre-DDR initialization ##"; \
	chmod 644 $(CURDIR)/bin/$(APP).dcd; \
	echo "DATA 4 0x020e4024 0x00000001  # TZASC_BYPASS" >> $(CURDIR)/bin/$(APP).dcd; \
	mkimage -n $(CURDIR)/bin/$(APP).dcd -T imximage -e $(TEXT_START) -d $(CURDIR)/bin/$(APP).bin $(CURDIR)/bin/$(APP).imx
	# Copy entry point from ELF file
	dd if=$(CURDIR)/bin/$(APP).elf of=$(CURDIR)/bin/$(APP).imx bs=1 count=4 skip=24 seek=4 conv=notrunc

$(APP).dcd: check_tamago
$(APP).dcd: GOMODCACHE=$(shell ${TAMAGO} env GOMODCACHE)
$(APP).dcd: TAMAGO_PKG=$(shell grep "github.com/usbarmory/tamago v" go.mod | awk '{print $$1"@"$$2}')
$(APP).dcd: dcd

#### utilities ####

check_applet_env:
	@if [ "${PRIVATE_KEY}" == "" ] || [ ! -f "${PRIVATE_KEY}" ]; then \
		echo 'You need to set the PRIVATE_KEY variable to a valid signing key path'; \
		exit 1; \
	fi

check_os_env:
	@if [ "${PRIVATE_KEY1}" == "" ] || [ ! -f "${PRIVATE_KEY1}" ]; then \
		echo 'You need to set the PRIVATE_KEY1 variable to a valid signing key path'; \
		exit 1; \
	fi
	@if [ "${PRIVATE_KEY2}" == "" ] || [ ! -f "${PRIVATE_KEY2}" ]; then \
		echo 'You need to set the PRIVATE_KEY2 variable to a valid signing key path'; \
		exit 1; \
	fi
	@if [ "${PUBLIC_KEY}" == "" ] || [ ! -f "${PUBLIC_KEY}" ]; then \
		echo 'You need to set the PUBLIC_KEY variable to a valid authentication key path'; \
		exit 1; \
	fi

check_tamago:
	@if [ "${TAMAGO}" == "" ] || [ ! -f "${TAMAGO}" ]; then \
		echo 'You need to set the TAMAGO variable to a compiled version of https://github.com/usbarmory/tamago-go'; \
		exit 1; \
	fi

dcd:
	cp -f $(GOMODCACHE)/$(TAMAGO_PKG)/board/usbarmory/mk2/imximage.cfg $(CURDIR)/bin/$(APP).dcd

clean:
	@rm -fr $(CURDIR)/bin/* $(CURDIR)/trusted_os/assets/* $(CURDIR)/qemu.dtb

qemu:
	$(QEMU) -kernel $(CURDIR)/bin/trusted_os.elf

qemu-gdb:
	$(QEMU) -kernel $(CURDIR)/bin/trusted_os.elf -S -s

#### application target ####

$(APP).elf: check_tamago proto
	cd $(DIR) && $(GOENV) $(TAMAGO) build -tags ${BUILD_TAGS} $(GOFLAGS) -o $(CURDIR)/bin/$(APP).elf
