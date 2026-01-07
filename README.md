# unifi-port-forwarder

Kubernetes controller for automatically configuring router settings to port forward services. 

A wise man once said that writing automation for managing ones router would be a fools errand. I wholeheartedly agree, but it does not change the fact that I also am a fool.

It has always been a wish of mine to learn how to implement kubernetes controllers. Once I realised UCG Max supports BGP and could be used to roll out metallb to automate IP allocation on a different subnet in my cluster, I had the perfect reason to investigate.

This controller will look for any `LoadBalancer` objects annotated with `unifi-port-forwarder/ports`. It will inspect the currently provisioned Port Forward rules on the Unifi router, either updating or ensuring that port forward rules match with the service object spec and annotation rule.

The controller does not delete other rules (as long as they don't use conflicting names) and has a small footprint.

## Features

- **Multi-port support**: Configure multiple ports for a single service
- **Port name-based mapping**: Use service port names for clear configuration
- **Port conflict detection**: Prevents external port conflicts across services
- **Protocol detection**: Automatically reads protocol from service definition
- **Individual port logging**: Detailed logging for each port operation
- **Graceful error handling**: Continue processing other ports if one fails

## Supported Routers

- Unifi Cloud Gateway Max
- Probably other Unifi routers like UDM, but YMMV. I have neither tested nor plan to add support for other variants.

## Usage

#### Single Port
```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-service
  annotations:
    unifi-port-forwarder/ports: "http:8080"
spec:
  selector:
    app: web-service
  ports:
  - name: http
    port: 80
    targetPort: 8080
  type: LoadBalancer
```

#### Multiple Ports
```yaml
apiVersion: v1
kind: Service
metadata:
  name: full-service
  annotations:
    unifi-port-forwarder/ports: "http:8080,https:8443,metrics:9090"
spec:
  selector:
    app: full-service
  ports:
  - name: http
    port: 80
    targetPort: 8080
  - name: https
    port: 443
    targetPort: 8443
  - name: metrics
    port: 9090
    targetPort: 9090
  type: LoadBalancer
```

#### Default Port Mapping
```yaml
# Use service port as external port
unifi-port-forwarder/ports: "http,https,metrics"
```

#### Mixed Mapping
```yaml
# Some custom, some default
unifi-port-forwarder/ports: "http:8080,https,metrics:9090"
```

### Annotation Format

- **Port Name**: Must match a port name in the service definition
- **External Port**: Optional, defaults to service port if not specified
- **Multiple Ports**: Comma-separated list of port mappings
- **Protocol**: Automatically read from service definition (TCP/UDP)

### Examples

#### Web Service with HTTP and HTTPS
```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-service
  namespace: production
  annotations:
    unifi-port-forwarder/ports: "http:80,https:443"
spec:
  selector:
    app: web-service
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 8080
  - name: https
    port: 443
    protocol: TCP
    targetPort: 8443
  type: LoadBalancer
```

#### Application with Multiple Ports
```yaml
apiVersion: v1
kind: Service
metadata:
  name: app-service
  namespace: development
  annotations:
    unifi-port-forwarder/ports: "web:3000,api:8080,metrics:9090"
spec:
  selector:
    app: app-service
  ports:
  - name: web
    port: 3000
    protocol: TCP
    targetPort: 3000
  - name: api
    port: 8080
    protocol: TCP
    targetPort: 8080
  - name: metrics
    port: 9090
    protocol: TCP
    targetPort: 9090
  type: LoadBalancer
```

## Behavior

### Services Without Annotation
Services without the `unifi-port-forwarder/ports` annotation are **completely skipped** - no port forwarding rules are created.

### Port Conflict Detection
The controller prevents external port conflicts across different services. If two services try to use the same external port, the second service will fail with an error message.

### Error Handling
- **Individual port failures**: If one port fails to configure, the controller continues with other ports
- **Detailed logging**: Each port operation is logged individually for debugging
- **Graceful degradation**: Partial success is better than complete failure

### Migration from Single-Port

If you have existing single-port services:

1. **Add annotation**: Add `unifi-port-forwarder/ports: "servicename:externalport"`
2. **Port name**: Use the service port name from your service definition
3. **External port**: Specify the desired external port

## Development

## CLI Commands

The `unifi-port-forwarder` provides four commands:

### controller (default)
Run Kubernetes controller for automatic port forwarding:
```bash
./unifi-port-forwarder controller
# or simply
./unifi-port-forwarder
```

### debug
Monitor Kubernetes services for debugging purposes:
### service-debugger
Monitor Kubernetes services for IP changes and debug LoadBalancer IP issues:
```bash
./unifi-port-forwarder service-debugger --namespace=default --log-level=debug
./unifi-port-forwarder service-debugger -labels="app=web" --output=json
```

### clean
Clean up specific port forwarding rules:
```bash
./unifi-port-forwarder clean --port-mappings="83:192.168.27.130"
```

For detailed cleaner documentation, see [cmd/cleaner/README.md](cmd/cleaner/README.md).

For detailed service-debugger documentation, see [cmd/service-debugger/README.md](cmd/service-debugger/README.md).

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
- `UNIFI_SITE`: UniFi site name (default: default)

### Kubernetes Installation

**Prerequisites**
- Create the namespace: `kubectl create namespace unifi-port-forwarder`

**Customize Environment Variables**
Edit `manifests/deployment.yaml` and update the environment variables in the container spec:
- `UNIFI_ROUTER_IP`: IP address of your UniFi router
- `UNIFI_USERNAME`: Username for router access
- `UNIFI_PASSWORD`: Password for router access

**Deploy the Controller**
```bash
kubectl apply -f manifests/
```

This will deploy:
- The controller deployment
- Service account with necessary permissions
- RBAC rules for service monitoring and updates

## Troubleshooting

### Common Issues

1. **Port conflicts**: Check for duplicate external ports across services
2. **Missing annotation**: Services without annotation are skipped
3. **Invalid port names**: Ensure port names exist in service definition
4. **Router connectivity**: Verify router IP and credentials

### Debug Logging

The controller provides detailed debug output:
- Service discovery and processing
- Individual port operations
- Port conflict detection
- Error details with context

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) file for details
