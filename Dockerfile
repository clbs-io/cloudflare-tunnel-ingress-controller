# syntax=docker/dockerfile:1
ARG ALPINE_VERSION=3.20

FROM --platform=$BUILDPLATFORM golang:1.22-alpine${ALPINE_VERSION} AS builder
ARG TARGETOS TARGETARCH
ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-X 'main.Version=$VERSION'" -o cloudflare-tunnel-ingress-controller ./cmd/controller

FROM alpine:${ALPINE_VERSION} AS main

COPY --from=builder /app/cloudflare-tunnel-ingress-controller /cloudflare-tunnel-ingress-controller

ENTRYPOINT ["/cloudflare-tunnel-ingress-controller"]
