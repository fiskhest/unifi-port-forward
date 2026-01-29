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

# Examples
- [Annotation-based: single rule](single-rule.yaml)
- [Annotation-based: multi rule](multi-rule.yaml)
- [CRD: portforwardrule-serviceref.yaml](crds/portforwardrule-serviceref.yaml)
- [CRD: portforwardrule-standalone.yaml](crds/portforwardrule-standalone.yaml)


# Behavior

## Port Conflict Detection
The controller prevents external port conflicts across different services. If two services try to use the same external port, the second service will fail with an error message.

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
