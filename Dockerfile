# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25.5-alpine AS builder
ARG TARGETOS TARGETARCH
ARG VERSION=dev

WORKDIR /app

RUN apk update && apk add --no-cache git ca-certificates tzdata && update-ca-certificates

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-X 'main.Version=$VERSION'" -o cloudflare-tunnel-ingress-controller ./cmd/controller

FROM scratch AS main

WORKDIR /app

COPY --from=builder /app/cloudflare-tunnel-ingress-controller .
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER 1001

ENTRYPOINT ["/app/cloudflare-tunnel-ingress-controller"]
