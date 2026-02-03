# unifi-port-forward

Rumour says that the Unifi Cloud Gateway Max finally supports BGP.
A wiser man than I once quipped that automating ones router would be a fools errand. I wholeheartedly agree, but it does not change the fact that I also am a fool. And proud inventor of footguns everywhere.

Kubernetes controllers are fun. This controller will look for any `LoadBalancer` objects annotated with `unifi-port-forward.fiskhe.st/mapping`. A mapping is a `key:value` pair of the externally facing port mapped to the service that the port forward should forward the traffic to.

On first startup, the controller will check all services and then inspect the currently provisioned Port Forward rules on the Unifi router, either updating or ensuring that port forward rules match with the service object spec and annotation rule. Thereafter, it will periodically reconcile on a schedule ensuring router port forward rules weren't brought out of sync by some other means.

The controller does not delete other rules (as long as they don't use conflicting names) and has a small footprint.

## Core Features
- Real-time monitoring of kubernetes LoadBalancer services, automatically configuring corresponding port forward rules on a UniFi router.
- Pre-created rules not maintained by this controller **stays untouched** (as long as there are no conflicts). Only manages services with valid annotations.
- Support for multiple rules per service
- Periodic reconciliation for state drift detection
- Publishes kubernetes events for improved observability
- Can create port forwards using CRDs for services that are managed outside of kubernetes
- Configurable with environment variables
- Detailed error handling and logging on the controller pod
- Graceful service deletion with finalizer-based cleanup

## Supported Routers

- UniFi Cloud Gateway Max (the only one tested)
- Likely compatible with other UniFi routers (UDM, etc.), but YMMV.
- This was neither tested for, nor is there a bigger plan for adding support for other variants.

## Usage

see [examples/README.md](examples/README.md) for more info


### Pre-commit Check
```bash
just check
```

or individually

```bash
just fmt
just lint
just test
```

## Configuration

### Environment Variables
- `UNIFI_ROUTER_IP`: IP address of the router (default: 192.168.1.1)
- `UNIFI_USERNAME`: Router username (default: admin)
- `UNIFI_PASSWORD`: Router password
- `UNIFI_API_KEY` : API key instead of user/pass. Untested(!)
- `UNIFI_SITE`: UniFi site name (default: default)

For authenticating, it is recommended to create a dedicated service account. Use role `Admin`, with full control to the network.

### Kubernetes Installation

**Prerequisites**
- A namespace (the default configured in these manifests: `unifi-port-forward`)
- A router with provisioned credentials
- A functional LoadBalancer implementation that assigns valid IP addresses to Service LoadBalancer objects


**Deploy the annotation based Controller**
Edit `manifests/deployment.yaml` and update the environment variables in the container spec.

```bash
kubectl apply -f manifests/deployment.yaml
```

**Test the controller by provisioning a test service**
``` bash
kubectl apply -f manifests/test-service.yaml
```

If everything works, you should see a new port forward rule added on the router:
- Name: `unifi-port-forward/test-service9090-80:http`
- WAN Port: `9090`
- Forward Port: `80`
- Forward IP: the IP allocated to the LoadBalancer service

**Deploy CRDs**  
Optionally, one can install Custom Resource Definition (CRD)
`portforwardrules.unifi-port-forward.fiskhe.st`.

``` bash
kubectl apply -f manifests/crd
```

There are two types of CRD-based port forward rules, serviceref and standalone.
Serviceref makes a mapping to a running kubernetes service, while standalone can be used to create port forward for external services
``` bash
kubectl apply -f examples/crds/portforwardrule-serviceref.yaml
kubectl apply -f examples/crds/portforwardrule-standalone.yaml
```

## Automated Deployment

This project uses GitHub Actions for continuous integration and automated Docker image deployment to GitHub Container Registry (GHCR).

### CI/CD Pipeline

- **Trigger**: Automatic on push to `main` branch
- **Registry**: `ghcr.io/fiskhest/unifi-port-forward` (public)
- **Tagging**: 
  - `latest` - Always points to the latest build
  - `YYYY-MM-DD-commit` - Unique tag per build (e.g., `2025-01-30-a1b2c3d`)

### Deployment Commands

```bash
# Using latest tag (recommended)
kubectl set image deployment/unifi-port-forward controller=ghcr.io/fiskhest/unifi-port-forward:latest

# Using specific date tag for rollback
kubectl set image deployment/unifi-port-forward controller=ghcr.io/fiskhest/unifi-port-forward:2025-01-30-a1b2c3d
```

### Local Development

Build and push locally using the justfile:
```bash
just build
```

## Contributing

Issues may be addressed, but no guarantees can be given.
I am reluctant on increasing the feature complexity/scope of this project.
PRs might get reviewed.
Forking is welcome.

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

Potential use cases that might be added in the future:
- add a configurable "policy" = sync / upsert-only / create-only?
- Support for Service NodePort Objects / no load balancer implemented

## License

MIT License - see [LICENSE](LICENSE) file for details
