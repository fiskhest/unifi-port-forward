# Annotation syntax

#### 1:1 Mapping
```yaml
# Use service port as external port
unifi-port-forward.fiskhe.st/ports: "http"
```
Creates a port forward rule for the servicePort named http using its Port as both WAN and LAN (forwarded) port. Comma separate for more than one port.

#### Mixed Mapping
```yaml
# Some custom, some with 1:1
unifi-port-forward.fiskhe.st/ports: "8080:http,443:https,9090:metrics"
```

#### Defined mapping
```yaml
# Some custom, some with 1:1
unifi-port-forward.fiskhe.st/ports: "8080:http"
```
Creates a port forward rule for WAN port 8080 going to the servicePort named http as LAN (forwarded) port. Comma separate for more than one port.

# Examples
- [single rule][single-rule.yaml]
- [multi rule][multi-rule.yaml]

## Behavior

### Port Conflict Detection
The controller prevents external port conflicts across different services. If two services try to use the same external port, the second service will fail with an error message.

### Error Handling
- **Individual port failures**: If one port fails to configure, the controller continues with other ports
- **Detailed logging**: Each port operation is logged individually for debugging
- **Graceful degradation**: Partial success is better than complete failure

## Development

## CLI Commands

The `unifi-port-forward` provides four commands:

### controller (default)
Run Kubernetes controller for automatic port forwarding:
```bash
./unifi-port-forward controller
# or simply
./unifi-port-forward
```

### debug
Monitor Kubernetes services for debugging purposes:

### cleaner
For detailed cleaner documentation, see [cmd/cleaner/README.md](cmd/cleaner/README.md).

### service-debugger

For detailed service-debugger documentation, see [cmd/service-debugger/README.md](cmd/service-debugger/README.md).
<We can probably delete this section>?
