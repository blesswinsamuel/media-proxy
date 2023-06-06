FROM golang:1.20-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

# RUN go get github.com/githubnemo/CompileDaemon

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN go build -ldflags="-s -w" -o media-proxy .

FROM alpine

# Copy our static executable.
COPY --from=builder /app/media-proxy /go/bin/media-proxy
# Run the hello binary.

CMD ["/go/bin/media-proxy"]
