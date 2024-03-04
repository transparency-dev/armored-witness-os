FROM golang:1.22-bookworm

ARG TAMAGO_VERSION
ARG LOG_ORIGIN
ARG LOG_PUBLIC_KEY
ARG APPLET_PUBLIC_KEY
ARG OS_PUBLIC_KEY1
ARG OS_PUBLIC_KEY2
ARG GIT_SEMVER_TAG
ARG SRK_HASH
# Build environment variables. In addition to routing these through to the make
# command, they MUST also be committed to in the manifest.
ARG BEE
ARG DEBUG

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
RUN echo "${OS_PUBLIC_KEY1}" > /tmp/os1.pub
RUN echo "${OS_PUBLIC_KEY2}" > /tmp/os2.pub

# Firmware transparency parameters for output binary.
ENV LOG_ORIGIN=${LOG_ORIGIN} \
    APPLET_PUBLIC_KEY="/tmp/applet.pub" \
    LOG_PUBLIC_KEY="/tmp/log.pub" \
    OS_PUBLIC_KEY1="/tmp/os1.pub" \
    OS_PUBLIC_KEY2="/tmp/os2.pub" \
    GIT_SEMVER_TAG=${GIT_SEMVER_TAG} \
    BEE=${BEE} \
    DEBUG=${DEBUG} \
    SRK_HASH=${SRK_HASH}

RUN make trusted_os_release
