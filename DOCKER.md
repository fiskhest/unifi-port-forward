# Docker Build Configuration

This repository uses Docker Buildx with bake files for optimized container builds.

## Quick Start

### Build Locally
```bash
# Build all targets (default: controller)
docker buildx bake

# Build only the controller
docker buildx bake controller

# Build with custom tag
docker buildx bake --set controller.tags=johrad/unifi-port-forward:v1.0.0

# Build and push to registry
docker buildx bake --push
```

### Prerequisites
- Docker with Buildx enabled
- `docker buildx create --use` (if not using default builder)

## Build Targets

### controller
- **Image**: `johrad/unifi-port-forward:latest`
- **Architecture**: `linux/amd64`

## Build Features

- **Multi-stage**: Efficient layer caching
- **Distroless**: Minimal runtime image for security
- **Optimized**: Stripped binaries for smaller size
- **Non-root**: Runs as user 65532:65532
- **Caching**: BuildKit mounts for faster rebuilds

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
