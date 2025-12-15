# Test Suite for kube-port-forward-controller

This directory contains a comprehensive test suite for the kube-port-forward-controller that verifies the Add, Update, Delete operations for the service lifecycle.

## Test Structure

### Unit Tests (`routers/unifi_test.go`)
Tests the core router functionality:
- `TestPortConfig_Validation` - Validates PortConfig struct fields
- `TestRouter_Interface` - Ensures UnifiRouter implements Router interface
- `TestCreateUnifiRouter` - Tests router creation (integration test)

### Integration Tests (`main_test.go`)
Tests the service lifecycle operations:
- `TestServiceLifecycle_AddFunc` - Tests service addition with port forwarding
- `TestServiceLifecycle_UpdateFunc` - Tests service updates (port changes, annotation changes)
- `TestServiceLifecycle_DeleteFunc` - Tests service deletion and port cleanup
- `TestServiceLifecycle_NonLoadBalancer` - Tests that non-LoadBalancer services are ignored
- `TestServiceLifecycle_NoAnnotation` - Tests that services without annotation are ignored
- `TestServiceLifecycle_MultiplePorts` - Tests services with multiple ports
- `TestGetLBIP` - Tests LoadBalancer IP extraction

### Test Utilities (`testutils/`)
- `mock_unifi.go` - Mock UniFi client for testing
- `fake_k8s_client.go` - Fake Kubernetes client and service utilities

## Running Tests

### Run All Tests
```bash
go test ./...
```

### Run Specific Test Files
```bash
# Unit tests
go test ./routers

# Integration tests
go test .
```

### Run with Verbose Output
```bash
go test -v ./...
```

### Run with Coverage
```bash
go test -cover ./...
```

## Test Coverage

The test suite covers:

### Add Operations
- ✅ LoadBalancer service creation with `kube-port-forward-controller/open` annotation
- ✅ Port forward rule creation on router
- ✅ Multiple port handling (first port only, as per current implementation)
- ✅ Service validation (type, annotation, IP)

### Update Operations
- ✅ Service port changes (removes old port, adds new port)
- ✅ Annotation addition (adds port forward)
- ✅ Annotation removal (removes port forward)
- ✅ LoadBalancer IP changes
- ✅ No-op updates (same port, same annotation)

### Delete Operations
- ✅ Annotated service deletion (removes port forward)
- ✅ Non-annotated service deletion (no router action)
- ✅ Port forward rule cleanup

### Edge Cases
- ✅ Non-LoadBalancer services are ignored
- ✅ Services without annotation are ignored
- ✅ Services with no LoadBalancer IP
- ✅ Services with multiple LoadBalancer IPs
- ✅ Invalid PortConfig validation

## Test Scenarios

### 1. Service Addition
```yaml
apiVersion: v1
kind: Service
metadata:
  name: test-service
  annotations:
    kube-port-forward-controller/open: "true"
spec:
  type: LoadBalancer
  ports:
  - port: 8080
    protocol: TCP
status:
  loadBalancer:
    ingress:
    - ip: 192.168.1.100
```
**Expected**: Port forward rule created for port 8080 → 192.168.1.100

### 2. Service Update (Port Change)
```yaml
# Before: port 8080
# After: port 9090
```
**Expected**: Old port 8080 rule removed, new port 9090 rule created

### 3. Service Update (Annotation Removal)
```yaml
# Before: kube-port-forward-controller/open: "true"
# After: annotation removed
```
**Expected**: Port forward rule removed

### 4. Service Deletion
```yaml
# Service with annotation is deleted
```
**Expected**: Port forward rule removed

## Mock Implementation

The tests use mock implementations to avoid dependencies on:
- Real UniFi controllers
- Real Kubernetes clusters
- Network connectivity

### MockUniFiClient
- Simulates UniFi API responses
- Tracks port forward rules in memory
- Validates port conflicts
- Supports all required operations

### FakeKubernetesClient
- Simulates Kubernetes service operations
- Tracks service state in memory
- Supports service CRUD operations
- Provides test data creation helpers

## Future Enhancements

### TODO Items from Code
1. **Multiple Port Support**: Currently only handles first port in service
2. **Port Conflict Detection**: Enhanced validation for existing port forwards
3. **Error Handling**: More comprehensive error scenarios
4. **Integration Tests**: Real UniFi controller integration

### Additional Test Scenarios
1. **Concurrent Operations**: Multiple service changes simultaneously
2. **Network Failures**: Router connectivity issues
3. **Authentication Failures**: Invalid UniFi credentials
4. **Rate Limiting**: UniFi API rate limiting
5. **Service Rollbacks**: Failed operations and recovery

## Test Data

### Test Services
- LoadBalancer services with annotations
- ClusterIP services (should be ignored)
- Services with multiple ports
- Services with no LoadBalancer IP
- Services with multiple LoadBalancer IPs

### Test Port Configurations
- TCP and UDP protocols
- Valid and invalid port ranges
- Valid and invalid IP addresses
- Various interface configurations

## Contributing

When adding new tests:
1. Follow the existing naming conventions
2. Use the provided test utilities
3. Test both success and failure scenarios
4. Update this documentation
5. Ensure adequate test coverage

## Dependencies

- `testing` - Go testing framework
- `k8s.io/api/core/v1` - Kubernetes API types
- `k8s.io/apimachinery/pkg/apis/meta/v1` - Kubernetes meta types
- `github.com/filipowm/go-unifi` - UniFi client library