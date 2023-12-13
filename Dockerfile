FROM golang:1.21-bookworm

ARG TAMAGO_VERSION
ARG LOG_ORIGIN
ARG LOG_PUBLIC_KEY
ARG APPLET_PUBLIC_KEY
ARG GIT_SEMVER_TAG

# Install dependencies.
RUN apt-get update && apt-get install -y make wget u-boot-tools binutils-arm-none-eabi

RUN wget --quiet "https://github.com/usbarmory/tamago-go/releases/download/tamago-go${TAMAGO_VERSION}/tamago-go${TAMAGO_VERSION}.linux-amd64.tar.gz"
RUN tar -xf "tamago-go${TAMAGO_VERSION}.linux-amd64.tar.gz" -C /
# Set Tamago path for Make rule.
ENV TAMAGO=/usr/local/tamago-go/bin/go

WORKDIR /build

COPY . .

# The Makefile expects the verifiers to be in files, so make it so.
RUN echo "${APPLET_PUBLIC_KEY}" > /tmp/applet.pub
RUN echo "${LOG_PUBLIC_KEY}" > /tmp/log.pub

# Firmware transparency parameters for output binary.
ENV LOG_ORIGIN=${LOG_ORIGIN} \
    APPLET_PUBLIC_KEY="/tmp/applet.pub" \
    LOG_PUBLIC_KEY="/tmp/log.pub" \
    GIT_SEMVER_TAG=${GIT_SEMVER_TAG}

RUN make trusted_os_release
