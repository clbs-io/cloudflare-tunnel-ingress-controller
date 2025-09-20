# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25.1-alpine AS builder
ARG TARGETOS TARGETARCH
ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-X 'main.Version=$VERSION'" -o cloudflare-tunnel-ingress-controller ./cmd/controller

FROM alpine:3.22.1 AS main

COPY --from=builder /app/cloudflare-tunnel-ingress-controller /cloudflare-tunnel-ingress-controller

ENTRYPOINT ["/cloudflare-tunnel-ingress-controller"]
