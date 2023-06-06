FROM --platform=$BUILDPLATFORM golang:1.20 AS builder

WORKDIR /app

# RUN apk add --no-cache git build-base pkgconfig vips-dev
RUN apt-get update && apt-get install -y gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu && apt-get clean
RUN dpkg --add-architecture arm64 && dpkg --add-architecture amd64

ARG TARGETOS
ARG TARGETARCH
RUN apt-get update && apt-get install -y libvips-dev:${TARGETARCH} && apt-get clean

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# https://gist.github.com/ryankurte/69a6ae74a60a0ea0b68e7f3ef43f44b2
RUN if [ "${TARGETARCH}" = "arm64" ] ; then \
    export CC=aarch64-linux-gnu-gcc ; \
    export PKG_CONFIG_LIBDIR=/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig ; \
    elif [ "${TARGETARCH}" = "amd64" ] ; then \
    export CC=x86_64-linux-gnu-gcc ; \
    export PKG_CONFIG_LIBDIR=/usr/lib/x86_64-linux-gnu/pkgconfig:/usr/share/pkgconfig ; \
    fi ; \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o media-proxy .

FROM alpine

RUN apk add --no-cache vips

COPY --from=builder /app/media-proxy /go/bin/media-proxy

CMD ["/go/bin/media-proxy"]
