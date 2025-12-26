# Test Suite for kube-port-forward-controller

This directory contains a comprehensive test suite for the kube-port-forward-controller that verifies automatic router port forwarding configuration for Kubernetes LoadBalancer services.

## Testing Philosophy

The test suite is organized into two main categories:

### Unit Tests
Isolated tests of individual functions and components without external dependencies. These include helper functions, change detection logic, and delta calculation algorithms.

### Integration Tests
Tests that verify complete workflows and interactions between multiple components. These include controller reconciliation, finalizer workflows, and end-to-end service lifecycle scenarios.

## Test Structure

### Controller Tests (`controller/`)
Tests the core controller reconciliation logic, change detection, and finalizer workflows:

#### `change_detection_test.go`
- `TestChangeDetection_IPChange` - Tests IP change detection logic
- `TestChangeDetection_AnnotationChange` - Tests annotation change detection logic
- `TestChangeDetection_SpecChange` - Tests service specification change detection logic
- `TestChangeAnalysis_PortChanges` - Tests port change analysis logic

#### `finalizer_test.go`
- `TestFinalizerAddition` - Tests that finalizers are added to managed services
- `TestFinalizerRemoval` - Tests that finalizers are removed from non-managed services
- `TestFinalizerCleanupSuccess` - Tests successful cleanup during service deletion
- `TestFinalizerCleanupRetry` - Tests retry logic during cleanup failure
- `TestFinalizerMaxRetries` - Tests behavior when max retries are exceeded
- `TestServiceWithoutFinalizerDeletion` - Tests deletion of services without finalizers (backward compatibility)

#### `finalizer_integration_test.go`
- `TestFinalizerWorkflowIntegration` - Tests the complete finalizer workflow
- `TestFinalizerBackwardCompatibility` - Tests backward compatibility with services without finalizers
- `TestFinalizerErrorRecovery` - Tests recovery from cleanup failures

#### `reconciler_test.go`
- `TestReconcile_RealServiceCreation` - Tests actual Reconcile method with service creation
- `TestReconcile_ServiceUpdate_PortChange` - Tests port changes trigger rule updates
- `TestReconcile_ServiceDeletion` - Tests service deletion triggers cleanup
- `TestReconcile_NonLoadBalancer_Ignored` - Tests that ClusterIP services are ignored
- `TestReconcile_NoAnnotation_Ignored` - Tests that services without annotation are ignored
- `TestReconcile_NoLBIP_Ignored` - Tests that services without LoadBalancer IP are ignored

#### `unified_operations_test.go`
- `TestCalculateDelta_CreationScenario` - Tests delta calculation for new port creation
- `TestCalculateDelta_UpdateScenario` - Tests delta calculation for existing rule update
- `TestCalculateDelta_DeletionScenario` - Tests delta calculation for port deletion

### Helper Function Tests (`helpers/helpers_test.go`)
Tests utility functions and port configuration logic:
- `TestGetLBIP` - Tests the GetLBIP helper function
- `TestMultiPortService_ValidAnnotation` - Tests multi-port service with valid annotation
- `TestServiceWithoutAnnotation_Skipped` - Tests that services without annotation are skipped
- `TestInvalidAnnotationSyntax_Error` - Tests invalid annotation syntax
- `TestPortNameNotFound_Error` - Tests annotation with non-existent port name
- `TestPortConflictDetection_Error` - Tests port conflict detection
- `TestDefaultPortMapping` - Tests default port mapping (external = service port)

### Router Tests (`routers/unifi_test.go`)
Tests the core router functionality:
- `TestPortConfig_Validation` - Tests the PortConfig struct validation
- `TestRouter_Interface` - Tests the Router interface contract

### Test Utilities (`testutils/`)
Advanced mock implementations and testing frameworks:
- `mock_router.go` - Enhanced Mock router with full Router interface implementation
- `mock_unifi.go` - Mock UniFi client for testing
- `mock_test_clock.go` - Mock clock for deterministic time-based testing
- `fake_k8s_client.go` - Fake Kubernetes client and service utilities
- `controller_test_helpers.go` - Controller test environment setup utilities
- `event_utils.go` - Event testing utilities for controller-runtime integration

## Running Tests

### Run All Tests
```bash
go test -v ./...
```

