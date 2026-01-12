package testutils

import (
	"context"
	"strings"
	"testing"

	"unifi-port-forwarder/pkg/config"
	"unifi-port-forwarder/pkg/helpers"

	"github.com/filipowm/go-unifi/unifi"
	v1 "k8s.io/api/core/v1"
)

// TestMultiPortService_ValidAnnotation tests multi-port service with valid annotation
func TestMultiPortService_ValidAnnotation(t *testing.T) {
	// Clear port tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create a multi-port service with annotation
	service := CreateTestMultiPortService(
		"multi-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 8080, Protocol: v1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: v1.ProtocolTCP},
			{Name: "metrics", Port: 9090, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"8080:http,8443:https,9090:metrics",
	)

	// Test getPortConfigs function
	lbIP := helpers.GetLBIP(service)
	portConfigs, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		t.Fatalf("Failed to get port configs: %v", err)
	}

	// Verify we got 3 port configs
	if len(portConfigs) != 3 {
		t.Errorf("Expected 3 port configs, got %d", len(portConfigs))
	}

	// Verify external port mappings
	expectedMappings := map[string]int{
		"http":    8080,
		"https":   8443,
		"metrics": 9090,
	}

	for _, pc := range portConfigs {
		portName := strings.TrimPrefix(pc.Name, "default/multi-service:")
		expectedPort, exists := expectedMappings[portName]
		if !exists {
			t.Errorf("Unexpected port name: %s", pc.Name)
		}
		if pc.DstPort != expectedPort {
			t.Errorf("Expected external port %d for %s, got %d", expectedPort, portName, pc.DstPort)
		}
		if pc.FwdPort != int(helpers.GetServicePortByName(service, portName).Port) {
			t.Errorf("Expected internal port %d for %s, got %d", helpers.GetServicePortByName(service, portName).Port, portName, pc.FwdPort)
		}
	}
}

// TestServiceWithoutAnnotation_Skipped tests that services without annotation are skipped
func TestServiceWithoutAnnotation_Skipped(t *testing.T) {
	// Create a service without annotation
	service := CreateTestMultiPortService(
		"no-annotation-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"", // No annotation
	)

	// Test getPortConfigs function - should return error
	lbIP := helpers.GetLBIP(service)
	_, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err == nil {
		t.Error("Expected error for service without annotation")
	}

	if !strings.Contains(err.Error(), "no port annotation found") {
		t.Errorf("Expected 'no port annotation found' error, got: %v", err)
	}
}

// TestInvalidAnnotationSyntax_Error tests invalid annotation syntax
func TestInvalidAnnotationSyntax_Error(t *testing.T) {
	// Create a service with invalid annotation
	service := CreateTestServiceWithInvalidAnnotation(
		"invalid-service",
		"default",
		"192.168.1.100",
		"http:invalid_port",
	)

	// Test getPortConfigs function - should return error
	lbIP := helpers.GetLBIP(service)
	_, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err == nil {
		t.Error("Expected error for invalid annotation syntax")
	}

	if !strings.Contains(err.Error(), "invalid external port") {
		t.Errorf("Expected 'invalid external port' error, got: %v", err)
	}
}

// TestPortNameNotFound_Error tests annotation with non-existent port name
func TestPortNameNotFound_Error(t *testing.T) {
	// Create a service with annotation referencing non-existent port
	service := CreateTestMultiPortService(
		"missing-port-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"8080:nonexistent",
	)

	// Test getPortConfigs function - should return error
	lbIP := helpers.GetLBIP(service)
	_, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err == nil {
		t.Error("Expected error for non-existent port name")
	}

	if !strings.Contains(err.Error(), "non-existent port") {
		t.Errorf("Expected 'non-existent port' error, got: %v", err)
	}
}

