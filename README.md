# kube-port-forward-controller

Kubernetes controller for automatically configuring router settings to port forward services. 

This came out of desire to completely provision an external service in my kubernetes homelab, using other services DNS can be automatically configured, as Load Balancer created, but no method to automatically open a port on my router, something frequently done when hosting things like game servers. 

## Features

- **Multi-port support**: Configure multiple ports for a single service
- **Port name-based mapping**: Use service port names for clear configuration
- **Port conflict detection**: Prevents external port conflicts across services
- **Protocol detection**: Automatically reads protocol from service definition
- **Individual port logging**: Detailed logging for each port operation
- **Graceful error handling**: Continue processing other ports if one fails

## Supported Routers

- **Unifi Dream Machine Pro** (Primary supported router)
- PfSense (Planned)
- OPNSense (Planned)
- VyOS (Planned)

## Usage

### Annotation Syntax

The controller uses the `kube-port-forward-controller/ports` annotation to configure port forwarding.

#### Single Port
```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-service
  annotations:
    kube-port-forward-controller/ports: "http:8080"
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
    kube-port-forward-controller/ports: "http:8080,https:8443,metrics:9090"
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
kube-port-forward-controller/ports: "http,https,metrics"
```

#### Mixed Mapping
```yaml
# Some custom, some default
kube-port-forward-controller/ports: "http:8080,https,metrics:9090"
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
    kube-port-forward-controller/ports: "http:80,https:443"
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
    kube-port-forward-controller/ports: "web:3000,api:8080,metrics:9090"
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
Services without the `kube-port-forward-controller/ports` annotation are **completely skipped** - no port forwarding rules are created.

### Port Conflict Detection
The controller prevents external port conflicts across different services. If two services try to use the same external port, the second service will fail with an error message.

### Error Handling
- **Individual port failures**: If one port fails to configure, the controller continues with other ports
- **Detailed logging**: Each port operation is logged individually for debugging
- **Graceful degradation**: Partial success is better than complete failure

### Migration from Single-Port

If you have existing single-port services:

1. **Add annotation**: Add `kube-port-forward-controller/ports: "servicename:externalport"`
2. **Port name**: Use the service port name from your service definition
3. **External port**: Specify the desired external port

## Development

## CLI Commands

The `kube-port-forward-controller` provides four commands:

### controller (default)
Run Kubernetes controller for automatic port forwarding:
```bash
./kube-port-forward-controller controller
# or simply
./kube-port-forward-controller
```

### debug
Monitor Kubernetes services for debugging purposes:
### service-debugger
Monitor Kubernetes services for IP changes and debug LoadBalancer IP issues:
```bash
./kube-port-forward-controller service-debugger --namespace=default --log-level=debug
./kube-port-forward-controller service-debugger -labels="app=web" --output=json
```

### clean
Clean up specific port forwarding rules:
```bash
./kube-port-forward-controller clean --port-mappings="83:192.168.27.130"
```

For detailed cleaner documentation, see [cmd/cleaner/README.md](cmd/cleaner/README.md).

For detailed service-debugger documentation, see [cmd/service-debugger/README.md](cmd/service-debugger/README.md).

## Building and Running

### Building
```bash
go build -o kube-port-forward-controller
```

### Testing
```bash
go test -v
```

### Running
```bash
# Set environment variables
export UNIFI_ROUTER_IP="192.168.1.1"
export UNIFI_USERNAME="admin"
export UNIFI_PASSWORD="password"
export UNIFI_SITE="default"

# Run the controller (default command)
./kube-port-forward-controller
```

## Code Quality

### Formatting
```bash
# Check formatting issues (non-vendor files only)
find . -name "*.go" -not -path "./vendor/*" | xargs gofmt -l

# Auto-fix formatting
gofmt -w .
```

### Linting
```bash
# Quick lint check
golangci-lint run ./...

# Run all linters with maximum issue detection
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./...

# Security-focused linting
golangci-lint run --enable-only=gosec,errcheck,staticcheck ./...

# Auto-fix available issues
golangci-lint run --fix ./...
```

### Pre-commit Check
```bash
# Complete code quality check before committing
gofmt -l .
golangci-lint run ./...
go test -v ./...
```

## Configuration

### Environment Variables
- `UNIFI_ROUTER_IP`: IP address of the router (default: 192.168.27.1)
- `UNIFI_USERNAME`: Router username (default: admin)
- `UNIFI_PASSWORD`: Router password
- `UNIFI_SITE`: UniFi site name (default: default)

### Kubernetes RBAC
The controller needs permission to watch and list services:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-port-forward-controller
rules:
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-port-forward-controller
subjects:
- kind: ServiceAccount
  name: kube-port-forward-controller
  namespace: your-namespace
roleRef:
  kind: ClusterRole
  name: kube-port-forward-controller
```

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

[Add your license information here]
