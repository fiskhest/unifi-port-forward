# unifi-port-forward

Rumour says that the Unifi Cloud Gateway Max finally supports BGP.
A wiser man than I once quipped that automating ones router would be a fools errand. I wholeheartedly agree, but it does not change the fact that I also am a fool. And proud inventor of footguns everywhere.

Kubernetes controllers are fun. This controller will look for any `Gateway`, `LoadBalancer` or `NodePort` objects annotated with `unifi-port-forward.fiskhe.st/mapping`. A mapping is one or more `key:value` pairs (comma-separated) of the externally facing port mapped to the service that the port forward should forward the traffic to. The external side may be a single port, an inclusive range (`27015-27020:game`), or a list.

A `LoadBalancer` service forwards to its ingress IP; a `NodePort` service forwards to a node address and the allocated nodePort. `ClusterIP` is not supported, since a cluster IP is not routable from the router. See [examples/README.md](examples/README.md#service-types) for how the node is chosen and how to pin it.

On first startup, the controller will check all services and then inspect the currently provisioned Port Forward rules on the Unifi router, either updating or ensuring that port forward rules match with the service object spec and annotation rule. Thereafter, it will periodically reconcile on a schedule ensuring router port forward rules weren't brought out of sync by some other means.

The controller does not delete other rules (as long as they don't use conflicting names) and has a small footprint.

## Core Features
- Real-time monitoring of kubernetes LoadBalancer and NodePort services, automatically configuring corresponding port forward rules on a UniFi router.
- Pre-created rules not maintained by this controller **stays untouched** (as long as there are no conflicts). Only manages services with valid annotations.
- Support for multiple rules per service or gateway
- Port ranges and port lists, both in annotations and in the `PortForwardRule` CRD
- `NodePort` services forward to a node address and the allocated nodePort, with the node discovered automatically or pinned via annotation
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
- `UNIFI_SYNC_INTERVAL`: Periodic reconcile interval (default: 15m, minimum 5m)
- `DEBUG`: Enable debug logging (default: false)

Every variable has a matching CLI flag (`--router-ip`, `--api-key`, …). Precedence is flag, then environment variable, then default.

### Authentication

Supply **either** an API key **or** a username and password — not both.

| Using | Set | Leave unset |
|---|---|---|
| API key (recommended) | `UNIFI_API_KEY` | `UNIFI_USERNAME`, `UNIFI_PASSWORD` |
| User/pass | `UNIFI_USERNAME`, `UNIFI_PASSWORD` | `UNIFI_API_KEY` |

With an API key nothing else auth-related is needed. The client library validates username, password and "remember me" as mutually exclusive with an API key, so when `UNIFI_API_KEY` is set the controller clears the other three before building the client — setting them anyway is harmless but has no effect. The key is sent on every request; there is no login step and therefore no session to renew, so a 401 means the key is wrong, revoked, or lacks permission.

API keys require UniFi OS 9.0.108 or newer. Create one under Settings → Admins → *your admin* → Create API Key.

For authenticating, it is recommended to create a dedicated service account. Use role `Admin`, with full control to the network.

### Kubernetes Installation

**Prerequisites**
- A namespace (the default configured in these manifests: `unifi-port-forward`)
- A router with provisioned credentials
- For LoadBalancer services: a functional LoadBalancer implementation that assigns valid IP addresses to Service LoadBalancer objects
- For NodePort services: `get`/`list`/`watch` on `nodes` (granted by `manifests/deployment.yaml`), or the `unifi-port-forward.fiskhe.st/dst-ip` annotation on each service. **Upgrading from an earlier version? Re-apply the manifests to pick up the new node permission.**


**Deploy the annotation based Controller**
Edit `manifests/deployment.yaml`: set `UNIFI_ROUTER_IP` in the container spec, and put your credentials in the `Secret` at the bottom of the file (an `api-key`, or a `username`/`password` pair — delete whichever block you are not using).

```bash
kubectl create namespace unifi-port-forward
kubectl apply -f manifests/deployment.yaml
```

The manifests are namespaced to `unifi-port-forward`; the Secret and ServiceAccount must live in the same namespace as the Deployment.

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
Serviceref makes a mapping to a running kubernetes service, while standalone can be used to create port forward for external services.
With serviceref, both the destination IP and the internal port are resolved from the Service, so `destinationPort` is only used by standalone rules.
``` bash
kubectl apply -f examples/crds/portforwardrule-serviceref.yaml
kubectl apply -f examples/crds/portforwardrule-standalone.yaml
```

`externalPort` (and `destinationPort`) accept a bare number, an inclusive range, or a comma-separated list of either. Ranges and lists must be quoted so they parse as strings:
``` yaml
externalPort: 25565          # single port
externalPort: "27015-27020"  # range
externalPort: "80,443"       # list
```
UniFi accepts at most 15 comma-separated elements per rule. See [examples/crds/portforwardrule-range.yaml](examples/crds/portforwardrule-range.yaml).

## Automated Deployment

This project uses GitHub Actions for continuous integration and automated Docker image deployment to GitHub Container Registry (GHCR).

### CI/CD Pipeline

- **Trigger**: Automatic on push to `main` branch
- **Registry**: `ghcr.io/<owner>/<repo>`, derived from the repository the workflow runs in — `ghcr.io/fiskhest/unifi-port-forward` (public) here, and a fork's own namespace in a fork. Nothing to edit after forking, and nothing fork-specific to strip out of a pull request.
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

This runs `docker buildx bake --push` and publishes to the `REGISTRY` default in
`docker-bake.hcl`. Override it to publish somewhere else — useful from a fork, so
a local build does not try to push to this repository's registry:

```bash
REGISTRY=ghcr.io/you/unifi-port-forward just build
```

The justfile loads a local `.env` (gitignored), so `REGISTRY=ghcr.io/you/unifi-port-forward`
in that file makes the override stick without repeating it.

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
