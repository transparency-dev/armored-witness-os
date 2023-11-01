FROM golang:1.21-bookworm

ARG TAMAGO_VERSION

# Install dependencies.
RUN apt-get update && apt-get install -y make wget u-boot-tools binutils-arm-none-eabi

RUN wget --quiet "https://github.com/usbarmory/tamago-go/releases/download/tamago-go${TAMAGO_VERSION}/tamago-go${TAMAGO_VERSION}.linux-amd64.tar.gz"
RUN tar -xf "tamago-go${TAMAGO_VERSION}.linux-amd64.tar.gz" -C /
# Set Tamago path for Make rule.
ENV TAMAGO=/usr/local/tamago-go/bin/go

WORKDIR /build

COPY . .

RUN make trusted_os_release
