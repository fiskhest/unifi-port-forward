# Annotation syntax

### 1:1 Mapping
```yaml
# Use service port as external port
unifi-port-forward.fiskhe.st/mapping: "http"
```
Creates a port forward rule for the servicePort named http using its Port as both WAN and LAN (forwarded) port. Comma separate for more than one port.

### Multiple-Mixed Mapping
```yaml
# Some custom, some with 1:1
unifi-port-forward.fiskhe.st/mapping: "8080:http,443:https,9090:metrics"
```

### Defined mapping
```yaml
# Some custom, some with 1:1
unifi-port-forward.fiskhe.st/mapping: "8080:http"
```
Creates a port forward rule for WAN port 8080 going to the servicePort named http as LAN (forwarded) port. Comma separate for more than one port.

### Port range mapping
```yaml
# A whole WAN range to one service port
unifi-port-forward.fiskhe.st/mapping: "27015-27020:game"
```
Creates a *single* port forward rule covering the whole WAN range. The forwarded
ports are a range of the same size starting at the servicePort, so a servicePort
of `27015` forwards `27015-27020 -> 27015-27020` and a servicePort of `30000`
forwards `27015-27020 -> 30000-30005`. The range must not run past port 65535.

### Port list mapping
Commas already separate mappings, so a list is written by pointing several
mappings at the same servicePort name:
```yaml
# WAN 80 and 443 both forwarded to the "https" servicePort
unifi-port-forward.fiskhe.st/mapping: "80:https,443:https"
```
Those coalesce into one rule with WAN ports `80,443`. Because the WAN ports are
not contiguous, they all forward to the single servicePort. UniFi accepts at most
15 comma-separated elements per rule.

### Gateway annotations
Gateway mappings are `Protocol:Port` and take a range on the port side:
```yaml
unifi-port-forward.fiskhe.st/mapping: "TCP:80,UDP:50000-50100"
```
Commas separate mappings here too, so repeat the protocol for several discrete
ports (`"TCP:80,TCP:443"`) - that yields one rule per entry.

# Service types

| Type | Forwards to | Notes |
|---|---|---|
| `LoadBalancer` | ingress IP : service port | The original and simplest case. |
| `NodePort` | node IP : **nodePort** | The node is discovered, or pinned with `dst-ip`. |
| `ClusterIP` | — | Not supported: a cluster IP is not routable from the router. |

For a NodePort service the internal port is the allocated `nodePort`, not the
service port — you never write it down. The external side still comes from the
annotation, and a bare port name still defaults to the *service* port, since that
is the number you think of as the service's port:

```yaml
# WAN 8080 -> <node IP>:31234
unifi-port-forward.fiskhe.st/mapping: "8080:http"

# WAN 80 (the service port) -> <node IP>:31234
unifi-port-forward.fiskhe.st/mapping: "http"
```

Port ranges behave differently on NodePort. nodePorts are allocated one at a time
and are not contiguous, so a range is **not** offset onto `nodePort..nodePort+N`
— every WAN port in the range forwards to the single nodePort instead.

## Picking the node

Addresses resolve in this order, first match wins:

1. the `unifi-port-forward.fiskhe.st/dst-ip` annotation on the Service
2. `.status.loadBalancer.ingress[].ip`, if the service has one
3. `.spec.externalIPs[0]`
4. the lowest-named `Ready` node's `InternalIP`, skipping any node labelled
   `node.kubernetes.io/exclude-from-external-load-balancers`

Step 4 needs `get`/`list`/`watch` on `nodes`, which the shipped manifests grant.
If you would rather not grant it, set `dst-ip` on your NodePort services and
resolution stops at step 1.

The node choice is re-made on every reconcile rather than remembered, so a node
going away heals on the next pass. Node address changes are not watched, so they
are picked up by the periodic drift pass rather than immediately.

## Why not ClusterIP

A cluster IP (`10.96.x.x`) is only routable inside the cluster unless you
advertise the service CIDR to the router over BGP or add static routes. Rather
than silently program a dead rule, annotated ClusterIP services are skipped with
an `UnsupportedServiceType` warning event. If you do have that routing, use a
standalone `PortForwardRule` with an explicit `destinationIP`.

# Examples
- [Annotation-based: single rule](single-rule.yaml)
- [Annotation-based: multi rule](multi-rule.yaml)
- [Annotation-based: port range](port-range.yaml)
- [Annotation-based: NodePort](nodeport.yaml)
- [CRD: portforwardrule-serviceref.yaml](crds/portforwardrule-serviceref.yaml)
- [CRD: portforwardrule-standalone.yaml](crds/portforwardrule-standalone.yaml)
- [CRD: portforwardrule-range.yaml](crds/portforwardrule-range.yaml)


# Behavior

## Port Conflict Detection
The controller prevents external port conflicts across different services. If two services try to use the same external port, the second service will fail with an error message. Ranges and lists are tracked port by port, so a rule claiming `8000-8100` also blocks another service asking for `8050`.

## Manual Rule management
The controller *does not touch already created rules*. If a managed rule is deployed that contains a WAN port that is already provisioned by a manual rule, the controller WILL take over Port Ownership, rename the port to match the managed rule, and use Forward Port as specified by the managed rule.

## Error Handling
- **Individual port failures**: If one port fails to configure, the controller continues with other ports
- **Detailed logging**: Each port operation is logged individually for debugging
- **Graceful degradation**: Partial success is better than complete failure

# Development

## CLI Commands

The `unifi-port-forward` provides two commands:

### controller (default)
Run Kubernetes controller for automatic port forwarding:
```bash
./unifi-port-forward controller
# or simply
./unifi-port-forward
```

### cleaner
For detailed cleaner documentation, see [cmd/cleaner/README.md](cmd/cleaner/README.md).
