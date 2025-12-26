# Service IP Debugger

A utility to monitor Kubernetes service IP changes and debug LoadBalancer IP issues in the kube-port-forward-controller.

## Usage

```bash
# Monitor all services
./kube-port-forward-controller service-debugger

# Monitor specific namespace
./kube-port-forward-controller service-debugger -namespace=default

# Monitor services with specific labels
./kube-port-forward-controller service-debugger -labels=app=web

# JSON output for parsing
./kube-port-forward-controller service-debugger -output=json

# Debug mode with verbose logging
./kube-port-forward-controller service-debugger -log-level=debug

# Custom polling interval
./kube-port-forward-controller service-debugger -interval=10s
```

## Command Line Options

- `-namespace`: Filter by namespace (empty = all namespaces)
- `-labels`: Filter by labels (e.g., `app=web,env=prod`)
- `-log-level`: Log level (`debug`, `info`, `warn`, `error`)
- `-output`: Output format (`text`, `json`)
- `-history`: Number of changes to track per service (default: 10)
- `-interval`: Polling interval for status checks (default: 5s)

## Output Examples

### Text Format
```
🟢 [2025-01-15T10:30:15Z] CREATED default/web-service
   IPs: ["192.168.27.130"] (type: loadbalancer)
   LB_STATUS: 1 ingress entries
   ANNOTATIONS: kube-port-forward-controller/ports=true

🔄 [2025-01-15T10:32:45Z] IP_CHANGED default/web-service
   IP_CHANGE: ["192.168.27.130"] -> ["192.168.72.1"]
   IP_TYPE: loadbalancer -> loadbalancer
   LB_STATUS: 1 ingress entries
   ANNOTATIONS: kube-port-forward-controller/ports=true
```

### JSON Format
```json
{
  "timestamp": "2025-01-15T10:32:45Z",
  "namespace": "default",
  "name": "web-service",
  "old_ips": ["192.168.27.130"],
  "new_ips": ["192.168.72.1"],
  "change_type": "ip_changed",
  "ip_type": "loadbalancer",
  "num_ingress": 1,
  "has_annotation": true
}
```

## IP Classification

The debugger classifies IPs to help identify LoadBalancer IP states:

- **LoadBalancer**: LoadBalancer IP (used for port forwarding)
- **Multiple**: Multiple IPs (may cause port forwarding issues)
- **Unknown**: No IP or unrecognized format

## Debugging Scenarios

### 1. LoadBalancer IP Monitoring
Monitor services and their LoadBalancer IP assignments:
```bash
./service-debugger -namespace=default -log-level=debug
```

### 2. Port Forwarding Issues
Check if services with port forwarding annotations have stable IPs:
```bash
./service-debugger -labels="kube-port-forward-controller/ports"
```

### 3. Cluster-wide Analysis
Monitor all services for IP stability patterns:
```bash
./service-debugger -output=json > service-changes.json
```

## Integration with Controller

Run the debugger alongside the main controller to correlate IP changes with port forwarding behavior:

1. Start the main controller
2. Start the debugger in another terminal
3. Create/update services with port forwarding annotations
4. Observe the timing between IP changes and port forwarding updates

## Troubleshooting

### Common Issues

**Services stuck with node IPs:**
- Check LoadBalancer configuration
- Verify MetalLB or cloud provider settings
- Look for `⚠️ WARNING` messages in debugger output

**Multiple IPs detected:**
- May indicate LoadBalancer misconfiguration
- Can cause port forwarding rule conflicts
- Review `mixed` IP type warnings

**Rapid IP changes:**
- Indicates cluster instability
- May cause port forwarding rule churn
- Monitor change frequency in summary

### Exit Summary

When stopped with Ctrl+C, the debugger prints a summary of all tracked services:
- Current IPs and types
- Recent change history
- Total number of changes per service

## Permissions

The debugger needs the same RBAC permissions as the main controller:
- Read access to Services
- List access to Services
- Watch access to Services

Deploy with the same service account and RBAC as the main controller.
