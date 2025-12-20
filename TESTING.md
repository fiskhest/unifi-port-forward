# Test Suite for kube-router-port-forward

This directory contains a comprehensive test suite for the kube-router-port-forward controller that verifies automatic router port forwarding configuration for Kubernetes LoadBalancer services.

## Test Structure

### Controller Tests (`controller/`)
Tests the core controller reconciliation logic and change detection:

#### `portforward_controller_test.go`
- `TestPortForwardReconciler_Init` - Tests controller initialization and cache setup
- `TestPortForwardReconciler_IPChange_UpdateCorrectly` - Tests IP change handling in port forwards
- `TestPortForwardReconciler_shouldProcessService` - Tests service annotation validation logic
- `TestPortForwardReconciler_ParseIntField` - Tests integer parsing helper functions

#### `simple_test.go`
- `TestChangeDetection_IPChange` - Tests IP change detection logic
- `TestChangeDetection_AnnotationChange` - Tests annotation change detection
- `TestChangeDetection_SpecChange` - Tests service specification change detection
- `TestChangeAnalysis_PortChanges` - Tests port change analysis logic
- `TestCalculateDelta_*` - Tests delta calculation for CREATE/UPDATE/DELETE scenarios
- `TestRuleBelongsToService_*` - Tests service rule ownership logic

### Helper Function Tests (`helpers/helpers_test.go`)
Tests utility functions and port configuration logic:
- `TestGetLBIP` - Tests LoadBalancer IP extraction from services
- `TestMultiPortService_ValidAnnotation` - Tests multi-port service configuration
- `TestServiceWithoutAnnotation_Skipped` - Tests that services without annotation are ignored
- `TestInvalidAnnotationSyntax_Error` - Tests invalid annotation syntax handling
- `TestPortNameNotFound_Error` - Tests non-existent port name handling
- `TestPortConflictDetection_Error` - Tests port conflict detection
- `TestDefaultPortMapping` - Tests default port mapping behavior

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
- `mock_router.go` - Enhanced Mock router with full Router interface implementation
- `mock_unifi.go` - Mock UniFi client for testing
- `mock_test_clock.go` - Mock clock for deterministic time-based testing
- `fake_k8s_client.go` - Fake Kubernetes client and service utilities
- `controller_test_helpers.go` - Controller test environment setup utilities

## Running Tests

### Run All Tests
```bash
go test ./...
```

### Run Specific Test Files
```bash
# Controller tests
go test ./controller

# Helper tests
go test ./helpers

# Unit tests
go test ./routers

# Integration tests
go test .

# Service debugger tests
go test ./cmd/service-debugger
```

### Run with Verbose Output
```bash
go test -v ./...
```

### Run with Coverage
```bash
go test -cover ./...
```

### Run with Coverage Report
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Coverage

The test suite covers:

### Add Operations
- ✅ LoadBalancer service creation with `kube-port-forward-controller/ports` annotation
- ✅ Port forward rule creation on router
- ✅ Multi-port support with name-based mapping
- ✅ Service validation (type, annotation, IP)
- ✅ Port conflict detection and prevention
- ✅ Protocol detection (TCP/UDP)
- ✅ Default port mapping (external = service port)

### Update Operations
- ✅ Service port changes (removes old port, adds new port)
- ✅ Annotation addition (adds port forward)
- ✅ Annotation removal (removes port forward)
- ✅ LoadBalancer IP changes
- ✅ No-op updates (same port, same annotation)
- ✅ Multi-port service updates
- ✅ Change detection logic
- ✅ Delta calculation for efficient updates

### Delete Operations
- ✅ Annotated service deletion (removes port forward)
- ✅ Non-annotated service deletion (no router action)
- ✅ Port forward rule cleanup
- ✅ Multiple port cleanup

### Controller Operations
- ✅ Controller initialization and setup
- ✅ Reconciliation logic
- ✅ Service processing validation
- ✅ Error handling and logging
- ✅ Time-based operations (using MockClock)

### Edge Cases
- ✅ Non-LoadBalancer services are ignored
- ✅ Services without annotation are ignored
- ✅ Services with no LoadBalancer IP
- ✅ Services with multiple LoadBalancer IPs
- ✅ Invalid PortConfig validation
- ✅ Port conflict scenarios
- ✅ Invalid annotation syntax
- ✅ Non-existent port names
- ✅ Empty and invalid integer parsing

## Test Scenarios

### 1. Service Addition with Multi-Port
```yaml
apiVersion: v1
kind: Service
metadata:
  name: multi-port-service
  annotations:
    kube-port-forward-controller/ports: "http:8080,https:8443,metrics:9090"
spec:
  type: LoadBalancer
  ports:
  - name: http
    port: 80
    protocol: TCP
  - name: https
    port: 443
    protocol: TCP
  - name: metrics
    port: 9090
    protocol: TCP
status:
  loadBalancer:
    ingress:
    - ip: 192.168.1.100
```
**Expected**: Three port forward rules created:
- External 8080 → 192.168.1.100:80 (http)
- External 8443 → 192.168.1.100:443 (https)  
- External 9090 → 192.168.1.100:9090 (metrics)

