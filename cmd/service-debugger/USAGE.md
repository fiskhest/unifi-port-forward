# Service IP Debugger - Usage Examples

## Quick Start

```bash
# Monitor all services in all namespaces
./kube-port-forward-controller service-debugger

# Monitor specific namespace
./kube-port-forward-controller service-debugger -namespace=default

# Monitor services with port forwarding annotations
./kube-port-forward-controller service-debugger -labels="kube-port-forward-controller/ports"
```

## Debugging Transient IP Issues

### 1. Monitor New Service Creation
```bash
# Terminal 1: Start debugger
./kube-port-forward-controller service-debugger -namespace=default -log-level=debug

# Terminal 2: Create a LoadBalancer service
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: test-lb
  annotations:
    kube-port-forward-controller/ports: "http:8080"
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 8080
EOF
```

**Expected Output:**
```
🟢 [2025-01-15T10:30:15Z] CREATED default/test-lb
   IPs: ["192.168.27.130"] (type: loadbalancer)
   LB_STATUS: 1 ingress entries
   ANNOTATIONS: kube-port-forward-controller/ports=true

🔄 [2025-01-15T10:32:45Z] IP_CHANGED default/test-lb
   IP_CHANGE: ["192.168.27.130"] -> ["192.168.72.1"]
   IP_TYPE: loadbalancer -> loadbalancer
   LB_STATUS: 1 ingress entries
   ANNOTATIONS: kube-port-forward-controller/ports=true
```

### 2. Monitor Multiple Services
```bash
# Monitor all services with port forwarding annotations
./kube-port-forward-controller service-debugger -labels="kube-port-forward-controller/ports" -output=json

# This will output JSON for easy parsing and analysis
```

### 3. Track IP Stability
```bash
# Monitor with custom polling interval
./kube-port-forward-controller service-debugger -interval=10s -history=20

# This tracks more history and polls less frequently
```

## Common Scenarios

### Scenario 1: LoadBalancer IP Assignment
```
🟢 [10:30:15] CREATED default/web-app
   IPs: ["192.168.27.130"] (type: loadbalancer)

🔄 [10:31:20] IP_CHANGED default/web-app
   IP_CHANGE: ["192.168.27.130"] -> ["192.168.72.1"]
   IP_TYPE: loadbalancer -> loadbalancer
   ✅ LoadBalancer IP assigned
```

### Scenario 2: Multiple IPs (Warning)
```
🔄 [10:35:10] IP_CHANGED default/multi-ip-service
   IP_CHANGE: ["192.168.72.1"] -> ["192.168.72.1", "192.168.27.130"]
   IP_TYPE: loadbalancer -> multiple
   ⚠️  WARNING: Multiple IPs detected - may cause port forwarding issues
```

### Scenario 3: Service Deletion
```
🔴 [10:40:00Z] DELETED default/web-app
   IP_CHANGE: ["192.168.72.1"] -> []
   IP_TYPE: loadbalancer
   LB_STATUS: 0 ingress entries
   ANNOTATIONS: kube-port-forward-controller/ports=false
```

## Integration with Main Controller

### Correlating Events
1. **Start both controllers:**
   ```bash
   # Terminal 1: Main controller
   ./kube-port-forward-controller
   
   # Terminal 2: Debugger
   ./kube-port-forward-controller service-debugger -namespace=default -log-level=debug
   ```

2. **Create service with port forwarding:**
   ```bash
   kubectl apply -f service-with-ports.yaml
   ```

3. **Observe timing:**
   - Debugger shows IP assignment timeline
   - Main controller logs port forwarding rule creation
   - Identify any timing gaps or issues

### Troubleshooting Workflow

1. **Identify Problem Service:**
   ```bash
   ./kube-port-forward-controller service-debugger -namespace=default | grep "IP_CHANGED"
   ```

2. **Check IP Classification:**
   - Look for `loadbalancer -> loadbalancer` transitions (normal)
   - Flag `multiple` IP types (potential issues)

3. **Monitor Frequency:**
   ```bash
   # Count changes per service
   ./service-debugger -output=json | jq '.name' | sort | uniq -c
   ```

4. **Validate Annotations:**
   - Ensure services with port forwarding have stable LoadBalancer IPs
   - Check annotation presence vs IP changes

## Advanced Usage

### JSON Output for Analysis
```bash
# Export changes for analysis
./kube-port-forward-controller service-debugger -output=json > service-changes.json

# Analyze with jq
cat service-changes.json | jq '
  group_by(.name) | 
  map({
    service: .[0].name,
    changes: length,
    ip_types: map_values(.ip_type) | keys,
    first_seen: .[0].timestamp,
    last_seen: .[-1].timestamp
  })
'
```

### Filter by Multiple Criteria
```bash
# Monitor production services with port forwarding
./kube-port-forward-controller service-debugger \
  -namespace=production \
  -labels="env=prod,kube-port-forward-controller/ports" \
  -log-level=info
```

### Custom Polling for Slow Clusters
```bash
# For clusters with slow LoadBalancer provisioning
./kube-port-forward-controller service-debugger -interval=30s -history=5
```

## Exit Summary

When you stop the debugger (Ctrl+C), it prints a comprehensive summary:

```
=== SERVICE DEBUGGER SUMMARY ===

Service: default/web-app
  Current IPs: ["192.168.72.1"]
  Last Seen: 2025-01-15T10:32:45Z
  Total Changes: 2
  Recent Changes:
    10:30:15: created ( [] -> ["192.168.27.130"])
    10:32:45: ip_changed (["192.168.27.130"] -> ["192.168.72.1"])

=== END SUMMARY ===
```

This summary helps identify:
- Services with frequent IP changes
- Current IP assignments
- Time since last change
- Change patterns

## Tips for Debugging

1. **Start Broad, Then Narrow:**
   - Monitor all services first
   - Filter down to problematic services

2. **Use Debug Mode Initially:**
   - See all service updates, not just IP changes
   - Identify related events

3. **Combine Logs:**
   - Correlate service-debugger output with main controller logs
   - Match timestamps to identify causality

4. **Look for Patterns:**
   - Specific namespaces with issues
   - Times of day with more changes
   - Service types with problems

This debugger provides comprehensive visibility into service IP changes, helping identify and resolve LoadBalancer IP issues in your Kubernetes cluster.