FROM amd64/golang:latest

ARG TAMAGO_VERSION
ARG PROTOC_VERSION
ARG PROTOC_GEN_GO_VERSION

# Install dependencies.
RUN apt-get update && apt-get install -y make
RUN apt-get install -y unzip
RUN apt-get install -y wget
RUN apt-get install -y u-boot-tools
RUN apt-get install -y binutils-arm-none-eabi

RUN go install "google.golang.org/protobuf/cmd/protoc-gen-go@v${PROTOC_GEN_GO_VERSION}"

RUN wget "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip"
RUN unzip "protoc-${PROTOC_VERSION}-linux-x86_64.zip" -d /
# Set protoc path for Make rule.
ENV PROTOC=/bin/protoc

RUN wget "https://github.com/usbarmory/tamago-go/releases/download/tamago-go${TAMAGO_VERSION}/tamago-go${TAMAGO_VERSION}.linux-amd64.tar.gz"
RUN tar -xvf "tamago-go${TAMAGO_VERSION}.linux-amd64.tar.gz" -C /
# Set Tamago path for Make rule.
ENV TAMAGO=/usr/local/tamago-go/bin/go

WORKDIR /build

COPY . .

RUN make trusted_os
