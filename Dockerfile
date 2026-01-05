# Multi-stage build for unifi-port-forwarder

# Stage 1: Build controller
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -a -ldflags="-w -s" -o /app/unifi-port-forwarder .

# Stage 2: Distroless runtime
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/unifi-port-forwarder /unifi-port-forwarder
USER 65532:65532
ENTRYPOINT ["/unifi-port-forwarder"]
