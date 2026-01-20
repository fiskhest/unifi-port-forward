# unifi-port-forwarder

Rumour says that the Unifi Cloud Gateway Max finally supports BGP.
A wiser man than I once quipped that automating ones router would be a fools errand. I wholeheartedly agree, but it does not change the fact that I also am a fool. And proud inventor of footguns everywhere.

Kubernetes controllers are fun. This controller will look for any `LoadBalancer` objects annotated with `unifi-port-forwarder/ports`. On first startup, it will check all services and then inspect the currently provisioned Port Forward rules on the Unifi router, either updating or ensuring that port forward rules match with the service object spec and annotation rule. Thereafter, it will periodically reconcile on a schedule ensuring router port forward rules weren't brought out of sync by some other means.

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

### Kubernetes Installation

**Prerequisites**
- A namespace
- A router with provisioned credentials
- A functional LoadBalancer implementation that assigns valid IP addresses to Service LoadBalancer objects

**Customize Environment Variables**
Edit `manifests/deployment.yaml` and update the environment variables in the container spec.

**Deploy the Controller**
```bash
kubectl apply -f manifests/
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
