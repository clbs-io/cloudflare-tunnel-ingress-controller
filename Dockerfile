# syntax=docker/dockerfile:1

ARG ALPINE_VERSION=3.19

FROM golang:1.22-alpine${ALPINE_VERSION} AS builder
ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -ldflags="-X 'main.Version=$VERSION'" -o cloudflare-tunnel-ingress-controller ./cmd/controller

FROM alpine:${ALPINE_VERSION} AS runtime

COPY --from=builder /app/cloudflare-tunnel-ingress-controller /cloudflare-tunnel-ingress-controller

ENTRYPOINT ["/cloudflare-tunnel-ingress-controller"]