### Run Specific Test Categories
```bash
# Controller tests
go test -v ./controller

# Helper tests
go test -v ./helpers

# Router tests
go test -v ./routers
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
- ✅ Finalizer-based cleanup blocking
- ✅ Retry logic for failed cleanups

### Controller Operations
- ✅ Controller initialization and setup
- ✅ Real Reconcile method testing with controller-runtime integration
- ✅ Service processing validation
- ✅ Error handling and logging
- ✅ Time-based operations (using MockClock)
- ✅ Finalizer workflow management
- ✅ Backward compatibility with existing services

### Finalizer Workflow Testing (NEW)
- ✅ Finalizer addition for managed services
- ✅ Finalizer removal after successful cleanup
- ✅ Cleanup retry logic with exponential backoff
- ✅ Max retry limit enforcement
- ✅ Error recovery from cleanup failures
- ✅ Backward compatibility with services without finalizers
- ✅ Complete workflow integration testing

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
- ✅ Finalizer stuck scenarios
- ✅ Service deletion without cleanup

## Finalizer Workflow Testing

The controller uses Kubernetes finalizers to guarantee cleanup of port forwarding rules when services are deleted. This critical feature is comprehensively tested:

### Finalizer Lifecycle Testing
- **Addition**: Finalizers are automatically added to services that should be managed
- **Blocking**: Service deletion is blocked until port forwarding rules are cleaned up
- **Removal**: Finalizers are removed only after successful cleanup
- **Retry**: Failed cleanup triggers retry logic with configurable backoff

### Error Recovery Testing
- **Cleanup Failures**: Tests router API failures during cleanup
- **Retry Logic**: Verifies exponential backoff and max retry limits
- **Partial Cleanup**: Handles scenarios where some ports fail to clean up
- **Finalizer Recovery**: Tests recovery from stuck finalizer scenarios

### Backward Compatibility Testing
- **Existing Services**: Services without finalizers are handled gracefully
- **Migration**: Existing services get finalizers on first reconciliation
- **Cleanup**: Services deleted without finalizers still get cleanup attempts

## Mock Implementation

The tests use enhanced mock implementations to avoid dependencies on:
- Real UniFi controllers
- Real Kubernetes clusters
- Network connectivity
- Time-based race conditions

### ControllerTestEnv
- Provides complete test environment setup
- Includes mock router, clock, and logger
- Supports service creation and management
- Handles cleanup automatically
- Provides failure injection capabilities

### MockRouter
- Implements full Router interface
- Simulates UniFi API responses
- Tracks port forward rules in memory
- Validates port conflicts
- Supports failure injection for testing error scenarios
- Provides call tracking for verification

### MockUniFiClient
- Simulates UniFi API responses
- Tracks port forward rules in memory
- Validates port conflicts
- Supports all required operations
- Provides configurable error scenarios

### MockClock
- Provides deterministic time control
- Eliminates race conditions in time-based tests
- Supports timer creation and advancement
- Enables testing of time-dependent logic
- Critical for testing retry logic and timeouts

### FakeKubernetesClient
- Simulates Kubernetes service operations
- Tracks service state in memory
- Supports service CRUD operations
- Provides test data creation helpers
- Supports VIP mode and modern service features
- Handles finalizer operations correctly

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
- Port change analysis with detailed tracking

### Port Conflict Prevention
- Global port conflict tracking
- Detailed error messages with conflicting service information
- Support for different destination IPs with same external port
- Automatic conflict detection and prevention

### Validation
- Comprehensive annotation syntax validation
- Service port name validation
- Port range validation
- IP address format validation
- Protocol validation (TCP/UDP)

## Testing Best Practices

### ControllerTestEnv Usage
```go
env := NewControllerTestEnv(t)
defer env.Cleanup()

service := env.CreateTestService("default", "test-service", 
    map[string]string{config.FilterAnnotation: "http:8080"},
    []corev1.ServicePort{{Name: "http", Port: 80}},
    "192.168.1.100")
```

### Finalizer Testing Patterns
- Always test both addition and removal scenarios
- Include retry logic testing with MockClock
- Test error recovery and cleanup failure scenarios
- Verify backward compatibility with existing services

### Mock Infrastructure Usage
- Use ControllerTestEnv for controller integration tests
- Leverage MockClock for time-dependent testing
- Use failure injection to test error scenarios
- Verify call tracking for expected interactions

## Recommended Test Enhancements

### Missing Integration Tests
- **End-to-End Service Lifecycle**: Create service with port forwarding → Update service → Delete service → Verify complete cleanup
- **Multiple Service Interaction**: Test multiple services with overlapping ports and potential conflicts
- **Controller Restart Scenarios**: Test controller restart with existing managed services
- **Router Connectivity Failures**: Test behavior when router becomes unavailable during operations

### Performance and Load Testing
- **Concurrent Operations**: Multiple services created/updated/deleted simultaneously
- **Large-Scale Testing**: Test with hundreds of services and port forwards
- **Memory Usage**: Monitor memory consumption during large operations
- **API Rate Limiting**: Test behavior under rate-limited router API conditions

### Command-Line Tool Testing
- **cmd/cleaner**: Test port forwarding cleanup functionality
- **cmd/service-debugger**: Test service-specific debugging functionality

### Enhanced Edge Case Testing
- **Network Partitions**: Test behavior during network connectivity issues
- **Router API Failures**: Test various API error scenarios and recovery
- **Kubernetes API Failures**: Test behavior with API server unavailability
- **Resource Exhaustion**: Test behavior when router resources are exhausted
- **Configuration Errors**: Test invalid controller configuration scenarios

## Contributing

When adding new tests:

1. **Follow Naming Conventions**: Use `Test` prefix with descriptive function names
2. **Use Test Utilities**: Leverage ControllerTestEnv and existing mock infrastructure
3. **Test Both Success and Failure**: Include both positive and negative test scenarios
4. **Include Edge Cases**: Consider unusual inputs and error conditions
5. **Update Documentation**: Keep this file updated with new tests
6. **Ensure Coverage**: Maintain adequate test coverage for new features
7. **Use MockClock**: For time-dependent tests, use MockClock for determinism
8. **Leverage ControllerTestEnv**: For controller tests, use the provided test environment
9. **Test Finalizer Workflows**: When modifying service lifecycle, test finalizer behavior
10. **Include Integration Tests**: Test complete workflows, not just individual functions

### Quality Standards

- **Unit Tests**: Should be fast, isolated, and test specific functionality
- **Integration Tests**: Should test real workflows and component interactions
- **Error Testing**: Every error path should have corresponding tests
- **Mock Usage**: Use mocks to isolate tests from external dependencies
- **Coverage**: Maintain high test coverage for critical paths
- **Documentation**: Document complex test scenarios and setup requirements