// TestPortConflictDetection_Error tests port conflict detection
func TestPortConflictDetection_Error(t *testing.T) {
	// Clear port tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create first service
	service1 := CreateTestMultiPortService(
		"service1",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"8080:http",
	)

	// First service should succeed
	lbIP1 := helpers.GetLBIP(service1)
	_, err1 := helpers.GetPortConfigs(service1, lbIP1, config.FilterAnnotation)
	if err1 != nil {
		t.Errorf("First service should succeed: %v", err1)
	}

	// Create second service with conflicting port
	service2 := CreateTestMultiPortService(
		"service2",
		"default",
		[]TestPort{
			{Name: "web", Port: 8080, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.101",
		"8080:web", // Same external port as service1
	)

	// Second service should fail due to port conflict
	lbIP2 := helpers.GetLBIP(service2)
	_, err2 := helpers.GetPortConfigs(service2, lbIP2, config.FilterAnnotation)
	if err2 == nil {
		t.Error("Expected port conflict error for second service")
	} else {
		t.Logf("Got error: %v", err2)
		if !strings.Contains(err2.Error(), "already used by service") {
			t.Errorf("Expected port conflict error, got: %v", err2)
		}
	}
}

// TestDefaultPortMapping tests default port mapping (external = service port)
func TestDefaultPortMapping(t *testing.T) {
	// Clear port tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create a service with default port mapping
	service := CreateTestMultiPortService(
		"default-service",
		"default",
		[]TestPort{
			{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
			{Name: "https", Port: 443, Protocol: v1.ProtocolTCP},
		},
		"192.168.1.100",
		"http,https", // Default mapping - external = service port
	)

	lbIP := helpers.GetLBIP(service)
	portConfigs, err := helpers.GetPortConfigs(service, lbIP, config.FilterAnnotation)
	if err != nil {
		t.Fatalf("Failed to get port configs: %v", err)
	}

	// Verify external ports match service ports
	for _, pc := range portConfigs {
		portName := strings.TrimPrefix(pc.Name, "default/default-service:")
		servicePort := helpers.GetServicePortByName(service, portName)

		if pc.DstPort != int(servicePort.Port) {
			t.Errorf("Expected external port %d for %s, got %d", servicePort.Port, portName, pc.DstPort)
		}
	}
}

func TestSyncPortTrackingWithRouterSelective_ListingError(t *testing.T) {
	// Clear tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create comprehensive mock router
	mockRouter := NewMockRouter()
	mockRouter.ClearAllPortForwards()
	mockRouter.ResetCallCounts()

	ctx := context.Background()

	// Setup: Simulate ListAllPortForwards failure
	mockRouter.SetSimulatedFailure("ListAllPortForwards", true)

	// Test: skipIfEmpty=true
	err := helpers.SyncPortTrackingWithRouterSelective(ctx, mockRouter, true)

	// Verify: Should return error
	if err == nil {
		t.Error("Expected error when ListAllPortForwards fails")
	}

	if !strings.Contains(err.Error(), "simulated ListAllPortForwards failure") {
		t.Errorf("Expected specific error message, got: %v", err)
	}

	// Should call ListAllPortForwards once before failing
	if mockRouter.GetCallCount("ListAllPortForwards") != 1 {
		t.Errorf("Expected ListAllPortForwards to be called once, got %d", mockRouter.GetCallCount("ListAllPortForwards"))
	}
}

func TestSyncPortTrackingWithRouterSelective_SkipWhenEmpty(t *testing.T) {
	// Clear tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create comprehensive mock router
	mockRouter := NewMockRouter()
	mockRouter.ClearAllPortForwards()
	mockRouter.ResetCallCounts()

	ctx := context.Background()

	// Setup: Only manual rules (no managed format)
	mockRouter.AddPortForwardRule(unifi.PortForward{
		Name:    "manual-rule",
		DstPort: "80",
		FwdPort: "8080",
		Proto:   "tcp",
		Enabled: true,
	})

	// Test: skipIfEmpty=true
	err := helpers.SyncPortTrackingWithRouterSelective(ctx, mockRouter, true)

	// Verify: Should skip sync
	if err != nil {
		t.Errorf("Expected no error when skipping, got: %v", err)
	}

	// Should call ListAllPortForwards once to check for managed rules
	if mockRouter.GetCallCount("ListAllPortForwards") != 1 {
		t.Errorf("Expected ListAllPortForwards to be called once, got %d", mockRouter.GetCallCount("ListAllPortForwards"))
	}

	// Should not perform sync (no additional calls for sync)
	if mockRouter.GetCallCount("AddPort") != 0 || mockRouter.GetCallCount("UpdatePort") != 0 {
		t.Error("Should not perform any port modifications when skipping")
	}
}

func TestSyncPortTrackingWithRouterSelective_SyncWhenNotEmpty(t *testing.T) {
	// Clear tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create comprehensive mock router
	mockRouter := NewMockRouter()
	mockRouter.ClearAllPortForwards()
	mockRouter.ResetCallCounts()

	ctx := context.Background()

	// Setup: Include managed rules in proper format
	mockRouter.AddPortForwardRule(unifi.PortForward{
		Name:    "default/service:http",
		DstPort: "80",
		FwdPort: "8080",
		Proto:   "tcp",
		Enabled: true,
	})

	// Test: skipIfEmpty=true
	err := helpers.SyncPortTrackingWithRouterSelective(ctx, mockRouter, true)

	// Verify: Should perform full sync
	if err != nil {
		t.Errorf("Expected no error during sync, got: %v", err)
	}

	// Should call ListAllPortForwards twice (check + sync)
	if mockRouter.GetCallCount("ListAllPortForwards") != 2 {
		t.Errorf("Expected ListAllPortForwards to be called twice, got %d", mockRouter.GetCallCount("ListAllPortForwards"))
	}

	// Verify sync operation occurred (port tracking should be populated)
	err = helpers.CheckPortConflict(80, "default/service")
	if err != nil {
		t.Errorf("Expected port tracking to be populated after sync, got: %v", err)
	}
}

func TestSyncPortTrackingWithRouterSelective_NoSkipFlag(t *testing.T) {
	// Clear tracking for test isolation
	helpers.ClearPortConflictTracking()

	// Create comprehensive mock router
	mockRouter := NewMockRouter()
	mockRouter.ClearAllPortForwards()
	mockRouter.ResetCallCounts()

	ctx := context.Background()

	// Setup: No rules at all (empty router)

	// Test: skipIfEmpty=false (should always sync)
	err := helpers.SyncPortTrackingWithRouterSelective(ctx, mockRouter, false)

	// Verify: Should always sync regardless of content
	if err != nil {
		t.Errorf("Expected no error during forced sync, got: %v", err)
	}

	// Should call ListAllPortForwards once for sync
	if mockRouter.GetCallCount("ListAllPortForwards") != 1 {
		t.Errorf("Expected ListAllPortForwards to be called once, got %d", mockRouter.GetCallCount("ListAllPortForwards"))
	}
}

func TestSyncPortTrackingWithRouterSelective_EdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		initialRules      []*unifi.PortForward
		skipIfEmpty       bool
		expectSync        bool
		expectedListCalls int
		expectError       bool
	}{
		{
			name:              "no rules skip true",
			initialRules:      []*unifi.PortForward{},
			skipIfEmpty:       true,
			expectSync:        false,
			expectedListCalls: 1,
			expectError:       false,
		},
		{
			name: "managed rules skip true",
			initialRules: []*unifi.PortForward{
				{
					Name:    "default/app:http",
					DstPort: "80",
					FwdPort: "8080",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			skipIfEmpty:       true,
			expectSync:        true,
			expectedListCalls: 2,
			expectError:       false,
		},
		{
			name: "manual rules skip true",
			initialRules: []*unifi.PortForward{
				{
					Name:    "manual-rule-1",
					DstPort: "80",
					FwdPort: "8080",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			skipIfEmpty:       true,
			expectSync:        false,
			expectedListCalls: 1,
			expectError:       false,
		},
		{
			name: "mixed rules skip true",
			initialRules: []*unifi.PortForward{
				{
					Name:    "default/service:http",
					DstPort: "80",
					FwdPort: "8080",
					Proto:   "tcp",
					Enabled: true,
				},
				{
					Name:    "manual-rule",
					DstPort: "89",
					FwdPort: "8089",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			skipIfEmpty:       true,
			expectSync:        true,
			expectedListCalls: 2,
			expectError:       false,
		},
		{
			name:              "no rules skip false",
			initialRules:      []*unifi.PortForward{},
			skipIfEmpty:       false,
			expectSync:        true,
			expectedListCalls: 1,
			expectError:       false,
		},
		{
			name: "complex service names",
			initialRules: []*unifi.PortForward{
				{
					Name:    "production/database:mysql-3306",
					DstPort: "3306",
					FwdPort: "3306",
					Proto:   "tcp",
					Enabled: true,
				},
				{
					Name:    "kube-system/api-server:https",
					DstPort: "443",
					FwdPort: "6443",
					Proto:   "tcp",
					Enabled: true,
				},
			},
			skipIfEmpty:       true,
			expectSync:        true,
			expectedListCalls: 2,
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear tracking for test isolation
			helpers.ClearPortConflictTracking()

			// Create comprehensive mock router
			mockRouter := NewMockRouter()
			mockRouter.ClearAllPortForwards()
			mockRouter.ResetCallCounts()

			ctx := context.Background()

			// Setup initial rules
			for _, rule := range tt.initialRules {
				mockRouter.AddPortForwardRule(*rule)
			}

			// Test the function
			err := helpers.SyncPortTrackingWithRouterSelective(ctx, mockRouter, tt.skipIfEmpty)

			// Verify error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Verify ListAllPortForwards call count
			if mockRouter.GetCallCount("ListAllPortForwards") != tt.expectedListCalls {
				t.Errorf("Expected ListAllPortForwards to be called %d times, got %d", tt.expectedListCalls, mockRouter.GetCallCount("ListAllPortForwards"))
			}

			// Verify sync occurred based on expectation
			if tt.expectSync {
				// Should have performed sync (at least one ListAllPortForwards call for sync)
				if mockRouter.GetCallCount("ListAllPortForwards") < 1 {
					t.Error("Expected sync to occur but ListAllPortForwards was not called")
				}
			} else {
				// Should have skipped sync (only the initial check)
				if mockRouter.GetCallCount("ListAllPortForwards") != 1 {
					t.Error("Expected sync to be skipped but additional operations were performed")
				}
			}
		})
	}
}
