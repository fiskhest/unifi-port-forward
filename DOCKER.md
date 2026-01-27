# Docker Build Configuration

This repository uses standard Docker builds with optimized multi-stage Dockerfiles.

## Quick Start

### Build Locally
```bash
# Build and push to registry (recommended)
just build

# Or use Docker directly
docker build --push -t johrad/unifi-port-forward .

# Build with custom tag
docker build --push -t your-username/unifi-port-forward:v1.0.0 .
```

### Prerequisites
- Docker (any recent version with multi-stage build support)

## Build Targets

### controller
- **Image**: `johrad/unifi-port-forward:latest`
- **Architecture**: `linux/amd64`

## Build Features

- **Multi-stage**: Efficient layer caching and smaller final image
- **Distroless**: Minimal runtime image for security
- **Optimized**: Stripped binaries for smaller size
- **Non-root**: Runs as user 65532:65532
- **Automated**: Push to registry after successful build (TODO: Not yet)

## Environment Variables

The controller supports these environment variables:

- `UNIFI_ROUTER_IP` - UniFi router IP (default: `192.168.1.1`)
- `UNIFI_USERNAME` - UniFi username (default: `admin`)  
- `UNIFI_PASSWORD` - UniFi password (required)
- `UNIFI_SITE` - UniFi site (default: `default`)

## Runtime Example

```bash
docker run --rm \
  -e UNIFI_ROUTER_IP=192.168.1.1 \
  -e UNIFI_USERNAME=admin \
  -e UNIFI_PASSWORD=mypassword \
  johrad/unifi-port-forward:latest
```

## Registry

Images are published to: `johrad/unifi-port-forward`