### 2. Service Update (Port Change)
```yaml
# Before: http:8080
# After: http:8081
```
**Expected**: Old port 8080 rule removed, new port 8081 rule created

### 3. Service Update (Annotation Removal)
```yaml
# Before: kube-port-forward-controller/ports: "http:8080"
# After: annotation removed
```
**Expected**: Port forward rule removed

### 4. Port Conflict Detection
```yaml
# Service 1
apiVersion: v1
kind: Service
metadata:
  name: service1
  annotations:
    kube-port-forward-controller/ports: "web:8080"
spec:
  type: LoadBalancer
  ports:
  - name: web
    port: 80
  status:
    loadBalancer:
      ingress:
      - ip: 192.168.1.100

# Service 2 (conflicts with Service 1)
apiVersion: v1
kind: Service
metadata:
  name: service2
  annotations:
    kube-port-forward-controller/ports: "api:8080"
spec:
  type: LoadBalancer
  ports:
  - name: api
    port: 3000
  status:
    loadBalancer:
      ingress:
      - ip: 192.168.1.101
```
**Expected**: Service 2 creation fails due to port 8080 conflict

### 5. Default Port Mapping
```yaml
apiVersion: v1
kind: Service
metadata:
  name: default-mapping-service
  annotations:
    kube-port-forward-controller/ports: "http,https"
spec:
  type: LoadBalancer
  ports:
  - name: http
    port: 80
    protocol: TCP
  - name: https
    port: 443
    protocol: TCP
status:
  loadBalancer:
    ingress:
    - ip: 192.168.1.100
```
**Expected**: Port forward rules created with external ports matching service ports:
- External 80 → 192.168.1.100:80 (http)
- External 443 → 192.168.1.100:443 (https)

## Mock Implementation

The tests use enhanced mock implementations to avoid dependencies on:
- Real UniFi controllers
- Real Kubernetes clusters
- Network connectivity
- Time-based race conditions

### MockRouter
- Implements full Router interface
- Simulates UniFi API responses
- Tracks port forward rules in memory
- Validates port conflicts
- Supports failure injection for testing error scenarios
- Call tracking for verification

### MockUniFiClient
- Simulates UniFi API responses
- Tracks port forward rules in memory
- Validates port conflicts
- Supports all required operations

### MockClock
- Provides deterministic time control
- Eliminates race conditions in time-based tests
- Supports timer creation and advancement
- Enables testing of time-dependent logic

### ControllerTestEnv
- Provides complete test environment setup
- Includes mock router, clock, and logger
- Supports service creation and management
- Handles cleanup automatically

### FakeKubernetesClient
- Simulates Kubernetes service operations
- Tracks service state in memory
- Supports service CRUD operations
- Provides test data creation helpers
- Supports VIP mode and modern service features

## Enhanced Features Tested

### Multi-Port Support
- Full support for services with multiple ports
- Port name-based mapping for clarity
- Individual port validation and error handling
- Graceful handling of mixed success/failure scenarios

### Change Detection
- Sophisticated change detection logic
- Separate handling of IP, annotation, and spec changes
- Efficient delta calculation to minimize router operations
- Support for partial updates and rollbacks

### Port Conflict Prevention
- Global port conflict tracking
- Detailed error messages with conflicting service information
- Support for different destination IPs with same external port
- Automatic conflict resolution strategies

### Validation
- Comprehensive annotation syntax validation
- Service port name validation
- Port range validation
- IP address format validation
- Protocol validation (TCP/UDP)

## Contributing

When adding new tests:
1. Follow the existing naming conventions
2. Use the provided test utilities and environments
3. Test both success and failure scenarios
4. Include edge cases and error conditions
5. Update this documentation
6. Ensure adequate test coverage
7. Use MockClock for time-dependent tests
8. Leverage ControllerTestEnv for controller tests

## Dependencies

- `testing` - Go testing framework
- `k8s.io/api/core/v1` - Kubernetes API types
- `k8s.io/apimachinery/pkg/apis/meta/v1` - Kubernetes meta types
- `sigs.k8s.io/controller-runtime` - Kubernetes controller framework
- `github.com/filipowm/go-unifi` - UniFi client library
- `go.uber.org/mock/gomock` - Mock generation (if using gomock)

## Test Data

### Test Services
- LoadBalancer services with multi-port annotations
- ClusterIP services (should be ignored)
- Services with multiple ports and complex annotations
- Services with no LoadBalancer IP
- Services with multiple LoadBalancer IPs
- Services with VIP mode enabled

### Test Port Configurations
- TCP and UDP protocols
- Valid and invalid port ranges
- Valid and invalid IP addresses
- Various interface configurations
- Port conflict scenarios
- Default and custom port mappings

### Test Scenarios
- Service lifecycle operations (Add/Update/Delete)
- Change detection and delta calculation
- Error handling and recovery
- Concurrent operations
- Time-dependent operations
- Large-scale service management